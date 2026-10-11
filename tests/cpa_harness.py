"""Isolated native CPA host shared by reliability and explicit live tests."""
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

from cpa_e2e import ROOT, free_port

API = "/v0/management/commandcodebridge"


class MockServer(ThreadingHTTPServer):
    request_queue_size = 128
    daemon_threads = True


class CPAHost:
    def __init__(self, handler=None, base_url=None, timeout=10, proxy_url=None):
        self.temp = tempfile.TemporaryDirectory(prefix="commandcodebridge-host-")
        self.folder = pathlib.Path(self.temp.name)
        self.auth = self.folder / "auth"
        self.data = self.folder / "data"
        self.auth.mkdir()
        plugins = self.folder / "plugins"
        plugins.mkdir()
        library = pathlib.Path(os.environ.get("BRIDGE_LIBRARY", ROOT / "dist" / "commandcodebridge.so"))
        if not library.is_file():
            self.temp.cleanup()
            raise FileNotFoundError("Set BRIDGE_LIBRARY to a built native plugin")
        shutil.copy2(library, plugins / library.name)
        self.upstream = None
        if handler is not None:
            self.upstream = MockServer(("127.0.0.1", 0), handler)
            threading.Thread(target=self.upstream.serve_forever, daemon=True).start()
            base_url = f"http://127.0.0.1:{self.upstream.server_port}"
        assert base_url
        port = free_port()
        self.base = f"http://127.0.0.1:{port}"
        self.config = self.folder / "config.yaml"
        self.config.write_text(f'''host: "127.0.0.1"
port: {port}
remote-management:
  allow-remote: false
  secret-key: "fixture-management-key"
  disable-control-panel: true
auth-dir: "{self.auth.as_posix()}"
api-keys: ["fixture-client-key"]
usage-statistics-enabled: true
request-retry: 0
logging-to-file: false
requests:
  proxy-url: {json.dumps(proxy_url or "")}
plugins:
  enabled: true
  dir: "{plugins.as_posix()}"
  configs:
    commandcodebridge:
      enabled: true
      data_dir: "{self.data.as_posix()}"
      base_url: "{base_url}"
      timeout_seconds: {timeout}
''', encoding="utf-8")
        self.binary = os.environ.get("CPA_BINARY", "cli-proxy-api")
        self.process = None
        self.log = None

    def start(self):
        self.log = open(self.folder / "cpa.log", "a+", encoding="utf-8")
        self.process = subprocess.Popen(
            [self.binary, "-config", str(self.config)], stdout=self.log,
            stderr=self.log, cwd=self.folder,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise AssertionError("CPA exited before plugin became ready")
            try:
                if self.call(API + "/status")["version"] == "0.1.0":
                    return self
            except (OSError, urllib.error.HTTPError):
                pass
            time.sleep(0.05)
        raise AssertionError("CPA plugin did not become ready")

    def stop(self):
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
                raise AssertionError("CPA did not stop within 15 seconds")
        if self.log is not None:
            self.log.close()
            self.log = None

    def restart(self):
        # Windows terminate is a hard process stop. Wait for the write worker
        # before testing recovery from persisted data; Go tests cover graceful
        # shutdown flushing while requests complete.
        self.wait(lambda: not self.call(API + "/status").get("storage_pending", False), "completed disk snapshot")
        self.stop()
        return self.start()

    def request(self, path, body=None, method=None, management=True):
        headers = {"Authorization": "Bearer " + (
            "fixture-management-key" if management else "fixture-client-key")}
        if body is not None:
            headers["Content-Type"] = "application/json"
        return urllib.request.Request(
            self.base + path, data=None if body is None else json.dumps(body).encode(),
            headers=headers, method=method or ("GET" if body is None else "POST"))

    def call(self, path, body=None, method=None, management=True, stream=False):
        with urllib.request.urlopen(self.request(path, body, method, management), timeout=30) as response:
            raw = response.read().decode()
            return raw if stream else json.loads(raw)

    def error(self, path, body, status, management=True, stream=False):
        try:
            self.call(path, body, management=management, stream=stream)
        except urllib.error.HTTPError as error:
            assert error.code == status, (path, error.code, status)
            payload = json.loads(error.read())
            assert "error" in payload, payload
            return payload
        raise AssertionError(f"Expected HTTP {status} for {path}")

    def credential(self, key="fixture-primary-key", label="fixture account", priority=10):
        return self.call(API + "/credentials", {
            "api_key": key, "label": label, "priority": priority})["id"]

    def models(self, models):
        self.call(API + "/models", {"models": models}, method="PUT")
        expected = {m["id"] for m in models}
        self.wait(lambda: expected.issubset({m["id"] for m in self.call(
            "/v1/models", management=False)["data"]}), "model registration")

    def wait(self, predicate, description, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(0.05)
        raise AssertionError("Timed out waiting for " + description)

    def logs(self):
        return self.call(API + "/logs?limit=200")["items"]

    def __enter__(self):
        try:
            return self.start()
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def __exit__(self, *args):
        try:
            self.stop()
        finally:
            if self.upstream is not None:
                self.upstream.shutdown()
                self.upstream.server_close()
            self.temp.cleanup()


def chat_model(alias, upstream=None):
    return {"id": alias, "upstream_id": upstream or alias,
            "supported_endpoints": ["/chat/completions"],
            "reasoning_mapping": {"enabled": False, "rules": {}}}


def chat_body(model, stream=False):
    return {"model": model, "messages": [{"role": "user", "content": "Reply with OK."}], "stream": stream}
