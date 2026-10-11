"""Native-host fault injection. Uses only loopback mocks and fixture keys."""
import collections
import io
import json
import socket
import threading
import time
import urllib.request

from cpa_e2e import MockUpstream
from cpa_harness import API, CPAHost, chat_body, chat_model


class FaultUpstream(MockUpstream):
    lock = threading.Lock()
    counts = collections.Counter()
    disconnected = threading.Event()
    completed = threading.Event()

    def do_POST(self):
        length = int(self.headers["Content-Length"])
        raw = self.rfile.read(length)
        body = json.loads(raw)
        model = body["model"]
        key = self.headers.get("Authorization")
        with self.lock:
            self.counts[(model, key)] += 1
            attempt = self.counts[(model, key)]
        if model == "fault-rate" and attempt == 1:
            self.send_response(429)
            self.send_header("Content-Type", "application/json")
            self.send_header("Retry-After", "2")
            self.end_headers()
            self.wfile.write(json.dumps({"error": {"message": "fixture rate limit", "code": "rate_limit_error"}}).encode())
            return
        if model == "fault-server":
            return self.send_json({"error": {"message": "fixture unavailable"}}, 503)
        if model in ("fault-truncated", "fault-timeout", "fault-cancel"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Connection", "close")
            self.end_headers()
            chunk = {"id": "fault-fixture", "object": "chat.completion.chunk", "model": model,
                     "choices": [{"index": 0, "delta": {"role": "assistant", "content": "partial"}, "finish_reason": None}]}
            if model == "fault-timeout":
                # Keep the stream idle after headers, without buffering content.
                self.connection.settimeout(15)
                try:
                    closed = self.connection.recv(1) == b""
                except ConnectionError:
                    closed = True
                if closed:
                    self.disconnected.set()
                return
            data = ("data: " + json.dumps(chunk) + "\n\n").encode()
            self.wfile.write(data)
            self.wfile.flush()
            if model == "fault-truncated":
                return
            try:
                for _ in range(150):
                    time.sleep(0.1)
                    self.wfile.write(data)
                    self.wfile.flush()
            except ConnectionError:
                self.disconnected.set()
            finally:
                self.completed.set()
            return
        # Replay the consumed request into the original success fixture.
        self.rfile = io.BytesIO(raw)
        super().do_POST()


def run():
    FaultUpstream.counts.clear()
    with CPAHost(FaultUpstream) as host:
        cid = host.credential()
        models = [chat_model(name) for name in ["fault-rate", "fault-server", "fault-truncated", "fault-timeout", "fault-cancel", "mock-chat"]]
        models += [chat_model("rate-alias", "fault-rate")]
        host.models(models)
        test = lambda model: {"credential_id": cid, "model": model}

        host.error(API + "/models/test", test("fault-rate"), 429)
        host.error(API + "/models/test", test("rate-alias"), 429)
        assert FaultUpstream.counts[("fault-rate", "Bearer fixture-primary-key")] == 1
        rate_logs = [row for row in host.logs() if row["upstream_model"] == "fault-rate"]
        assert len(rate_logs) == 2 and sum(row["upstream_skipped"] for row in rate_logs) == 1
        assert all(row.get("retry_at") for row in rate_logs)
        time.sleep(2.1)
        host.call(API + "/models/test", test("rate-alias"))
        assert FaultUpstream.counts[("fault-rate", "Bearer fixture-primary-key")] == 2

        # Establish the shared identity before testing different keys.
        second = host.credential("fixture-second-key", "same account", 5)
        host.call(API + "/credentials/usage?id=" + cid + "&refresh=1")
        host.call(API + "/credentials/usage?id=" + second + "&refresh=1")
        # A fresh upstream model gives both keys a fresh rate-limited window.
        with FaultUpstream.lock:
            FaultUpstream.counts[("fault-rate", "Bearer fixture-primary-key")] = 0
        host.error(API + "/models/test", test("fault-rate"), 429)
        host.error(API + "/models/test", {"credential_id": second, "model": "rate-alias"}, 429)
        assert FaultUpstream.counts[("fault-rate", "Bearer fixture-second-key")] == 0
        host.call(API + "/models/test", test("mock-chat"))
        time.sleep(2.1)
        host.call(API + "/models/test", test("fault-rate"))

        # CPA must use the lower-priority key after the primary's 503 failure.
        # The server fixture fails both keys; the client receives a single error.
        host.error("/v1/chat/completions", chat_body("fault-server"), 503, management=False)
        assert FaultUpstream.counts[("fault-server", "Bearer fixture-primary-key")] >= 1
        assert FaultUpstream.counts[("fault-server", "Bearer fixture-second-key")] >= 1
        host.error(API + "/models/test", test("fault-truncated"), 502)
        streamed = host.call("/v1/chat/completions", chat_body("fault-truncated", True), management=False, stream=True)
        assert "partial" in streamed and '"error"' in streamed, streamed

        FaultUpstream.disconnected.clear()
        started = time.monotonic()
        host.error(API + "/models/test", test("fault-timeout"), 504)
        assert 9 <= time.monotonic() - started < 15
        assert FaultUpstream.disconnected.wait(2), "timeout left upstream connection open"

        # Close the client while upstream is still emitting; it must be released.
        FaultUpstream.disconnected.clear()
        FaultUpstream.completed.clear()
        req = host.request("/v1/chat/completions", chat_body("fault-cancel", True), management=False)
        with urllib.request.urlopen(req, timeout=10) as response:
            while b"partial" not in response.readline():
                pass
            # An immediate close avoids reading the rest of the HTTP response.
            response.fp.raw._sock.shutdown(socket.SHUT_RDWR)
        assert FaultUpstream.disconnected.wait(3), "cancel left upstream connection open"
        assert FaultUpstream.completed.wait(1)
        host.wait(lambda: any(row["upstream_model"] == "fault-cancel" and row["status"] >= 400 for row in host.logs()), "cancelled request log")
        # The native adapter must publish one failure per client request, even
        # when the 503 request tries two credentials. No usage was reported by
        # these fixtures, so they must not invent tokens for partial output.
        queue = []
        def failures_accounted():
            queue.extend(host.call("/v8/management/observability/usage/queue?count=100"))
            return len(queue) >= 3
        host.wait(failures_accounted, "failure accounting")
        assert len(queue) == 3, len(queue)
        assert all(row["failed"] and row["tokens"]["total_tokens"] == 0 for row in queue)

        # Rotate, edit and disable before restarting the real CPA process.
        rotation = host.call(API + "/credentials/clear-affinity", {})
        assert rotation["reset_count"] == 2
        host.call(API + "/credentials?id=" + second, {"disabled": True, "label": "retained disabled account"}, method="PUT")
        host.wait(lambda: len(host.logs()) == host.call(API + "/statistics")["totals"]["requests"], "statistics persistence")
        before = host.call(API + "/statistics")["totals"]
        before_logs = {row["id"] for row in host.logs()}
        before_auth = {path.name: json.loads(path.read_text()) for path in host.auth.glob("*.json")}
        host.restart()
        host.wait(lambda: host.call(API + "/status")["credentials"] == 2, "restored credentials")
        accounts = host.call(API + "/credentials")["credentials"]
        assert any(account["id"] == second and account["disabled"] and account["label"] == "retained disabled account" for account in accounts)
        assert host.call(API + "/models")["models"] == models
        assert host.call(API + "/statistics")["totals"] == before
        assert {row["id"] for row in host.logs()} == before_logs
        after_auth = {path.name: json.loads(path.read_text()) for path in host.auth.glob("*.json")}
        assert {k: v.get("routing_id") for k, v in before_auth.items()} == {k: v.get("routing_id") for k, v in after_auth.items()}
        result = host.call("/v1/chat/completions", chat_body("mock-chat"), management=False)
        assert result["choices"][0]["message"]["content"] == "OK from mock"
        host.wait(lambda: host.call(API + "/statistics")["totals"]["requests"] == before["requests"] + 1, "post-restart accounting")
        status = host.call(API + "/status")
        assert not status["storage_error"], status
        for path in host.data.glob("*.json"):
            assert "fixture-primary-key" not in path.read_text(), path.name
        print("PASS: native CPA 429/alias/shared-account cooldown and recovery, 503 failover, incomplete SSE, timeout, client cancel, restart persistence and accounting", flush=True)


if __name__ == "__main__":
    run()
