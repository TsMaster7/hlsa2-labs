#!/usr/bin/env python3
"""analyze-sync-chain.py - compare healthy vs faulted sync-chain runs.

Reads each <root>/<run>/ directory, groups by the LABEL in
<run>/meta.json, computes per-label E2E availability + p99, and prints
the measured-vs-product availability comparison the topic guide asks
for.

Usage:
    python3 scripts/analyze-sync-chain.py perf/results/sync-chain
"""

from __future__ import annotations

import json
import math
import sys
from collections import defaultdict
from pathlib import Path


def load_summary(p: Path) -> dict:
    s = p / "summary.json"
    if not s.exists():
        return {}
    with s.open() as f:
        return json.load(f)


def load_meta(p: Path) -> dict:
    m = p / "meta.json"
    if not m.exists():
        return {}
    with m.open() as f:
        return json.load(f)


def extract(summary: dict) -> dict[str, float]:
    """k6 summary -> {p99_ms, fail_rate, throughput}."""
    metrics = summary.get("metrics") or {}

    # Phase-measured request duration.
    dur_key = None
    for k in metrics:
        if k.startswith("http_req_duration") and "phase:measured" in k:
            dur_key = k
            break
    if dur_key is None and "http_req_duration" in metrics:
        dur_key = "http_req_duration"

    p99 = math.nan
    if dur_key:
        v = (metrics[dur_key] or {}).get("values") or {}
        if v.get("p(99)") is not None:
            p99 = float(v["p(99)"])

    # Failure rate.
    fail = math.nan
    for k in metrics:
        if k.startswith("http_req_failed") and "phase:measured" in k:
            v = (metrics[k] or {}).get("values") or {}
            if v.get("rate") is not None:
                fail = float(v["rate"])
                break

    return {"p99_ms": p99, "fail_rate": fail}


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    root = Path(sys.argv[1])
    if not root.exists():
        print(f"No runs under {root}", file=sys.stderr)
        return 3

    groups: dict[str, list[dict]] = defaultdict(list)
    for child in sorted(root.iterdir()):
        if not child.is_dir():
            continue
        meta = load_meta(child)
        summary = load_summary(child)
        if not summary:
            continue
        label = meta.get("label") or child.name
        cb = meta.get("circuit_breaker", "off")
        key = f"{label} (cb={cb})"
        groups[key].append({"path": str(child), **extract(summary)})

    if not groups:
        print(f"No sync-chain runs found in {root}.")
        return 3

    print()
    print("Sync-chain runs (E2E view from k6):")
    print(f"  {'group':<40} {'p99_ms':>10} {'success_rate':>14} {'runs':>5}")
    for k in sorted(groups):
        rows = groups[k]
        valid = [r for r in rows if not math.isnan(r["p99_ms"])]
        if not valid:
            continue
        p99 = sum(r["p99_ms"] for r in valid) / len(valid)
        sr = sum(1 - r["fail_rate"] for r in valid if not math.isnan(r["fail_rate"])) / len(valid)
        print(f"  {k:<40} {p99:>10.2f} {sr*100:>13.2f}% {len(valid):>5}")

    print()
    print("Topic-guide reminder: measured success_rate should be CLOSE to")
    print("the product of per-hop success rates (PromQL: lab32:hop_success_rate).")
    print("Read those gauges from Prometheus while the run is in flight - the")
    print("multiplication is the empirical 0.999 * 0.999 * 0.999 the topic argues for.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
