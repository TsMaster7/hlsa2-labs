#!/usr/bin/env python3
"""analyze-replay.py - replay throughput + downstream impact summary.

Reads:
    <root>/<run>/meta.json   - duration_s, mode
    <root>/<run>/events.csv  - per-second queue depth samples
    <root>/assert-idempotent-{idempotent,naive}.json - hash + count
                               from the two-pass replays.

Usage:
    python3 scripts/analyze-replay.py perf/results/replay
"""

from __future__ import annotations

import csv
import json
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    root = Path(sys.argv[1])
    if not root.exists():
        print(f"No replay runs under {root}", file=sys.stderr)
        return 3

    print()
    print("Replay runs:")
    print(f"  {'run':<48} {'mode':<12} {'duration_s':>10} {'max_qdepth':>12}")
    for child in sorted(root.iterdir()):
        if not child.is_dir():
            continue
        meta_p = child / "meta.json"
        if not meta_p.exists():
            continue
        with meta_p.open() as f:
            meta = json.load(f)
        ev_p = child / "events.csv"
        max_q = 0
        if ev_p.exists():
            with ev_p.open() as f:
                rdr = csv.DictReader(f)
                for row in rdr:
                    try:
                        q = int(row.get("queue_depth", 0))
                    except ValueError:
                        q = 0
                    if q > max_q:
                        max_q = q
        print(f"  {child.name:<48} {meta.get('mode','?'):<12} {meta.get('duration_s',0):>10} {max_q:>12}")

    # Idempotency assertions
    print()
    print("Idempotency assertions (two replays of the same window):")
    for mode in ("idempotent", "naive"):
        p = root / f"assert-idempotent-{mode}.json"
        if not p.exists():
            print(f"  {mode:<12} - no assert-idempotent run yet")
            continue
        with p.open() as f:
            d = json.load(f)
        identical = d.get("identical", False)
        p1 = d.get("pass1", {})
        p2 = d.get("pass2", {})
        print(f"  {mode:<12} pass1: count={p1.get('count', 0)} hash={p1.get('hash','')[:12]}...")
        print(f"  {' ':<12} pass2: count={p2.get('count', 0)} hash={p2.get('hash','')[:12]}...")
        print(f"  {' ':<12} identical={identical}")
        if mode == "idempotent" and not identical:
            print("  ^ FAIL: idempotent mode should leave the hash unchanged across replays.")
        if mode == "naive" and identical:
            print("  ^ UNEXPECTED: naive mode hash did NOT change. Either no new events landed,")
            print("    or the naive path actually has a hidden unique constraint - investigate.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
