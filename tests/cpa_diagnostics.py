"""Read-only diagnostics against an isolated real CPA host and mock upstream."""
import json

from cpa_e2e import MockUpstream
from cpa_harness import API, CPAHost, chat_body, chat_model


with CPAHost(MockUpstream) as host:
    cid = host.credential(label="private-fixture-label")
    host.models([chat_model("diagnostic-model")])

    def registered():
        report = host.call(API + "/diagnostics")
        row = next(item for item in report["credentials"] if item["id"] == cid)
        return row["status"] == "confirmed"

    host.wait(registered, "runtime registration diagnosis")
    row = host.call(API + "/diagnostics")["credentials"][0]
    assert all(item["status"] == "unknown" for item in row["evidence"] if item["source"] != "account_query")
    assert not host.logs(), "diagnosis sent a model request"

    host.call(API + "/models/test", {"credential_id": cid, "model": "diagnostic-model"})
    row = host.call(API + "/diagnostics")["credentials"][0]
    evidence = {item["source"]: item for item in row["evidence"]}
    assert evidence["plugin_test"]["status"] == "success" and evidence["cpa_request"]["status"] == "unknown", evidence
    host.call("/v1/chat/completions", chat_body("diagnostic-model"), management=False)
    assert next(item for item in host.call(API + "/diagnostics")["credentials"][0]["evidence"] if item["source"] == "cpa_request")["status"] == "success"

    result = host.call(API + "/credentials?id=" + cid, {"disabled": True}, method="PUT")
    assert result["saved"] is True and "registration" in result
    host.wait(lambda: host.call(API + "/diagnostics")["credentials"][0]["status"] == "disabled", "disabled runtime diagnosis")
    host.call(API + "/credentials?id=" + cid, {"disabled": False}, method="PUT")
    host.wait(registered, "reenabled runtime diagnosis")
    reset = host.call(API + "/credentials/clear-affinity", {})
    assert reset["host_confirmed"] is True
    host.wait(registered, "rotated runtime diagnosis")
    # Watchers may deduplicate unchanged bytes. Saving again must preserve the
    # existing confirmation instead of waiting for an event that will not fire.
    host.call(API + "/credentials?id=" + cid, {}, method="PUT")
    host.wait(registered, "unchanged save preserves runtime confirmation")

    report = host.call(API + "/compatibility")
    assert report["baseline"]["cpa_baseline"] == "8.0.23"
    assert next(item for item in report["checks"] if item["name"] == "snapshot_backend")["status"] == "unknown"
    summary = json.dumps(host.call(API + "/diagnostics/summary"))
    for private in (cid, "private-fixture-label", "fixture-primary-key", str(host.folder), "diagnostic-model"):
        assert private not in summary, "summary exposed private diagnostic detail"

    host.restart()
    host.wait(registered, "registration after restart")
    assert next(item for item in host.call(API + "/diagnostics")["credentials"][0]["evidence"] if item["source"] == "cpa_request")["status"] == "success"
    # A watched file removal unregisters it in CPA while the plugin retains its
    # last credential snapshot: reproduce the route failure before the executor.
    credential_path = next(host.auth.glob("*.json"))
    saved = credential_path.read_bytes()
    credential_path.unlink()
    host.wait(lambda: host.call(API + "/diagnostics")["credentials"][0]["status"] == "missing", "missing runtime registration")
    before = len(host.logs())
    host.error("/v1/chat/completions", chat_body("diagnostic-model"), 503, management=False)
    assert len(host.logs()) == before, "host rejection should not create a plugin attempt"
    credential_path.write_bytes(saved)
    host.wait(registered, "restored runtime registration")
    host.call("/v1/chat/completions", chat_body("diagnostic-model"), management=False)
    print("PASS: isolated CPA registration, save confirmation, disable/re-enable, rotation, evidence sources, redaction, restart, missing runtime rejection and recovery")
