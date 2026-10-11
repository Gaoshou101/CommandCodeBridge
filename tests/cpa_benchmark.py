"""Loopback native-host throughput baseline; emits metrics, never paid traffic."""
import argparse
import concurrent.futures
import json
import os
import pathlib
import statistics
import time

from cpa_e2e import MockUpstream
from cpa_harness import API, CPAHost, chat_body, chat_model

def proc_metrics(pid):
    proc = pathlib.Path("/proc") / str(pid)
    if not proc.is_dir():
        return {}
    stats = (proc / "stat").read_text().rsplit(")", 1)[1].split()
    io = dict(line.split(": ") for line in (proc / "io").read_text().splitlines())
    return {"cpu_seconds": (int(stats[11]) + int(stats[12])) / os.sysconf("SC_CLK_TCK"),
            "rss_bytes": int(stats[21]) * os.sysconf("SC_PAGE_SIZE"),
            "written_bytes": int(io["write_bytes"])}


def run(total, levels):
    reports = []
    for concurrency in levels:
        with CPAHost(MockUpstream) as host:
            host.credential()
            host.models([chat_model("benchmark", "mock-chat")])
            # Fill the default 1000-entry buffer before measuring disk overhead.
            sample = {"id": "seed", "model": "benchmark", "status": 200,
                      "time": "2026-10-11T00:00:00Z", "credential": "fixture account"}
            host.wait(lambda: not host.call(API + "/status").get("storage_pending", False), "initial persistence")
            host.stop()
            host.data.mkdir(exist_ok=True)
            (host.data / "requests.json").write_text(json.dumps([sample] * 1000))
            host.start()
            host.wait(lambda: host.call(API + "/status")["credentials"] == 1, "restored credential")
            baseline = proc_metrics(host.process.pid)

            def request(_):
                start = time.perf_counter()
                result = host.call("/v1/chat/completions", chat_body("benchmark"), management=False)
                assert result["choices"][0]["message"]["content"] == "OK from mock"
                return time.perf_counter() - start

            started = time.perf_counter()
            with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
                latency = sorted(pool.map(request, range(total)))
            elapsed = time.perf_counter() - started
            host.wait(lambda: not host.call(API + "/status").get("storage_pending", False), "final persistence")
            metrics = proc_metrics(host.process.pid)
            stats = host.call(API + "/statistics")["totals"]
            assert stats["requests"] == total and stats["success"] == total and stats["with_usage"] == total, stats
            report = {"concurrency": concurrency, "requests": total, "requests_per_second": round(total / elapsed, 2),
                      "median_ms": round(statistics.median(latency) * 1000, 2),
                      "p95_ms": round(latency[min(len(latency) - 1, int(len(latency) * .95))] * 1000, 2),
                      "average_ttft_ms": host.call(API + "/statistics")["average_ttft_ms"]}
            if metrics:
                report.update({"cpu_seconds": round(metrics["cpu_seconds"] - baseline["cpu_seconds"], 3),
                               "rss_bytes": metrics["rss_bytes"],
                               "written_bytes": metrics["written_bytes"] - baseline["written_bytes"]})
            reports.append(report)
            print(json.dumps(report), flush=True)
    return reports


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--requests", type=int, default=100)
    parser.add_argument("--concurrency", default="1,8,32")
    args = parser.parse_args()
    assert 1 <= args.requests <= 10000
    levels = [int(n) for n in args.concurrency.split(",")]
    assert levels and all(1 <= n <= 128 for n in levels)
    run(args.requests, levels)
