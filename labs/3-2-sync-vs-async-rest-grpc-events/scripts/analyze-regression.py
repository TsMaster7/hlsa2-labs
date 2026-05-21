#!/usr/bin/env python3
"""analyze-regression.py - 3-run regression decision for the targeted
improvement.

Reads baseline + candidate directories of k6 summary.json files,
computes per-percentile median + sigma, then applies the
|delta_p99| > 2 * max(sigma_base, sigma_cand) rule from the topic guide.

The same script handles both /sync-chain and /async candidates - it
auto-detects whether each summary has http_req_duration or
grpc_req_duration data.

Usage:
    python3 scripts/analyze-regression.py <baseline-dir> <candidate-dir>
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


def measured_duration_key(metrics: dict, prefix: str) -> str | None:
    for k in metrics:
        if k.startswith(prefix) and "phase:measured" in k:
            return k
    return prefix if prefix in metrics else None


def percentiles(summary: dict) -> dict[str, float] | None:
    metrics = summary.get("metrics") or {}
    key = measured_duration_key(metrics, "http_req_duration")
    if key is None:
        key = measured_duration_key(metrics, "grpc_req_duration")
    if key is None:
        return None
    v = (metrics[key] or {}).get("values") or {}
    return {
        "p50": v.get("med", v.get("p(50)")),
        "p95": v.get("p(95)"),
        "p99": v.get("p(99)"),
        "p999": v.get("p(99.9)"),
    }


def runs_for(paths: list[Path]) -> list[dict[str, float]]:
    out = []
    for p in paths:
        with p.open() as f:
            s = json.load(f)
        pct = percentiles(s)
        if pct is None:
            print(f"WARN: {p} has no duration histogram", file=sys.stderr)
            continue
        out.append({k: float(v) if v is not None else math.nan for k, v in pct.items()})
    return out


def aggregate(runs: list[dict[str, float]]) -> dict[str, tuple[float, float]]:
    out = {}
    for pct in ("p50", "p95", "p99", "p999"):
        values = [r[pct] for r in runs if not math.isnan(r.get(pct, math.nan))]
        if not values:
            out[pct] = (math.nan, math.nan)
            continue
        out[pct] = (median(values), pstdev(values) if len(values) > 1 else 0.0)
    return out


def fmt(v: float) -> str:
    if v is None or (isinstance(v, float) and math.isnan(v)):
        return "   n/a"
    return f"{v:7.2f}"


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    base = Path(sys.argv[1])
    cand = Path(sys.argv[2])

    base_runs = runs_for(find_summaries(base))
    cand_runs = runs_for(find_summaries(cand))

    if len(base_runs) < 2 or len(cand_runs) < 2:
        print(f"ERROR: need >=2 runs per side, got baseline={len(base_runs)} candidate={len(cand_runs)}", file=sys.stderr)
        return 3
    if len(base_runs) < 3 or len(cand_runs) < 3:
        print(f"WARN: rubric expects 3 runs per side; got baseline={len(base_runs)} candidate={len(cand_runs)}", file=sys.stderr)

    b = aggregate(base_runs)
    c = aggregate(cand_runs)

    print()
    print(f"Three-run regression:  baseline={len(base_runs)} candidate={len(cand_runs)}")
    print()
    print(f"  {'percentile':<8} {'base_med':>10} {'base_sigma':>10}    {'cand_med':>10} {'cand_sigma':>10}    {'delta':>10}")
    for pct in ("p50", "p95", "p99", "p999"):
        bm, bs = b[pct]
        cm, cs = c[pct]
        delta = cm - bm if not (math.isnan(cm) or math.isnan(bm)) else math.nan
        print(f"  {pct:<8} {fmt(bm)} ms {fmt(bs)} ms  {fmt(cm)} ms {fmt(cs)} ms  {fmt(delta)} ms")

    bm, bs = b["p99"]
    cm, cs = c["p99"]
    delta = cm - bm
    threshold = 2.0 * max(bs, cs)

    print()
    print(f"  delta_p99    = {fmt(delta)} ms")
    print(f"  2*max(sigma) = {fmt(threshold)} ms")
    if math.isnan(delta) or math.isnan(threshold):
        verdict = "INSUFFICIENT-DATA"
    elif abs(delta) <= threshold:
        verdict = "noise (within 2 sigma)"
    elif delta < 0:
        verdict = "IMPROVED (>2 sigma)"
    else:
        verdict = "REGRESSED (>2 sigma)"
    print(f"  decision     = {verdict}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
