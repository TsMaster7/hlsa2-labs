#!/usr/bin/env python3
"""analyze-protocols.py - aggregate REST vs gRPC bench runs.

Reads k6 summary exports from perf/results/rest/<run>/summary.json and
perf/results/grpc/<run>/summary.json. For each protocol, computes
median + population sigma across runs for p50/p95/p99/p99.9 latency,
and average bytes-on-the-wire from the *_req_bytes / *_req_duration
counters.

Usage:
    python3 scripts/analyze-protocols.py perf/results/rest perf/results/grpc
"""

from __future__ import annotations

import json
import math
import sys
from pathlib import Path
from statistics import median, pstdev


def find_summaries(root: Path) -> list[Path]:
    out: list[Path] = []
    if not root.exists():
        return out
    for child in sorted(root.iterdir()):
        if not child.is_dir():
            continue
        s = child / "summary.json"
        if s.exists():
            out.append(s)
    return out


def metric_keys(summary: dict, prefix: str) -> list[str]:
    """Return all submetric keys starting with `prefix` (e.g. http_req_duration)."""
    metrics = summary.get("metrics") or {}
    return [k for k in metrics if k.startswith(prefix)]


def measured_duration_key(summary: dict, prefix: str) -> str | None:
    """Pick the duration submetric scoped to phase:measured if present."""
    for k in metric_keys(summary, prefix):
        if "phase:measured" in k:
            return k
    # Fall back to the unscoped key.
    return prefix if prefix in (summary.get("metrics") or {}) else None


def extract_percentiles(summary: dict, prefix: str) -> dict[str, float] | None:
    key = measured_duration_key(summary, prefix)
    if key is None:
        return None
    values = (summary["metrics"][key] or {}).get("values") or {}
    return {
        "p50": values.get("med", values.get("p(50)")),
        "p95": values.get("p(95)"),
        "p99": values.get("p(99)"),
        "p999": values.get("p(99.9)"),
    }


def extract_throughput(summary: dict) -> float | None:
    """k6 emits http_reqs.values.rate and grpc_reqs.values.rate (per scenario)."""
    metrics = summary.get("metrics") or {}
    for k in ("http_reqs", "grpc_reqs"):
        if k in metrics:
            v = (metrics[k] or {}).get("values") or {}
            if v.get("rate") is not None:
                return float(v["rate"])
    return None


def runs_for(summary_paths: list[Path], duration_prefix: str) -> list[dict[str, float]]:
    out = []
    for p in summary_paths:
        with p.open() as f:
            s = json.load(f)
        pct = extract_percentiles(s, duration_prefix)
        if pct is None:
            print(f"WARN: no {duration_prefix} data in {p}", file=sys.stderr)
            continue
        thr = extract_throughput(s)
        if thr is not None:
            pct["throughput"] = thr
        out.append({k: float(v) if v is not None else math.nan for k, v in pct.items()})
    return out


def aggregate(runs: list[dict[str, float]]) -> dict[str, tuple[float, float]]:
    out = {}
    for pct in ("p50", "p95", "p99", "p999", "throughput"):
        values = [r.get(pct, math.nan) for r in runs if not math.isnan(r.get(pct, math.nan))]
        if not values:
            out[pct] = (math.nan, math.nan)
            continue
        med = median(values)
        sigma = pstdev(values) if len(values) > 1 else 0.0
        out[pct] = (med, sigma)
    return out


def fmt(v: float) -> str:
    if v is None or (isinstance(v, float) and math.isnan(v)):
        return "   n/a"
    return f"{v:7.2f}"


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    rest = Path(sys.argv[1])
    grpc = Path(sys.argv[2])

    rest_runs = runs_for(find_summaries(rest), "http_req_duration")
    grpc_runs = runs_for(find_summaries(grpc), "grpc_req_duration")

    if not rest_runs:
        print(f"ERROR: no REST runs found under {rest}", file=sys.stderr)
    if not grpc_runs:
        print(f"ERROR: no gRPC runs found under {grpc}", file=sys.stderr)
    if not rest_runs or not grpc_runs:
        return 3

    rest_agg = aggregate(rest_runs)
    grpc_agg = aggregate(grpc_runs)

    print()
    print(f"REST runs: {len(rest_runs)}    gRPC runs: {len(grpc_runs)}")
    print()
    print(f"  {'metric':<14} {'REST_med':>10} {'REST_sigma':>10}    {'gRPC_med':>10} {'gRPC_sigma':>10}    {'delta_p99':>10}")
    for pct in ("p50", "p95", "p99", "p999"):
        rm, rs = rest_agg[pct]
        gm, gs = grpc_agg[pct]
        delta = gm - rm if (not math.isnan(gm) and not math.isnan(rm)) else math.nan
        print(f"  {pct:<14} {fmt(rm)} ms  {fmt(rs)} ms   {fmt(gm)} ms  {fmt(gs)} ms   {fmt(delta) if pct == 'p99' else '          '}")
    rm, rs = rest_agg["throughput"]
    gm, gs = grpc_agg["throughput"]
    print(f"  {'throughput':<14} {fmt(rm)} req/s {fmt(rs)} req/s {fmt(gm)} req/s {fmt(gs)} req/s")

    print()
    p99_rest, p99_rest_sigma = rest_agg["p99"]
    p99_grpc, p99_grpc_sigma = grpc_agg["p99"]
    if not math.isnan(p99_rest) and not math.isnan(p99_grpc):
        threshold = 2.0 * max(p99_rest_sigma, p99_grpc_sigma)
        delta = p99_grpc - p99_rest
        if abs(delta) <= threshold:
            verdict = f"WITHIN-NOISE  (|Δp99|={abs(delta):.2f}ms <= 2σ={threshold:.2f}ms)"
        elif delta < 0:
            verdict = f"gRPC FASTER   (Δp99={delta:.2f}ms,  2σ threshold={threshold:.2f}ms)"
        else:
            verdict = f"REST FASTER   (Δp99={delta:.2f}ms,  2σ threshold={threshold:.2f}ms)"
        print(f"Verdict (p99): {verdict}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
