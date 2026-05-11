#!/usr/bin/env bash
# Three-run regression protocol.
#
# Runs the open-loop baseline three times against whatever pool size
# the stack currently has (default 10), then prompts you to bounce the
# stack with the candidate pool size (40), then runs three more times.
# Finally calls scripts/analyze-regression.py to print the per-endpoint
# Δp99 vs 2σ decision table.
#
# This is the script the homework's Step 5 acceptance gate checks
# against.

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

BASELINE_POOL="${BASELINE_POOL:-10}"
CANDIDATE_POOL="${CANDIDATE_POOL:-40}"

echo "[regression] === phase 1: baseline (SUT_DB_POOL_SIZE=${BASELINE_POOL}) ==="
SUT_DB_POOL_SIZE="${BASELINE_POOL}" docker compose up -d --no-deps sut
sleep 8
for i in 1 2 3; do
  echo "[regression] baseline run ${i}/3"
  RUN_LABEL="baseline-pool${BASELINE_POOL}-run${i}" SUT_DB_POOL_SIZE="${BASELINE_POOL}" \
    bash scripts/run-experiment.sh baseline
done

echo
echo "[regression] === phase 2: candidate (SUT_DB_POOL_SIZE=${CANDIDATE_POOL}) ==="
SUT_DB_POOL_SIZE="${CANDIDATE_POOL}" docker compose up -d --no-deps sut
sleep 8
for i in 1 2 3; do
  echo "[regression] candidate run ${i}/3"
  RUN_LABEL="candidate-pool${CANDIDATE_POOL}-run${i}" SUT_DB_POOL_SIZE="${CANDIDATE_POOL}" \
    bash scripts/run-experiment.sh baseline
done

# Move the candidate runs from baseline/ to candidate/ for the analyzer.
echo
echo "[regression] organising results..."
mkdir -p perf/results/candidate
shopt -s nullglob
for d in perf/results/baseline/*candidate-pool*; do
  mv "$d" perf/results/candidate/
done
shopt -u nullglob

echo
echo "[regression] === analysis ==="
python3 scripts/analyze-regression.py perf/results/baseline perf/results/candidate
