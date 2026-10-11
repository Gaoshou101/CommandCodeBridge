"""Load the actual shared library in CPA and exercise local mock upstreams.

CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_e2e.py
Add --serve to keep the tested console open for browser verification.
No real upstream credentials or paid model requests are used.
"""
import argparse
import json
import os
import pathlib
import socket
import shutil
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = pathlib.Path(__file__).resolve().parents[1]
requests = []
authorization_headers = []


class MockUpstream(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def send_json(self, body, status=200):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path.endswith("/models"):
            return self.send_json({"data": [
                {"id": "mock-chat", "name": "Mock chat", "supported_endpoints": ["/chat/completions", "/responses"], "context_length": 32000},
                {"id": "mock-claude", "name": "Mock Claude", "supported_endpoints": ["/messages"], "context_length": 32000},
            ]})
        if "/whoami" in self.path:
            return self.send_json({"user": {"id": "fixture-shared-account"}})
        if "/billing/credits" in self.path:
            return self.send_json({"credits": {"monthlyCredits": 40, "purchasedCredits": 5, "freeCredits": 0, "planId": "pro"}, "windowLimits": {"fiveHour": {"used": 2, "cap": 5, "resetAt": int(time.time())+3600}, "weekly": {"used": 3, "cap": 10, "resetAt": int(time.time())+86400}}})
        if "/billing/subscriptions" in self.path:
            return self.send_json({"data": {"planId": "pro", "currentPeriodEnd": "2026-11-01T00:00:00Z"}})
        if "/usage/summary" in self.path:
            return self.send_json({"totalRequests": 12})
        self.send_json({"error": {"message": "unknown route"}}, 404)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        requests.append((self.path, body))
        authorization_headers.append(self.headers.get("Authorization"))
        if self.headers.get("Authorization") == "Bearer fixture-invalid-key":
            return self.send_json({"error": {"message": "invalid API key", "code": "invalid_api_key"}}, 401)
        if body.get("model") == "mock-error":
            return self.send_json({"error": {"message": "model permission denied", "code": "model_not_in_plan"}}, 403)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Connection", "close")
        self.end_headers()
        tool = bool(body.get("tools")) and not any(m.get("role") == "tool" or any(p.get("type") == "tool_result" for p in m.get("content", []) if isinstance(p, dict)) for m in body.get("messages", []))
        if self.path.endswith("/messages"):
            events = [
                {"type": "message_start", "message": {"id": "msg_fixture", "type": "message", "role": "assistant", "model": body["model"], "content": [], "usage": {"input_tokens": 6, "output_tokens": 0, "cache_read_input_tokens": 4}}},
            ]
            if body.get("thinking", {}).get("type") in ("adaptive", "enabled"):
                events += [
                    {"type": "content_block_start", "index": 1, "content_block": {"type": "thinking", "thinking": ""}},
                    {"type": "content_block_delta", "index": 1, "delta": {"type": "thinking_delta", "thinking": "fixture reasoning"}},
                    {"type": "content_block_delta", "index": 1, "delta": {"type": "signature_delta", "signature": "fixture-signed-thought"}},
                    {"type": "content_block_stop", "index": 1},
                ]
            if tool:
                events += [
                    {"type": "content_block_start", "index": 0, "content_block": {"type": "tool_use", "id": "call_fixture", "name": "read_file", "input": {}}},
                    {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": '{"path":'}},
                    {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": '"a.txt"}'}},
                ]
            else:
                events += [
                    {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}},
                    {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "OK from mock"}},
                ]
            events += [
                {"type": "content_block_stop", "index": 0},
                {"type": "message_delta", "delta": {"stop_reason": "tool_use" if tool else "end_turn"}, "usage": {"output_tokens": 3}},
                {"type": "message_stop"},
            ]
            data = "".join("event: "+e["type"]+"\ndata: "+json.dumps(e)+"\n\n" for e in events)
        else:
            def chunk(delta, finish=None, usage=None):
                d = {"id": "chatcmpl_fixture", "object": "chat.completion.chunk", "created": int(time.time()), "model": body["model"], "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
                if usage is not None:
                    d["usage"] = usage
                return "data: "+json.dumps(d)+"\n\n"
            if tool:
                data = chunk({"role": "assistant", "tool_calls": [{"index": 0, "id": "call_fixture", "type": "function", "function": {"name": "read_file", "arguments": '{"path":'}}]})
                data += chunk({"tool_calls": [{"index": 0, "function": {"arguments": '"a.txt"}'}}]})
            else:
                data = chunk({"role": "assistant", "content": "OK from mock"})
            data += chunk({}, "tool_calls" if tool else "stop", {"prompt_tokens": 10, "completion_tokens": 3, "total_tokens": 13, "prompt_tokens_details": {"cached_tokens": 4}})+"data: [DONE]\n\n"
        encoded = data.encode()
        try:
            for i in range(0, len(encoded), 13):
                self.wfile.write(encoded[i:i+13])
                self.wfile.flush()
                time.sleep(0.001)
        except (BrokenPipeError, ConnectionResetError):
            pass


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run(serve=False):
    binary = os.environ.get("CPA_BINARY", "cli-proxy-api")
    library = pathlib.Path(os.environ.get("BRIDGE_LIBRARY", str(ROOT / "dist" / "commandcodebridge.so")))
    assert library.exists(), "Build dist/commandcodebridge.so first"
    upstream = ThreadingHTTPServer(("127.0.0.1", 0), MockUpstream)
    threading.Thread(target=upstream.serve_forever, daemon=True).start()
    with tempfile.TemporaryDirectory(prefix="commandcodebridge-cpa-") as folder:
        folder = pathlib.Path(folder)
        plugins = folder / "plugins"
        plugins.mkdir()
        shutil.copy2(library, plugins / library.name)
        port = free_port()
        auth = folder / "auth"
        auth.mkdir()
        config = folder / "config.yaml"
        # Legacy keys are supported by CPA v8 and simplify the test fixture.
        config.write_text(f'''host: "127.0.0.1"
port: {port}
remote-management:
  allow-remote: false
  secret-key: "fixture-management-key"
  disable-control-panel: true
auth-dir: "{auth.as_posix()}"
api-keys: ["fixture-client-key"]
usage-statistics-enabled: true
request-retry: 0
logging-to-file: false
plugins:
  enabled: true
  dir: "{plugins.as_posix()}"
  configs:
    commandcodebridge:
      enabled: true
      data_dir: "{(folder / 'data').as_posix()}"
      base_url: "http://127.0.0.1:{upstream.server_port}"
''')
        log = open(folder / "cpa.log", "w+", encoding="utf-8")
        process = subprocess.Popen([binary, "-config", str(config)], stdout=log, stderr=log, cwd=folder, creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        base = f"http://127.0.0.1:{port}"

        def call(path, body=None, method=None, management=True, stream=False, extra_headers=None):
            headers = {"Authorization": "Bearer "+("fixture-management-key" if management else "fixture-client-key")}
            headers.update(extra_headers or {})
            if body is not None:
                headers["Content-Type"] = "application/json"
            req = urllib.request.Request(base+path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method or ("GET" if body is None else "POST"))
            with urllib.request.urlopen(req, timeout=20) as response:
                raw = response.read().decode()
                return raw if stream else json.loads(raw)

        api = "/v0/management/commandcodebridge"

        def await_credential(cid, disabled):
            for _ in range(100):
                files = call("/v8/management/credentials")["files"]
                matching = [f for f in files if f.get("name") == cid+".json"]
                if matching and all(f["disabled"] == disabled for f in matching):
                    return
                time.sleep(0.05)
            raise AssertionError("CPA watcher did not apply credential state")
        try:
            for _ in range(100):
                if process.poll() is not None:
                    log.seek(0)
                    raise AssertionError("CPA exited: "+log.read()[-5000:])
                try:
                    status = call(api+"/status")
                    break
                except (OSError, urllib.error.HTTPError):
                    time.sleep(0.1)
            else:
                log.seek(0)
                raise AssertionError("CPA did not load plugin: "+log.read()[:5000])
            assert status["version"] == "0.1.0"
            credential = call(api+"/credentials", {"label": "测试账号", "api_key": "fixture-upstream-key", "priority": 10})
            cid = credential["id"]
            models = call(api+"/models/refresh", {})["models"]
            models[0]["reasoning_mapping"] = {"enabled": True, "rules": {"low": "high"}}
            call(api+"/models", {"models": models}, "PUT")
            for _ in range(60):
                available = call("/v1/models", management=False)["data"]
                if len(available) >= 2:
                    break
                time.sleep(0.1)
            else:
                raise AssertionError("models were not registered after import")
            # Use actual CPA auth watchers for priority, rotation and deletion.
            secondary = call(api+"/credentials", {"label": "备用账号", "api_key": "fixture-secondary-key", "priority": 5})
            sid = secondary["id"]
            rotation = call(api+"/credentials/clear-affinity", {})
            assert rotation["reset_count"] == 2, rotation
            assert len(list((auth / ".commandcodebridge-affinity-backups").glob("*/*.bak"))) == 2
            count = 0
            probe = {"model": "commandcode/mock-chat", "messages": [{"role": "user", "content": "priority probe"}]}
            call("/v1/chat/completions", probe, management=False)
            assert authorization_headers[-1] == "Bearer fixture-upstream-key"
            count += 1
            call(api+"/credentials?id="+cid, {"label": "已编辑账号", "disabled": True}, "PUT")
            await_credential(cid, True)
            call("/v1/chat/completions", probe, management=False)
            assert authorization_headers[-1] == "Bearer fixture-secondary-key"
            count += 1
            call(api+"/credentials?id="+cid, {"disabled": False}, "PUT")
            await_credential(cid, False)
            call(api+"/credentials?id="+sid, method="DELETE")
            for _ in range(50):
                files = call("/v8/management/credentials")["files"]
                commandcode_files = [f for f in files if f.get("provider") == "commandcode"]
                if len(commandcode_files) == 1:
                    break
                time.sleep(0.1)
            assert len(commandcode_files) == 1, commandcode_files
            for model in ("commandcode/mock-chat", "commandcode/mock-claude"):
                body = {"model": model, "messages": [{"role": "user", "content": "hello"}], "reasoning_effort": "low"}
                result = call("/v1/chat/completions", body, management=False)
                assert result["model"] == model and result["choices"][0]["message"]["content"] == "OK from mock", result
                assert result["usage"]["prompt_tokens"] == 10, result
                if model.endswith("mock-chat"):
                    assert requests[-1][1]["reasoning_effort"] == "high", requests[-1]
                else:
                    assert requests[-1][1]["output_config"]["effort"] == "low" and "reasoning_effort" not in requests[-1][1], requests[-1]
                streamed = call("/v1/chat/completions", {**body, "stream": True}, management=False, stream=True)
                assert "OK from mock" in streamed and "[DONE]" in streamed, streamed
                count += 2
                tools = [{"type": "function", "function": {"name": "read_file", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}}}}]
                tool_result = call("/v1/chat/completions", {**body, "tools": tools}, management=False)
                message = tool_result["choices"][0]["message"]
                assert json.loads(message["tool_calls"][0]["function"]["arguments"]) == {"path": "a.txt"}, tool_result
                follow = {**body, "tools": tools, "messages": [*body["messages"], message, {"role": "tool", "tool_call_id": "call_fixture", "content": "file content"}]}
                assert call("/v1/chat/completions", follow, management=False)["choices"][0]["message"]["content"] == "OK from mock"
                count += 2
            for stream in (False, True):
                response = call("/v1/responses", {"model": "commandcode/mock-chat", "input": "hello", "stream": stream}, management=False, stream=stream)
                assert "OK from mock" in json.dumps(response), response
                if stream:
                    assert "response.completed" in response, response
                count += 1
            for stream in (False, True):
                body = {"model": "commandcode/mock-claude", "max_tokens": 64, "thinking": {"type": "adaptive"}, "messages": [{"role": "user", "content": "hello"}], "stream": stream}
                response = call("/v1/messages", body, management=False, stream=stream)
                assert "fixture-signed-thought" in json.dumps(response), response
                assert requests[-1][1]["thinking"] == {"type": "adaptive"}, requests[-1]
                count += 1
                if not stream:
                    signed_content = response["content"]
            body = {"model": "commandcode/mock-claude", "max_tokens": 64, "thinking": {"type": "adaptive"}, "messages": [{"role": "user", "content": "hello"}, {"role": "assistant", "content": signed_content}, {"role": "user", "content": "continue"}]}
            call("/v1/messages", body, management=False)
            assert "fixture-signed-thought" in json.dumps(requests[-1][1]["messages"]), requests[-1]
            count += 1
            for stream in (False, True):
                response = call("/v1/messages", {"model": "commandcode/mock-chat", "max_tokens": 64, "messages": [{"role": "user", "content": "hello"}], "stream": stream}, management=False, stream=stream)
                assert "OK from mock" in json.dumps(response), response
                assert "message_stop" in response if stream else response["type"] == "message", response
                count += 1
            native_body = {"model": "commandcode/mock-claude", "max_tokens": 128, "thinking": {"type": "adaptive"}, "tools": [{"name": "read_file", "input_schema": {"type": "object", "properties": {"path": {"type": "string"}}}}], "messages": [{"role": "user", "content": "read a.txt"}]}
            result = call("/v1/messages", native_body, management=False)
            tool = next(b for b in result["content"] if b["type"] == "tool_use")
            assert tool["input"] == {"path": "a.txt"}, result
            follow = {**native_body, "messages": [*native_body["messages"], {"role": "assistant", "content": result["content"]}, {"role": "user", "content": [{"type": "tool_result", "tool_use_id": tool["id"], "content": "file content"}]}]}
            assert "OK from mock" in json.dumps(call("/v1/messages", follow, management=False))
            result = call("/v1/messages", {**native_body, "stream": True}, management=False, stream=True)
            assert "input_json_delta" in result and "fixture-signed-thought" in result and "message_stop" in result, result
            image = {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "fixture-base64-image"}}
            call("/v1/messages", {"model": "commandcode/mock-claude", "max_tokens": 64, "messages": [{"role": "user", "content": [image, {"type": "text", "text": "describe"}]}]}, management=False)
            assert requests[-1][1]["messages"][0]["content"][0] == image
            count += 4
            response_tools = [{"type": "function", "name": "read_file", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}}}]
            result = call("/v1/responses", {"model": "commandcode/mock-chat", "input": "read a.txt", "tools": response_tools}, management=False)
            function = next(b for b in result["output"] if b["type"] == "function_call")
            assert json.loads(function["arguments"]) == {"path": "a.txt"}, result
            result = call("/v1/responses", {"model": "commandcode/mock-chat", "input": [function, {"type": "function_call_output", "call_id": function["call_id"], "output": "file content"}], "tools": response_tools}, management=False)
            assert "OK from mock" in json.dumps(result), result
            count += 2
            for stream in (False, True):
                response = call("/v1/messages", {"model": "commandcode/mock-claude", "max_tokens": 64, "messages": [{"role": "user", "content": "hello"}], "stream": stream}, management=False, stream=stream)
                assert "OK from mock" in json.dumps(response), response
                if stream:
                    assert "message_stop" in response, response
                count += 1
            usage = call(api+"/credentials/usage?id="+cid+"&refresh=1")
            assert usage["remaining_credits"] == 45 and len(usage["windows"]) == 2, usage
            assert "fixture-upstream-key" not in json.dumps(call(api+"/credentials"))
            for _ in range(50):
                stats = call(api+"/statistics")
                if stats["totals"]["requests"] >= count:
                    break
                time.sleep(0.05)
            assert stats["totals"]["requests"] == count, stats
            assert stats["totals"]["with_usage"] == count, stats
            logs = call(api+"/logs")
            assert logs["total"] == count and all(r["status"] == 200 for r in logs["items"]), logs
            # v8 publishes usage records through its management queue.
            cpa_usage = []
            for _ in range(50):
                cpa_usage.extend(call("/v8/management/observability/usage/queue?count=100"))
                if len(cpa_usage) >= count:
                    break
                time.sleep(0.1)
            assert len(cpa_usage) == count, cpa_usage
            assert sum(r["tokens"]["total_tokens"] for r in cpa_usage) == count*13, cpa_usage
            # Error semantics must survive the actual native RPC adapter.
            error_models = [*models, {**models[0], "id": "commandcode/mock-error", "upstream_id": "mock-error"}]
            call(api+"/models", {"models": error_models}, "PUT")
            try:
                call(api+"/models/test", {"credential_id": cid, "model": "commandcode/mock-error"})
                raise AssertionError("403 permission error was accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 403, error.code
            invalid = call(api+"/credentials", {"label": "无效测试密钥", "api_key": "fixture-invalid-key"})
            try:
                call(api+"/models/test", {"credential_id": invalid["id"], "model": "commandcode/mock-chat"})
                raise AssertionError("401 key error was accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 401, error.code
            call(api+"/credentials?id="+invalid["id"], method="DELETE")
            call(api+"/models", {"models": models}, "PUT")
            console = call("/v0/resource/plugins/commandcodebridge/console", stream=True)
            assert "/*__CPA_AUTH__*/" not in console and "CommandCodeBridgeAuthHeaders" in console
            print(f"PASS: native CPA plugin, {count} successful requests, Chat/Responses/Messages, streaming, tool roundtrips, images, signed thinking, reasoning mapping, quotas, host accounting, priority, affinity rotation, edit/delete and 401/403 errors", flush=True)
            if serve:
                print("CONSOLE "+base+"/v0/resource/plugins/commandcodebridge/console", flush=True)
                while process.poll() is None:
                    time.sleep(1)
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            log.close()
            upstream.shutdown()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--serve", action="store_true")
    run(parser.parse_args().serve)
