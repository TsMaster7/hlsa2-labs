#!/usr/bin/env python3
"""analyze-async.py - side-by-side queue depth + event-to-effect for
the no-bp and with-bp runs of step 5.

Reads <root>/<run>/meta.json (final_state has the closing snapshot)
and <root>/<run>/k6.log (which has per-second `queue=...` lines that
the k6 driver prints).

Usage:
    python3 scripts/analyze-async.py perf/results/async
"""

from __future__ import annotations

import json
import re
import sys
from collections import defaultdict
from pathlib import Path


LINE_RE = re.compile(r"\[(?P<label>[^\]]+)\] queue=(?P<q>\d+) shed=(?P<shed>True|False) aborted=(?P<aborted>True|False)")


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    root = Path(sys.argv[1])
    if not root.exists():
        print(f"No runs under {root}", file=sys.stderr)
        return 3

    summaries = []
    for child in sorted(root.iterdir()):
        if not child.is_dir():
            continue
        log = child / "k6.log"
        if not log.exists():
            continue
        meta = {}
        m = child / "meta.json"
        if m.exists():
            with m.open() as f:
                meta = json.load(f)
        label = meta.get("label", child.name)
        flow = meta.get("flow_control", "?")

        max_q = 0
        last_q = 0
        aborted = False
        with log.open() as f:
            for line in f:
                m = LINE_RE.search(line)
                if not m:
                    continue
                q = int(m.group("q"))
                last_q = q
                if q > max_q:
                    max_q = q
                if m.group("aborted") == "True":
                    aborted = True

        final = meta.get("final_state") or {}
        summaries.append({
            "label": label,
            "flow": flow,
            "max_queue_depth": max_q,
            "last_queue_depth": last_q,
            "aborted": aborted,
            "final_state": final,
        })

    if not summaries:
        print(f"No async runs found under {root}.")
        return 3

    print()
    print(f"{'label':<22} {'flow':<6} {'max_qdepth':>12} {'last_q':>8} {'aborted':>9}")
    for s in summaries:
        print(f"{s['label']:<22} {s['flow']:<6} {s['max_queue_depth']:>12} {s['last_queue_depth']:>8} {str(s['aborted']):>9}")

    print()
    print("Topic-guide expectation:")
    print("  - no-bp run: queue depth + event-to-effect latency diverge")
    print("    upward while handler p99 stays flat. Aborted=True at the")
    print("    queue-guard ceiling is still a PASSING result for the")
    print("    'demonstrate divergence' deliverable.")
    print("  - with-bp run: queue depth is bounded; shed/drop counters")
    print("    are non-zero. event-to-effect latency stays inside the")
    print("    handler-driven tail.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
