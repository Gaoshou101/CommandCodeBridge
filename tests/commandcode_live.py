"""Explicit real-account validation in an isolated local CPA host.

Credentials are read from a JSON file outside the repository. Read-only by
default; --generate enables at most eight short model requests. Never run in CI.
"""
import argparse
import json
import pathlib
import urllib.error
import urllib.request

from cpa_e2e import ROOT
from cpa_harness import API, CPAHost


def run(args):
    path = args.credential_file.resolve()
    if path.is_relative_to(ROOT.resolve()):
        raise ValueError("Credential file must be outside the repository")
    key = json.loads(path.read_text(encoding="utf-8"))["api_key"]
    if not isinstance(key, str) or not key.strip():
        raise ValueError("Credential file has no API key")
    proxy = urllib.request.getproxies().get("https") if args.system_proxy else None
    with CPAHost(base_url="https://api.commandcode.ai", timeout=120, proxy_url=proxy) as host:
        cid = host.credential(key, "live verification account")
        if not args.skip_usage:
            usage = host.call(API + "/credentials/usage?id=" + cid + "&refresh=1")
            assert usage["status"] == "ok", {k: usage.get(k) for k in ("status", "unavailable", "error")}
            assert usage["remaining_credits"] is not None and usage["account_requests"] is not None
            print(json.dumps({"check": "account", "status": "PASS", "plan": usage["plan"],
                              "balance_recognized": True, "request_count_recognized": True,
                              "windows": len(usage["windows"])}), flush=True)
        catalog = host.call(API + "/models/refresh", {})["models"]
        print(json.dumps({"check": "catalog", "status": "PASS", "models": len(catalog)}), flush=True)
        assert key not in json.dumps(host.call(API + "/credentials"))
        if not args.generate:
            return
        selected = []
        for upstream in (args.chat_model, args.messages_model):
            if upstream:
                selected.append(next(m for m in catalog if m["upstream_id"] == upstream))
        assert selected, "Specify a model when enabling generation"
        host.models(selected)
        count = 0
        if args.chat_model:
            alias = "commandcode/" + args.chat_model
            for stream in (False, True):
                body = {"model": alias, "messages": [{"role": "user", "content": "Reply only OK."}],
                        "max_tokens": 128, "stream": stream}
                response = host.call("/v1/chat/completions", body, management=False, stream=stream)
                assert "[DONE]" in response if stream else response["choices"][0]["message"].get("content")
                count += 1
                print(json.dumps({"check": "Chat", "stream": stream, "status": "PASS"}), flush=True)
            for stream in (False, True):
                response = host.call("/v1/responses", {"model": alias, "input": "Reply only OK.",
                    "max_output_tokens": 128, "stream": stream}, management=False, stream=stream)
                assert "response.completed" in response if stream else response["status"] == "completed", (
                    {k: response.get(k) for k in ("status", "incomplete_details", "usage")} if not stream else response[-500:])
                count += 1
                print(json.dumps({"check": "Responses", "stream": stream, "status": "PASS"}), flush=True)
            if args.tools:
                tools = [{"type": "function", "function": {"name": "ping", "description": "Return pong",
                          "parameters": {"type": "object", "properties": {}, "additionalProperties": False}}}]
                first = {"model": alias, "messages": [{"role": "user", "content": "Call ping."}],
                         "tools": tools, "tool_choice": {"type": "function", "function": {"name": "ping"}}, "max_tokens": 64}
                result = host.call("/v1/chat/completions", first, management=False)
                message = result["choices"][0]["message"]
                tool = message["tool_calls"][0]
                assert isinstance(json.loads(tool["function"]["arguments"]), dict)
                follow = {**first, "tool_choice": "auto", "messages": [*first["messages"], message,
                         {"role": "tool", "tool_call_id": tool["id"], "content": "pong"}]}
                result = host.call("/v1/chat/completions", follow, management=False)
                assert result["choices"][0]["message"].get("content")
                count += 2
                print(json.dumps({"check": "tool roundtrip", "status": "PASS"}), flush=True)
        if args.messages_model:
            for stream in (False, True):
                response = host.call("/v1/messages", {"model": "commandcode/" + args.messages_model,
                    "max_tokens": 32, "messages": [{"role": "user", "content": "Reply only OK."}],
                    "stream": stream}, management=False, stream=stream)
                assert "message_stop" in response if stream else response["type"] == "message" and response["content"]
                count += 1
                print(json.dumps({"check": "Messages", "stream": stream, "status": "PASS"}), flush=True)
        host.wait(lambda: host.call(API + "/statistics")["totals"]["requests"] == count, "live accounting")
        stats = host.call(API + "/statistics")["totals"]
        assert stats["success"] == count and stats["with_usage"] == count, stats
        assert all(row["status"] == 200 and row["usage_reported"] for row in host.logs())
        cpa_usage = []
        def accounted():
            cpa_usage.extend(host.call("/v8/management/observability/usage/queue?count=100"))
            return len(cpa_usage) >= count
        host.wait(accounted, "CPA live usage queue")
        assert len(cpa_usage) == count, len(cpa_usage)
        assert sum(row["tokens"]["total_tokens"] for row in cpa_usage) == stats["input_tokens"] + stats["output_tokens"]
        for path in host.data.glob("*.json"):
            assert key not in path.read_text(), "Upstream key persisted in plugin data"
        print(json.dumps({"check": "generation", "status": "PASS", "requests": count,
                          "protocols": ["Chat", "Responses", "Messages"] if args.messages_model else ["Chat", "Responses"],
                          "tools": args.tools, "tokens": stats["input_tokens"] + stats["output_tokens"],
                          "CPA_accounting_matches": True}), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--credential-file", type=pathlib.Path, required=True)
    parser.add_argument("--generate", action="store_true", help="Enable up to eight real short generation requests")
    parser.add_argument("--skip-usage", action="store_true", help="Test generation separately from account queries")
    parser.add_argument("--system-proxy", action="store_true", help="Apply the existing system HTTPS proxy to this isolated CPA host")
    parser.add_argument("--chat-model")
    parser.add_argument("--messages-model")
    parser.add_argument("--tools", action="store_true")
    arguments = parser.parse_args()
    try:
        run(arguments)
    except urllib.error.HTTPError as error:
        # Do not print upstream error bodies, credentials or private CPA logs.
        raise SystemExit(f"Live validation failed with HTTP {error.code}") from None
