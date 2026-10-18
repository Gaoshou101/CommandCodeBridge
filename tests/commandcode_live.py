"""Explicit isolated live validation. Default is read only; never run in CI.

--dry-run prints the request budget without credentials or network requests.
The report contains no prompts, response content, signatures or account IDs.
"""
import argparse
import base64
import json
import pathlib
import struct
import urllib.error
import urllib.request
import zlib
from datetime import datetime, timezone

from cpa_e2e import ROOT
from cpa_harness import API, CPAHost


def image_fixture():
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(b"\x00\xff\x00\x00")) + chunk(b"IEND", b"")
    return base64.b64encode(png).decode()


def generation_plan(args):
    if not 1 <= args.max_requests <= 20 or not 32 <= args.max_tokens <= 8192:
        raise ValueError("Request/token limits are outside the supported bounds")
    if any((args.image, args.thinking)) and not args.messages_model:
        raise ValueError("Image/thinking checks require --messages-model")
    if args.thinking and args.max_tokens < 2048:
        raise ValueError("Thinking requires an explicit --max-tokens of at least 2048")
    cases = []
    if args.chat_model:
        cases += [("Chat", args.chat_model, 2), ("Responses", args.chat_model, 2)]
        if args.tools:
            cases += [("Chat tools", args.chat_model, 2)]
    if args.messages_model:
        cases += [("Messages", args.messages_model, 2)]
        if args.tools:
            cases += [("Messages tools", args.messages_model, 2)]
        if args.image:
            cases += [("Messages image", args.messages_model, 1)]
        if args.thinking:
            cases += [("thinking signature replay", args.messages_model, 2)]
    count = sum(case[2] for case in cases)
    if args.generate and (not count or count > args.max_requests):
        raise ValueError("Selected cases exceed --max-requests or no model was selected")
    return {"cases": [{"check": c, "model": m, "requests": n} for c, m, n in cases],
            "generation_enabled": args.generate, "planned_requests": count if args.generate else 0,
            "maximum_output_tokens": count * args.max_tokens if args.generate else 0,
            "max_requests": args.max_requests, "max_tokens_per_request": args.max_tokens,
            "zdr_requested": args.zdr, "retention_policy": "unverified"}


def run(args):
    plan = generation_plan(args)
    report = {"checked_at": datetime.now(timezone.utc).isoformat(), "plan": plan,
              "status": "DRY_RUN" if args.dry_run else "RUNNING", "results": []}
    args.result = report
    if args.dry_run:
        return report
    if not args.credential_file:
        raise ValueError("--credential-file is required outside dry-run")
    path = args.credential_file.resolve()
    if path.is_relative_to(ROOT.resolve()):
        raise ValueError("Credential file must be outside the repository")
    key = json.loads(path.read_text(encoding="utf-8"))["api_key"]
    if not isinstance(key, str) or not key.strip():
        raise ValueError("Credential file has no API key")
    proxy = urllib.request.getproxies().get("https") if args.system_proxy else None
    with CPAHost(base_url="https://api.commandcode.ai", timeout=120, proxy_url=proxy) as host:
        cid = host.credential(key, "live verification account")
        host.call(API + "/config", {"zdr": args.zdr}, method="PUT")
        if not args.skip_usage:
            usage = host.call(API + "/credentials/usage?id=" + cid + "&refresh=1")
            assert usage["status"] == "ok" and usage["remaining_credits"] is not None
            assert usage["account_requests"] is not None
            report["account"] = {"plan": usage["plan"], "windows": len(usage["windows"]),
                                 "identity_sharing": "unverified"}
        catalog = host.call(API + "/models/refresh", {})["models"]
        report["catalog_models"] = len(catalog)
        assert key not in json.dumps(host.call(API + "/credentials"))
        if not args.generate:
            report["status"] = "PASS_READ_ONLY"
            return report
        selected = [next(m for m in catalog if m["upstream_id"] == upstream)
                    for upstream in dict.fromkeys(filter(None, (args.chat_model, args.messages_model)))]
        assert len({m["id"] for m in selected}) == len(selected)
        host.models(selected)
        aliases = {m["upstream_id"]: m["id"] for m in selected}
        count = 0

        def call(check, upstream, path, body, stream=False):
            nonlocal count
            assert count < args.max_requests
            count += 1
            result = host.call(path, {**body, "model": aliases[upstream], "stream": stream},
                               management=False, stream=stream)
            if stream:
                assert ("message_stop" if path.endswith("messages") else
                        "response.completed" if path.endswith("responses") else "[DONE]") in result
            elif path.endswith("messages"):
                assert result["type"] == "message" and result["content"]
            elif path.endswith("responses"):
                assert result["status"] == "completed"
            else:
                assert result["choices"][0]["message"]
            model = next(m for m in selected if m["upstream_id"] == upstream)
            report["results"].append({"check": check, "model": upstream, "status": "PASS",
                "client_protocol": path, "upstream_protocol": "messages" if "/messages" in model["supported_endpoints"] and "/chat/completions" not in model["supported_endpoints"] else "chat-completions",
                "stream": stream, "zdr_requested": args.zdr,
                "upstream_accepted": True, "retention_policy": "unverified"})
            return result

        prompt = [{"role": "user", "content": "Reply only OK."}]
        if args.chat_model:
            for stream in (False, True):
                call("Chat", args.chat_model, "/v1/chat/completions",
                     {"messages": prompt, "max_tokens": args.max_tokens}, stream)
                call("Responses", args.chat_model, "/v1/responses",
                     {"input": "Reply only OK.", "max_output_tokens": args.max_tokens}, stream)
            if args.tools:
                tool = {"type": "function", "function": {"name": "read_file", "description": "Read the synthetic a.txt fixture",
                        "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"], "additionalProperties": False}}}
                first = {"messages": [{"role": "user", "content": "Call read_file with path a.txt."}], "tools": [tool],
                         "tool_choice": {"type": "function", "function": {"name": "read_file"}}, "max_tokens": args.max_tokens}
                result = call("Chat tool call", args.chat_model, "/v1/chat/completions", first)
                message = result["choices"][0]["message"]
                tc = message["tool_calls"][0]
                assert tc["function"]["name"] == "read_file" and json.loads(tc["function"]["arguments"]).get("path") == "a.txt"
                call("Chat tool result", args.chat_model, "/v1/chat/completions", {**first, "tool_choice": "auto",
                     "messages": [*first["messages"], message, {"role": "tool", "tool_call_id": tc["id"], "content": "pong"}]})
        if args.messages_model:
            base = {"messages": prompt, "max_tokens": args.max_tokens}
            for stream in (False, True):
                call("Messages", args.messages_model, "/v1/messages", base, stream)
            if args.tools:
                first = {**base, "messages": [{"role": "user", "content": "Call read_file with path a.txt."}],
                         "tools": [{"name": "read_file", "description": "Read the synthetic a.txt fixture", "input_schema": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}],
                         "tool_choice": {"type": "tool", "name": "read_file"}}
                result = call("Messages tool call", args.messages_model, "/v1/messages", first)
                tc = next(block for block in result["content"] if block["type"] == "tool_use")
                assert tc["name"] == "read_file" and tc["input"].get("path") == "a.txt"
                call("Messages tool result", args.messages_model, "/v1/messages", {**first, "tool_choice": {"type": "auto"},
                    "messages": [*first["messages"], {"role": "assistant", "content": result["content"]},
                                 {"role": "user", "content": [{"type": "tool_result", "tool_use_id": tc["id"], "content": "pong"}]}]})
            if args.image:
                call("Messages image transport", args.messages_model, "/v1/messages", {**base,
                    "messages": [{"role": "user", "content": [{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": image_fixture()}},
                                 {"type": "text", "text": "Describe this image briefly."}]}]})
            if args.thinking:
                thinking = {"type": "enabled", "budget_tokens": 1024} if args.thinking_mode == "budget" else {"type": "adaptive"}
                first = {**base, "thinking": thinking}
                result = call("thinking signature", args.messages_model, "/v1/messages", first)
                assert any(b["type"] == "thinking" and b.get("signature") for b in result["content"])
                call("thinking signature replay", args.messages_model, "/v1/messages", {**first,
                    "messages": [*prompt, {"role": "assistant", "content": result["content"]}, {"role": "user", "content": "Reply only OK again."}]})
        assert count == plan["planned_requests"]
        host.wait(lambda: host.call(API + "/statistics")["totals"]["requests"] == count, "live accounting")
        stats = host.call(API + "/statistics")["totals"]
        assert stats["success"] == count and stats["with_usage"] == count
        logs = host.logs()
        assert len(logs) == count and all(r["source"] == "cpa_request" and r["status"] == 200 and r["usage_reported"] for r in logs)
        assert all(r["zdr_requested"] == args.zdr and r["client_protocol"] in ("messages", "responses", "chat-completions") for r in logs)
        if args.chat_model:
            assert sum(r["client_protocol"] == "responses" for r in logs) == 2
        cpa_usage = []
        def accounted():
            cpa_usage.extend(host.call("/v8/management/observability/usage/queue?count=100"))
            return len(cpa_usage) >= count
        host.wait(accounted, "CPA live usage queue")
        assert len(cpa_usage) == count
        assert sum(r["tokens"]["total_tokens"] for r in cpa_usage) == stats["input_tokens"] + stats["output_tokens"]
        for file in host.data.glob("*.json*"):
            assert key not in file.read_text(), "Credential persisted in runtime data"
        report.update(status="PASS", requests=count, tokens=stats["input_tokens"] + stats["output_tokens"], CPA_accounting_matches=True)
        return report


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--credential-file", type=pathlib.Path)
    p.add_argument("--report", type=pathlib.Path)
    for flag in ("generate", "dry-run", "skip-usage", "system-proxy", "tools", "image", "thinking", "zdr"):
        p.add_argument("--" + flag, action="store_true")
    p.add_argument("--chat-model")
    p.add_argument("--messages-model")
    p.add_argument("--thinking-mode", choices=("adaptive", "budget"), default="adaptive")
    p.add_argument("--max-requests", type=int, default=8)
    p.add_argument("--max-tokens", type=int, default=128)
    return p


if __name__ == "__main__":
    args = parser().parse_args()
    if args.report and args.credential_file and args.report.resolve() == args.credential_file.resolve():
        raise SystemExit("Report must not overwrite the credential file")
    try:
        result = run(args)
    except (Exception,) as error:
        # Never print upstream bodies, response blocks, private paths or CPA logs.
        result = getattr(args, "result", {})
        result.update(status="FAIL", error_type=type(error).__name__)
        if isinstance(error, urllib.error.HTTPError):
            result["http_status"] = error.code
        elif isinstance(error, ValueError):
            result["error"] = str(error) if str(error).startswith(("Request/", "Image/", "Thinking ", "Selected ", "--credential", "Credential file")) else "Invalid input"
    encoded = json.dumps(result, ensure_ascii=False, indent=2)
    if args.report:
        args.report.write_text(encoded + "\n", encoding="utf-8")
    print(encoded)
    raise SystemExit(1 if result["status"] == "FAIL" else 0)
