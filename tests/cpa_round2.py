"""Round two native integration: loopback evidence, recovery and bounded live runner."""
import io
import json
import pathlib
import tempfile
import time
import unittest.mock

import commandcode_live
from cpa_e2e import MockUpstream
from cpa_harness import API, CPAHost, chat_body, chat_model


class EvidenceUpstream(MockUpstream):
    outcomes = {}
    zdr_headers = []

    def do_POST(self):
        raw = self.rfile.read(int(self.headers["Content-Length"]))
        body = json.loads(raw)
        outcome = self.outcomes.get(body["model"])
        self.zdr_headers.append(dict(self.headers))
        if outcome:
            self.send_response(outcome[0])
            self.send_header("Content-Type", "application/json")
            self.send_header("Retry-After", "1")
            self.end_headers()
            self.wfile.write(json.dumps({"error": {"code": outcome[1], "message": "fixture error"}}).encode())
            return
        self.rfile = io.BytesIO(raw)
        super().do_POST()


def evidence(host, cid):
    return {r["model"]: r for r in host.call(API + "/models/evidence?credential_id=" + cid)["items"]}


def run():
    with CPAHost(EvidenceUpstream) as host:
        cid = host.credential()
        other = host.credential("fixture-other-key", "other", 1)
        host.call(API + "/credentials/usage?id=" + cid)
        host.models([chat_model("allowed"), chat_model("denied"), chat_model("rate"), chat_model("generic")])
        assert all(r["status"] == "unknown" for r in evidence(host, cid).values())
        EvidenceUpstream.outcomes = {"denied": (403, "MODEL_NOT_IN_PLAN"), "rate": (429, "rate_limit_error"), "generic": (403, "permission_error")}
        for name, code in (("denied", 403), ("rate", 429), ("generic", 403)):
            host.error(API + "/models/test", {"credential_id": cid, "model": name}, code)
        host.call(API + "/models/test", {"credential_id": cid, "model": "allowed"})
        rows = evidence(host, cid)
        assert [rows[m]["status"] for m in ("allowed", "denied", "rate", "generic")] == ["available", "not_in_plan", "temporary_limit", "permission_unknown"]
        assert all(r["status"] == "unknown" for r in evidence(host, other).values())
        assert next(c for c in host.call(API + "/credentials")["credentials"] if c["id"] == cid)["health"]["status"] != "invalid"
        del EvidenceUpstream.outcomes["rate"]
        time.sleep(1.1)
        host.call(API + "/models/test", {"credential_id": cid, "model": "rate"})
        assert evidence(host, cid)["rate"]["status"] == "available"
        host.call("/v1/chat/completions", chat_body("allowed"), management=False)
        rows = evidence(host, cid)
        # CPA may select either enabled credential. Direct source remains independent.
        assert rows["allowed"]["evidence"][0]["status"] == "available"
        before = len(host.logs())
        for _ in range(3):
            evidence(host, cid)
        assert len(host.logs()) == before
        host.restart()
        host.wait(lambda: host.call(API + "/status")["credentials"] == 2, "restored credentials")
        assert evidence(host, cid)["rate"]["status"] == "available"
        host.call(API + "/credentials?id=" + cid, {"api_key": "fixture-replacement-key"}, method="PUT")
        assert all(r["status"] == "unknown" for r in evidence(host, cid).values())
        host.call(API + "/models/test", {"credential_id": cid, "model": "allowed"})
        host.models([chat_model("allowed", "remapped")])
        assert evidence(host, cid)["allowed"]["status"] == "unknown"
        # Damage one file after a completed snapshot, then hard-stop/restart CPA.
        host.wait(lambda: not host.call(API + "/status")["storage_pending"], "snapshot")
        host.stop()
        assert (host.data / "requests.json.last-good").is_file()
        (host.data / "requests.json").write_text('{"truncated":', encoding="utf-8")
        host.start()
        status = host.call(API + "/status")
        assert status["models"] == 1
        assert any(w["file"] == "requests.json" and w["recovered"] for w in status["storage"]["recovery_warnings"])
        assert list(host.data.glob("requests.json.corrupt-*"))
        host.call(API + "/statistics/reset", {})
        host.restart()
        assert host.call(API + "/statistics")["totals"]["requests"] == 0
        host.stop()
        (host.data / "usage.json.last-good").unlink(missing_ok=True)
        (host.data / "usage.json").write_text("invalid", encoding="utf-8")
        host.start()
        assert any(w["file"] == "usage.json" and not w["recovered"] for w in host.call(API + "/status")["storage"]["recovery_warnings"])
        host.wait(lambda: host.call(API + "/status")["credentials"] == 2, "config survived recovery")
        print(json.dumps({"check": "evidence and recovery", "status": "PASS", "real_requests": 0}), flush=True)

    # Run the live validator itself against the local mock. Never target real API.
    EvidenceUpstream.outcomes = {}
    EvidenceUpstream.zdr_headers = []
    args = commandcode_live.parser().parse_args(["--generate", "--chat-model", "mock-chat", "--messages-model", "mock-claude",
            "--tools", "--image", "--thinking", "--zdr", "--max-requests", "13", "--max-tokens", "2048"])
    with tempfile.TemporaryDirectory() as folder:
        args.credential_file = pathlib.Path(folder) / "fixture.json"
        args.credential_file.write_text(json.dumps({"api_key": "fixture-live-runner-key"}))
        with unittest.mock.patch.object(commandcode_live, "CPAHost", side_effect=lambda **_: CPAHost(EvidenceUpstream)):
            report = commandcode_live.run(args)
    assert report["status"] == "PASS" and report["requests"] == 13
    assert len(EvidenceUpstream.zdr_headers) == 13
    assert all(any(k.lower() == "x-cmd-zdr" and v == "1" for k, v in h.items()) for h in EvidenceUpstream.zdr_headers)
    print(json.dumps({"check": "bounded live runner against mock", "status": "PASS", "requests": 13,
                      "protocols": ["Chat", "Responses", "Messages"], "tools": True, "image_transport": True,
                      "signature_replay": True, "zdr_header": True, "real_requests": 0}), flush=True)


if __name__ == "__main__":
    run()
