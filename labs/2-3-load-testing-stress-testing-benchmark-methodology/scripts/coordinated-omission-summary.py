#!/usr/bin/env python3
"""Side-by-side summary of the closed-loop and open-loop runs.

Reads the most recent `summary-closed-loop.json` and
`summary-open-loop.json` under perf/results/coordinated-omission/
(or the path you pass) and prints per-endpoint p99 for both, plus
the open/closed ratio. The homework requires that ratio to be >=5x
for at least one endpoint.

Usage:
    python3 scripts/coordinated-omission-summary.py perf/results/coordinated-omission
"""

from __future__ import annotations

import json
import math
import sys
from pathlib import Path

ENDPOINTS = ("fast", "medium", "slow")


def latest(parent: Path, summary_name: str) -> Path | None:
    candidates = []
    for d in sorted(parent.iterdir(), reverse=True):
        if not d.is_dir():
            continue
        s = d / summary_name
        if s.exists():
            candidates.append(s)
    return candidates[0] if candidates else None


def percentile(summary_path: Path, endpoint: str, pct_key: str) -> float:
    with summary_path.open() as f:
        summary = json.load(f)
    metrics = summary.get("metrics") or {}
    for k, v in metrics.items():
        if not k.startswith("http_req_duration"):
            continue
        if f"endpoint:{endpoint}" not in k:
            continue
        # Closed-loop tags phase=measured too; if neither is present we accept.
        return float((v.get("values") or {}).get(pct_key, math.nan))
    return math.nan


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2

    root = Path(sys.argv[1])
    closed = latest(root, "summary-closed-loop.json")
    openl = latest(root, "summary-open-loop.json")
    if closed is None or openl is None:
        print(f"ERROR: need both summary-closed-loop.json and summary-open-loop.json under {root}", file=sys.stderr)
        print(f"  closed-loop: {closed}", file=sys.stderr)
        print(f"  open-loop:   {openl}", file=sys.stderr)
        return 3

    print(f"\nCoordinated-omission comparison")
    print(f"  closed-loop summary: {closed}")
    print(f"  open-loop   summary: {openl}")
    print()
    print(f"  {'endpoint':<8} {'closed p99':>12} {'open p99':>12} {'ratio (open/closed)':>22}")

    biggest_ratio = 0.0
    for ep in ENDPOINTS:
        c = percentile(closed, ep, "p(99)")
        o = percentile(openl, ep, "p(99)")
        if math.isnan(c) or math.isnan(o) or c <= 0:
            ratio = math.nan
        else:
            ratio = o / c
        biggest_ratio = max(biggest_ratio, ratio if not math.isnan(ratio) else 0.0)
        c_s = "n/a" if math.isnan(c) else f"{c:.2f} ms"
        o_s = "n/a" if math.isnan(o) else f"{o:.2f} ms"
        r_s = "n/a" if math.isnan(ratio) else f"{ratio:.2f}x"
        print(f"  {ep:<8} {c_s:>12} {o_s:>12} {r_s:>22}")

    print()
    if biggest_ratio >= 5.0:
        print(f"PASS: peak ratio {biggest_ratio:.2f}x >= 5x. Coordinated omission successfully demonstrated.")
        return 0
    else:
        print(f"FAIL: peak ratio {biggest_ratio:.2f}x < 5x. See homework Step 4 'If you get stuck':")
        print(f"      - Increase STALL_DURATION_MS to 10000 on the downstream.")
        print(f"      - Decrease STALL_EVERY_N (more frequent stalls) or increase TARGET_RPS.")
        print(f"      - For closed-loop, set K6_VUS=1 so the stall blocks the only generator.")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
