"""Explicit anonymous GitHub installation in an isolated CPA, without model calls.

CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_store_install.py
Requires Internet access. Not run on every CI job to avoid GitHub API throttling.
"""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from cpa_e2e import free_port

SOURCE = "https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json"
parser = argparse.ArgumentParser()
parser.add_argument("--proxy-url", help="Existing CPA outbound proxy URL, if needed")
args = parser.parse_args()
local_http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

with tempfile.TemporaryDirectory(prefix="commandcodebridge-install-") as temporary:
    folder = pathlib.Path(temporary)
    port = free_port()
    base = f"http://127.0.0.1:{port}/v0/management"
    config = folder / "config.yaml"
    config.write_text(f'''host: "127.0.0.1"
port: {port}
remote-management:
  allow-remote: false
  secret-key: "fixture-management-key"
  disable-control-panel: true
auth-dir: "{(folder / 'auth').as_posix()}"
api-keys: ["fixture-client-key"]
logging-to-file: false
requests:
  proxy-url: {json.dumps(args.proxy_url or "")}
plugins:
  enabled: true
  dir: "{(folder / 'plugins').as_posix()}"
  store-sources:
    - {SOURCE}
''', encoding="utf-8")
    environment = os.environ.copy()
    for name in ("GITHUB_TOKEN", "github_token", "GH_TOKEN", "GITHUB_API_TOKEN", "GITSTORE_GIT_TOKEN"):
        environment.pop(name, None)
    process = None

    def call(path, body=None):
        request = urllib.request.Request(
            base + path,
            data=json.dumps(body).encode() if body is not None else None,
            headers={"Authorization": "Bearer fixture-management-key", "Content-Type": "application/json"})
        try:
            with local_http.open(request, timeout=120) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise AssertionError(f"CPA returned {error.code}: {error.read().decode()}") from None

    def start(log):
        global process
        process = subprocess.Popen(
            [os.environ.get("CPA_BINARY", "cli-proxy-api"), "-config", str(config)],
            cwd=folder, stdout=log, stderr=log, env=environment,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            assert process.poll() is None, "CPA exited before becoming ready"
            try:
                call("/plugins")
                return
            except (OSError, AssertionError):
                time.sleep(0.1)
        raise AssertionError("CPA did not become ready")

    def stop():
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)

    with (folder / "cpa.log").open("w", encoding="utf-8") as log:
        try:
            start(log)
            listing = call("/plugin-store")
            plugin = next(item for item in listing["plugins"] if item["id"] == "commandcodebridge")
            assert not plugin["installed"]
            source_id = plugin["source_id"]
            result = call(f"/plugin-store/commandcodebridge/install?source={source_id}", {})
            assert result["status"] == "installed" and result["version"] == "0.1.0", result
            installed = pathlib.Path(result["path"])
            assert installed.is_file() and installed.name.startswith("commandcodebridge-v0.1.0")
            digest = hashlib.sha256(installed.read_bytes()).hexdigest()
            stop()
            start(log)
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                try:
                    assert call("/commandcodebridge/status")["version"] == "0.1.0"
                    break
                except (OSError, AssertionError, KeyError):
                    time.sleep(0.1)
            else:
                raise AssertionError("Installed plugin did not load after restart")
            listing = call("/plugin-store")
            plugin = next(item for item in listing["plugins"] if item["id"] == "commandcodebridge")
            assert plugin["installed"] and plugin["installed_version"] == "0.1.0"
            assert plugin["registered"] and plugin["effective_enabled"]
            assert hashlib.sha256(installed.read_bytes()).hexdigest() == digest
            print("PASS: anonymous GitHub source, Release selection, checksum verification, install and restart loading")
        finally:
            stop()
