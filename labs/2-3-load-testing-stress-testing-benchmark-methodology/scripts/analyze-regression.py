#!/usr/bin/env python3
"""Three-run regression analyzer.

Reads k6 summary exports from the baseline and candidate run
directories, computes per-endpoint median percentiles and run-to-run
sigma, then applies the |Δp99| > 2 * max(σ_base, σ_cand) decision rule
from the homework.

Usage:
    python3 scripts/analyze-regression.py perf/results/baseline perf/results/candidate

Each input directory is expected to contain >= 3 sub-directories, each
with a `summary-open-loop.json` produced by `k6 run --summary-export`.
The k6 summary nests trends under `metrics.<name>.values.<stat>`. We
slice http_req_duration by the sub-metric submetric name pattern
`http_req_duration{endpoint:fast,phase:measured}` etc.
"""

from __future__ import annotations

import json
import math
import sys
from pathlib import Path
from statistics import median, pstdev


ENDPOINTS = ("fast", "medium", "slow")
PHASE = "measured"


def find_summaries(root: Path) -> list[Path]:
    out: list[Path] = []
    for child in sorted(root.iterdir()):
        if not child.is_dir():
            continue
        s = child / "summary-open-loop.json"
        if s.exists():
            out.append(s)
    return out


def submetric_key(endpoint: str) -> str:
    # k6 stores submetrics as `http_req_duration{endpoint:fast,phase:measured}`
    return f"http_req_duration{{endpoint:{endpoint},phase:{PHASE}}}"


def extract_percentiles(summary: dict, endpoint: str) -> dict[str, float] | None:
    metrics = summary.get("metrics") or {}
    key = submetric_key(endpoint)
    if key not in metrics:
        # Newer k6 versions key submetrics by tag-only:
        for k in metrics:
            if k.startswith("http_req_duration") and f"endpoint:{endpoint}" in k and f"phase:{PHASE}" in k:
                key = k
                break
        else:
            return None
    values = metrics[key].get("values") or {}
    return {
        "p50":  values.get("med",   values.get("p(50)")),
        "p95":  values.get("p(95)"),
        "p99":  values.get("p(99)"),
        "p999": values.get("p(99.9)"),
    }


def percentiles_per_run(run_paths: list[Path], endpoint: str) -> list[dict[str, float]]:
    out = []
    for p in run_paths:
        with p.open() as f:
            summary = json.load(f)
        pct = extract_percentiles(summary, endpoint)
        if pct is None:
            print(f"WARN: no http_req_duration submetric for endpoint={endpoint} in {p}", file=sys.stderr)
            continue
        if any(v is None for v in pct.values()):
            print(f"WARN: missing percentile in {p}: {pct}", file=sys.stderr)
        out.append({k: float(v) if v is not None else math.nan for k, v in pct.items()})
    return out


def aggregate(runs: list[dict[str, float]]) -> dict[str, tuple[float, float]]:
    """Return {percentile: (median across runs, population sigma across runs)}."""
    out = {}
    for pct in ("p50", "p95", "p99", "p999"):
        values = [r[pct] for r in runs if not math.isnan(r.get(pct, math.nan))]
        if not values:
            out[pct] = (math.nan, math.nan)
            continue
        med = median(values)
        sigma = pstdev(values) if len(values) > 1 else 0.0
        out[pct] = (med, sigma)
    return out


def fmt_ms(ms: float) -> str:
    if math.isnan(ms):
        return "    n/a"
    return f"{ms:7.2f}"


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2

    baseline_dir = Path(sys.argv[1])
    candidate_dir = Path(sys.argv[2])
    baseline_runs = find_summaries(baseline_dir)
    candidate_runs = find_summaries(candidate_dir)

    if len(baseline_runs) < 3:
        print(f"ERROR: baseline needs >= 3 runs, found {len(baseline_runs)} under {baseline_dir}", file=sys.stderr)
        return 3
    if len(candidate_runs) < 3:
        print(f"ERROR: candidate needs >= 3 runs, found {len(candidate_runs)} under {candidate_dir}", file=sys.stderr)
        return 3

    print(f"\nThree-run regression analysis")
    print(f"  baseline runs:  {len(baseline_runs)} under {baseline_dir}")
    print(f"  candidate runs: {len(candidate_runs)} under {candidate_dir}")

    overall_decision = "PASS"
    for endpoint in ENDPOINTS:
        b = aggregate(percentiles_per_run(baseline_runs, endpoint))
        c = aggregate(percentiles_per_run(candidate_runs, endpoint))

        b_p99_med, b_p99_sigma = b["p99"]
        c_p99_med, c_p99_sigma = c["p99"]
        delta = c_p99_med - b_p99_med
        threshold = 2.0 * max(b_p99_sigma, c_p99_sigma)

        if math.isnan(delta) or math.isnan(threshold):
            decision = "INSUFFICIENT-DATA"
        elif abs(delta) > threshold and delta < 0:
            decision = "IMPROVED (>2 sigma)"
        elif abs(delta) > threshold and delta > 0:
            decision = "REGRESSED (>2 sigma)"
            overall_decision = "INVESTIGATE"
        else:
            decision = "noise (within 2 sigma)"

        print()
        print(f"  endpoint = {endpoint}  (units: ms)")
        print(f"    {'percentile':<8} {'baseline':>10} {'sigma':>8}    {'candidate':>10} {'sigma':>8}")
        for pct in ("p50", "p95", "p99", "p999"):
            bm, bs = b[pct]
            cm, cs = c[pct]
            print(f"    {pct:<8} {fmt_ms(bm):>10} {fmt_ms(bs):>8}    {fmt_ms(cm):>10} {fmt_ms(cs):>8}")
        print(f"    delta_p99    = {fmt_ms(delta)} ms")
        print(f"    2*max(sigma) = {fmt_ms(threshold)} ms")
        print(f"    decision     = {decision}")

    print()
    print(f"OVERALL: {overall_decision}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
