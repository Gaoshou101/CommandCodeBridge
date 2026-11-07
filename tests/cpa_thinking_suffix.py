"""CPA model-name thinking suffixes must work and must not cool down a model.

CPA_BINARY=/path/to/cli-proxy-api BRIDGE_LIBRARY=/path/to/commandcodebridge.so python3 tests/cpa_thinking_suffix.py

Clients may select a thinking level as "model(high)" or a budget as
"model(16384)". CLIProxyAPI v8.0.59 strips the suffix before it calls a plugin;
older hosts pass it through. Both must reach the base model with the same
reasoning settings, and native Messages requests must keep the suffix even
though the bridge rebuilds them from the original client body. Only loopback
mock upstreams and fixture keys are used.
"""
import json
import urllib.error

import cpa_e2e
from cpa_harness import API, CPAHost

MESSAGES = [{"role": "user", "content": "hello"}]


def upstream(host, path, body, stream=False):
    before = len(cpa_e2e.requests)
    try:
        result = host.call(path, body, management=False, stream=stream)
    except urllib.error.HTTPError as error:
        raise AssertionError(f"{path} {body['model']}: HTTP {error.code} {error.read().decode('utf-8', 'replace')[:500]}")
    assert "OK from mock" in (result if stream else json.dumps(result)), result
    assert len(cpa_e2e.requests) == before + 1, "expected exactly one upstream request"
    return cpa_e2e.requests[-1]


def chat(host, model, **extra):
    path, body = upstream(host, "/v1/chat/completions", {"model": model, "messages": MESSAGES, **extra})
    assert path.endswith("/chat/completions") and body["model"] == "mock-chat", (path, body)
    return body.get("reasoning_effort")


def claude(host, model, path="/v1/messages", stream=False, **extra):
    request = {"model": model, "max_tokens": 64, "messages": MESSAGES, "stream": stream, **extra}
    sent_path, body = upstream(host, path, request, stream=stream)
    assert sent_path.endswith("/messages") and body["model"] == "mock-claude", (sent_path, body)
    return body.get("thinking"), (body.get("output_config") or {}).get("effort")


def main():
    with CPAHost(cpa_e2e.MockUpstream) as host:
        host.credential()
        host.models(host.call(API + "/models/refresh", {})["models"])
        assert chat(host, "mock-chat") is None
        expected = {"high": "high", "HIGH": "high", "none": "none", "0": "none", "512": "minimal",
                    "1024": "low", "8192": "medium", "24576": "high", "24577": "xhigh", "auto": None, "-1": None, "bogus": None}
        for suffix, effort in expected.items():
            assert chat(host, f"mock-chat({suffix})") == effort, suffix
        assert chat(host, "mock-chat(low)", reasoning_effort="high") == "low", "suffix overrides the body"
        path, body = upstream(host, "/v1/responses", {"model": "mock-chat(low)", "input": "hello"})
        assert body["model"] == "mock-chat" and body.get("reasoning_effort") == "low", (path, body)
        for chat_path in (True, False):
            route = "/v1/chat/completions" if chat_path else "/v1/messages"
            extra = {"max_tokens": 64} if chat_path else {}
            assert claude(host, "mock-claude(high)", route, **extra) == ({"type": "adaptive"}, "high"), route
            assert claude(host, "mock-claude(none)", route, **extra) == ({"type": "disabled"}, None), route
            assert claude(host, "mock-claude(auto)", route, **extra) == (None, None), route
        assert claude(host, "mock-claude(16000)", max_tokens=32000) == ({"type": "adaptive"}, "high")
        assert claude(host, "mock-claude(low)", thinking={"type": "adaptive"}, output_config={"effort": "max"}) == ({"type": "adaptive"}, "low")
        assert claude(host, "mock-claude(medium)", stream=True) == ({"type": "adaptive"}, "medium")
        # Unsuffixed requests after every suffixed one prove that no model was cooled down.
        assert chat(host, "mock-chat", reasoning_effort="low") == "low"
        assert claude(host, "mock-claude", thinking={"type": "adaptive"}) == ({"type": "adaptive"}, None)
        upstream(host, "/v1/responses", {"model": "mock-chat", "input": "hello"})
        credentials = [c for c in host.call("/v8/management/credentials")["files"] if c.get("provider") == "commandcode"]
        assert credentials and not any(c.get("unavailable") for c in credentials), credentials
        print("PASS: thinking suffixes on Chat, Responses and native Messages; no model cooldown")


if __name__ == "__main__":
    main()