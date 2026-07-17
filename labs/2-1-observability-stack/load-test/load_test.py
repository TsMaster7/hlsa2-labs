#!/usr/bin/env python3
"""HLSA2 Lab 2-1 — open-loop load test with a saturation phase.

Why open-loop (fixed arrival rate) instead of closed-loop (fixed concurrency):
requests are dispatched on a fixed wall-clock schedule regardless of how fast
the service responds. During the saturation phase the offered load therefore
genuinely EXCEEDS service capacity, so the DB connection pool saturates,
requests time out (5xx), the error rate crosses 5%, and the provisioned Grafana
alert fires. A single-threaded closed loop cannot do this: it self-throttles to
1/latency and can never offer more load than the service can already handle.

Phases (all tunable via env, see PHASES below):
  Warm-up     light load, everything healthy
  Nominal     target operating load, below capacity
  Saturation  load exceeds capacity -> pool saturates -> 5xx -> ALERT FIRES
  Recovery    back to nominal -> alert resolves

Capacity note: with DB_POOL_SIZE=8 and ~20-50ms queries, service capacity is
~150-230 req/s. The saturation phase defaults to 400 rps to clearly exceed it.

Usage:
  pip install -r load-test/requirements.txt
  python load-test/load_test.py
Env overrides: BASE_URL, MAX_WORKERS, TIMEOUT_S, and <PHASE>_RPS / <PHASE>_S.
"""

import json
import os
import random
import statistics
import time
from concurrent.futures import ThreadPoolExecutor

import requests

BASE_URL = os.environ.get("BASE_URL", "http://localhost:18080")
TIMEOUT_S = float(os.environ.get("TIMEOUT_S", "5"))
MAX_WORKERS = int(os.environ.get("MAX_WORKERS", "200"))

# Raise the open-file limit so we can hold MAX_WORKERS concurrent sockets.
try:
    import resource
    _soft, _hard = resource.getrlimit(resource.RLIMIT_NOFILE)
    resource.setrlimit(resource.RLIMIT_NOFILE, (min(8192, _hard), _hard))
except Exception:
    pass

# Phase definitions: (name, requests/sec, duration_sec). Each is env-overridable,
# e.g. SATURATION_RPS=600 SATURATION_S=240 python load_test.py
PHASES = [
    ("Warm-up", 10, 45),
    ("Nominal", 50, 90),
    ("Saturation", 400, 210),  # >= 3 min so the `for: 2m` alert reliably fires
    ("Recovery", 50, 75),
]

session = requests.Session()  # keep-alive / connection pooling
# CRITICAL: size the connection pool to the worker count. urllib3's default
# pool of 10 would throttle offered load at the CLIENT (600 threads sharing 10
# sockets), so the server would never actually saturate. pool_block=True keeps
# real concurrency bounded to MAX_WORKERS instead of creating+discarding sockets.
_adapter = requests.adapters.HTTPAdapter(
    pool_connections=MAX_WORKERS, pool_maxsize=MAX_WORKERS, pool_block=True)
session.mount("http://", _adapter)
session.mount("https://", _adapter)


def _target():
    """Weighted mix of endpoints. All use bounded route templates so metric
    cardinality stays low (route label = /api/orders/{id} or /api/orders)."""
    roll = random.random()
    if roll < 0.8:
        return "GET", f"/api/orders/{random.randint(1, 100000)}"
    if roll < 0.9:
        return "GET", "/api/orders"
    return "POST", "/api/orders"


def _one_request():
    method, path = _target()
    t0 = time.monotonic()
    try:
        if method == "GET":
            r = session.get(BASE_URL + path, timeout=TIMEOUT_S)
        else:
            r = session.post(BASE_URL + path, timeout=TIMEOUT_S)
        return {"status": r.status_code, "duration_ms": (time.monotonic() - t0) * 1000.0}
    except Exception:
        # Client-side timeout / connection error counts as a failed request.
        return {"status": 0, "duration_ms": (time.monotonic() - t0) * 1000.0}


def run_phase(pool, name, rps, duration_s):
    print(f"\n[Phase: {name}] target {rps} rps for {duration_s}s -> {BASE_URL}", flush=True)
    interval = 1.0 / rps
    futures = []
    start = time.monotonic()
    end = start + duration_s
    next_send = start
    # Open-loop scheduler: fire on a fixed schedule; never block on responses.
    while True:
        now = time.monotonic()
        if now >= end:
            break
        if now >= next_send:
            futures.append(pool.submit(_one_request))
            next_send += interval
            if next_send < now:  # fell behind; resync without unbounded burst
                next_send = now + interval
        else:
            time.sleep(min(next_send - now, 0.005))
    results = [f.result() for f in futures]
    return summarize(name, rps, duration_s, results)


def summarize(name, rps, duration_s, results):
    n = len(results)
    stats = {"phase": name, "target_rps": rps, "duration_s": duration_s, "requests": n}
    if n == 0:
        print("  no requests sent", flush=True)
        return stats
    durations = sorted(r["duration_ms"] for r in results)
    errors = sum(1 for r in results if r["status"] == 0 or r["status"] >= 500)

    def pct(p):
        idx = min(len(durations) - 1, int(len(durations) * p))
        return durations[idx]

    stats.update({
        "achieved_rps": round(n / duration_s, 1),
        "errors": errors,
        "error_pct": round(errors / n * 100.0, 1),
        "p50_ms": round(statistics.median(durations), 1),
        "p95_ms": round(pct(0.95), 1),
        "p99_ms": round(pct(0.99), 1),
    })
    print(f"  requests: {n}  achieved: {stats['achieved_rps']} rps", flush=True)
    print(f"  errors:   {errors} ({stats['error_pct']}%)", flush=True)
    print(f"  latency:  p50 {stats['p50_ms']}ms  p95 {stats['p95_ms']}ms  p99 {stats['p99_ms']}ms", flush=True)
    return stats


def reset_injection():
    """Clear any leftover fault injection so warm-up/nominal start healthy."""
    try:
        session.post(f"{BASE_URL}/admin/inject",
                     json={"fail_ratio": 0.0, "extra_latency_ms": 0}, timeout=2)
    except Exception:
        pass


def main():
    reset_injection()
    pool = ThreadPoolExecutor(max_workers=MAX_WORKERS)
    all_stats = []
    try:
        for name, rps, dur in PHASES:
            key = name.upper().replace("-", "")
            rps = int(os.environ.get(f"{key}_RPS", rps))
            dur = int(os.environ.get(f"{key}_S", dur))
            all_stats.append(run_phase(pool, name, rps, dur))
    finally:
        pool.shutdown(wait=True)

    out = os.environ.get("RESULTS_OUT",
                         os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                      "results-summary.json"))
    try:
        with open(out, "w") as f:
            json.dump(all_stats, f, indent=2)
        print(f"\nSaved summary -> {out}", flush=True)
    except Exception as e:
        print("could not save summary:", e, flush=True)
    print("Load test complete. Grafana: http://localhost:3000", flush=True)


if __name__ == "__main__":
    main()
