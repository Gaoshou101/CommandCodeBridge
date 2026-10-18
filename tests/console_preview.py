"""Keep an isolated CPA console with synthetic data open for browser QA.

Set CPA_BINARY and BRIDGE_LIBRARY, then run with --ready-file under dist/.
No real credentials or external model requests are used. Press Enter to stop.
"""
import argparse
import json
import pathlib
from datetime import datetime, timedelta, timezone

from cpa_harness import API, CPAHost, chat_model
from cpa_round2 import EvidenceUpstream


class PreviewUpstream(EvidenceUpstream):
    fail_usage = False

    def do_GET(self):
        if self.fail_usage and any(path in self.path for path in ["/billing/", "/usage/summary"]):
            return self.send_json({"error": {"message": "fixture usage refresh unavailable"}}, 503)
        return super().do_GET()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ready-file", type=pathlib.Path, required=True)
    parser.add_argument("--scenario", choices=["normal", "empty", "single", "dense", "recovery"], default="normal")
    args = parser.parse_args()
    PreviewUpstream.outcomes = {
        "denied": (403, "MODEL_NOT_IN_PLAN"),
        "generic": (403, "permission_error"),
        "rate": (429, "rate_limit_error"),
    }
    with CPAHost(PreviewUpstream) as host:
        cid = spare = None
        if args.scenario != "empty":
            cid = host.credential(label="日常开发账号")
            spare = host.credential("fixture-spare-key", "备用账号 · 已停用", 0)
            host.call(API + "/credentials?id=" + spare, {"disabled": True}, method="PUT")
            host.models([chat_model(name) for name in ["mock-chat", "denied", "generic", "rate", "unverified", "workspace/very-long-model-name-for-responsive-layout-and-evidence-review"]])
            host.call(API + "/credentials/usage?id=" + cid)
            for model, code in [("denied", 403), ("generic", 403), ("rate", 429)]:
                host.error(API + "/models/test", {"credential_id": cid, "model": model}, code)
            for _ in range(3):
                host.call(API + "/models/test", {"credential_id": cid, "model": "mock-chat"})
            host.call("/v1/chat/completions", {"model": "mock-chat", "messages": [{"role": "user", "content": "fixture"}]}, management=False)
        if args.scenario == "empty":
            host.models([])
        elif args.scenario == "single":
            host.call(API + "/credentials?id=" + spare, method="DELETE")
            host.models([chat_model(name) for name in ["mock-chat", "generic", "unverified"]])
        elif args.scenario == "dense":
            for i in range(18):
                host.credential("fixture-dense-key-" + str(i), "开发团队 · 相似长备注 · 工作区 " + str(i), 0)
            host.models([chat_model("workspace/shared-model-name-with-long-alias-" + str(i)) for i in range(100)])
        elif args.scenario == "recovery":
            host.wait(lambda: not host.call(API + "/status")["storage_pending"], "completed snapshots")
            host.stop()
            usage_path = host.data / "usage.json"
            usage = json.loads(usage_path.read_text(encoding="utf-8"))
            for item in usage.values():
                item["value"].update(status="error", error="模拟刷新失败：保留旧额度", updated_at=(datetime.now(timezone.utc)-timedelta(days=2)).isoformat())
                for window in item["value"]["windows"]:
                    window["cap"] = None
            usage_path.write_text(json.dumps(usage), encoding="utf-8")
            logs_path = host.data / "requests.json"
            logs = json.loads(logs_path.read_text(encoding="utf-8"))
            for log in logs:
                log["time"] = (datetime.now(timezone.utc)-timedelta(days=2)).isoformat()
                if log.get("error"):
                    log["error"] = "模拟长错误 <script>不可执行</script> " * 300
            logs_path.write_text(json.dumps(logs), encoding="utf-8")
            (host.data / "statistics.json").write_text('{"truncated":', encoding="utf-8")
            PreviewUpstream.fail_usage = True
            host.start()
        ready = {"base": host.base, "console": host.base + "/v0/resource/plugins/commandcodebridge/console", "credential": cid, "spare": spare, "scenario": args.scenario}
        args.ready_file.parent.mkdir(parents=True, exist_ok=True)
        args.ready_file.write_text(json.dumps(ready), encoding="utf-8")
        print(json.dumps(ready), flush=True)
        try:
            input("Press Enter to stop the isolated fixture.\n")
        except (EOFError, KeyboardInterrupt):
            pass


if __name__ == "__main__":
    main()
