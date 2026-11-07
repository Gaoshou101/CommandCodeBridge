"""Unsupported token counting must not cool down CommandCode inference.

CPA_BINARY=/path/to/cli-proxy-api BRIDGE_LIBRARY=/path/to/commandcodebridge.so python3 tests/cpa_count_tokens.py

Claude-compatible clients call /v1/messages/count_tokens before many requests.
CommandCode has no exact counting endpoint, so the plugin must report an
endpoint-level gap that the CPA scheduler treats as availability-neutral. Older
hosts cooled a credential down after a 501 there, turning the following
/v1/messages request into 503 auth_unavailable (CLIProxyAPI issue #7271).
Only loopback mock upstreams and fixture keys are used.
"""
import json
import urllib.error

import cpa_e2e
from cpa_harness import API, CPAHost

ROUNDS = 3


def count_tokens(host, body):
    try:
        host.call("/v1/messages/count_tokens", body, management=False)
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode("utf-8", "replace")
    raise AssertionError("count_tokens must stay unsupported instead of fabricating a token count")


def main():
    with CPAHost(cpa_e2e.MockUpstream) as host:
        host.credential()
        models = host.call(API + "/models/refresh", {})["models"]
        host.models(models)
        report = []
        for model in ("mock-claude", "mock-chat"):
            body = {"model": model, "max_tokens": 64, "messages": [{"role": "user", "content": "hello"}]}
            for attempt in range(ROUNDS):
                before = len(cpa_e2e.requests)
                status, payload = count_tokens(host, body)
                assert len(cpa_e2e.requests) == before, "count_tokens must not reach the upstream"
                assert status in (404, 501), (status, payload)
                try:
                    result = host.call("/v1/messages", body, management=False)
                except urllib.error.HTTPError as error:
                    raise AssertionError(
                        f"{model} round {attempt + 1}: /v1/messages failed with HTTP {error.code} "
                        f"after count_tokens HTTP {status}: {error.read().decode('utf-8', 'replace')[:500]}")
                assert "OK from mock" in json.dumps(result), result
                report.append({"model": model, "round": attempt + 1, "count_tokens": status, "messages": 200})
        stream = host.call("/v1/messages", {"model": "mock-claude", "max_tokens": 64, "stream": True,
            "messages": [{"role": "user", "content": "hello"}]}, management=False, stream=True)
        assert "OK from mock" in stream and "message_stop" in stream, stream
        credentials = [c for c in host.call("/v8/management/credentials")["files"] if c.get("provider") == "commandcode"]
        assert credentials and not any(c.get("unavailable") for c in credentials), credentials
        print(json.dumps({"result": "PASS", "rounds": report}, ensure_ascii=False))


if __name__ == "__main__":
    main()