#!/usr/bin/env bash
# Three-run regression for the targeted improvement.
#
# CANDIDATE=grpc-hot-path        - re-runs bench-sync-chain with the
#                                  gateway pointed at the gRPC lookup.
# CANDIDATE=async-side-effect    - re-runs bench-sync-chain with
#                                  GATEWAY_AUDIT_MODE=event.
# CANDIDATE=dlq-retry-budget     - re-runs bench-async-overload with
#                                  the consumer's DLQ engaged.
#
# In every case we run RUNS baseline runs first (default knobs) and
# RUNS candidate runs second. analyze-regression.py reads
# perf/results/regression/<CANDIDATE>/baseline + /candidate.

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

CANDIDATE="${CANDIDATE:?CANDIDATE=grpc-hot-path|async-side-effect|dlq-retry-budget is required}"
RUNS="${RUNS:-3}"

OUT_BASE="perf/results/regression/${CANDIDATE}"
mkdir -p "${OUT_BASE}/baseline" "${OUT_BASE}/candidate"

run_chain_set() {
  local label="$1" subdir="$2"
  for i in $(seq 1 "${RUNS}"); do
    LABEL="${label}-run${i}" bash scripts/run-sync-chain.sh
    # Move the newest sync-chain run into the regression subdir.
    local newest
    newest="$(ls -dt perf/results/sync-chain/*/ | head -n1)"
    mv "${newest}" "${OUT_BASE}/${subdir}/"
  done
}

run_async_set() {
  local label="$1" subdir="$2" flow="$3"
  for i in $(seq 1 "${RUNS}"); do
    LABEL="${label}-run${i}" FLOW_CONTROL="${flow}" DURATION="2m" bash scripts/run-async-overload.sh
    local newest
    newest="$(ls -dt perf/results/async/*/ | head -n1)"
    mv "${newest}" "${OUT_BASE}/${subdir}/"
  done
}

case "${CANDIDATE}" in

  grpc-hot-path)
    echo "[regression] candidate=grpc-hot-path"
    # Baseline: REST hot path.
    GATEWAY_LOOKUP_TRANSPORT=rest docker compose up -d --force-recreate gateway >/dev/null
    sleep 8
    run_chain_set "baseline" "baseline"
    # Candidate: gRPC hot path.
    GATEWAY_LOOKUP_TRANSPORT=grpc docker compose up -d --force-recreate gateway >/dev/null
    sleep 8
    run_chain_set "candidate-grpc" "candidate"
    # Restore default.
    GATEWAY_LOOKUP_TRANSPORT=rest docker compose up -d --force-recreate gateway >/dev/null
    ;;

  async-side-effect)
    echo "[regression] candidate=async-side-effect"
    GATEWAY_AUDIT_MODE=sync docker compose up -d --force-recreate gateway >/dev/null
    sleep 8
    run_chain_set "baseline" "baseline"
    GATEWAY_AUDIT_MODE=event docker compose up -d --force-recreate gateway >/dev/null
    sleep 8
    run_chain_set "candidate-event" "candidate"
    GATEWAY_AUDIT_MODE=sync docker compose up -d --force-recreate gateway >/dev/null
    ;;

  dlq-retry-budget)
    echo "[regression] candidate=dlq-retry-budget"
    # Baseline: no flow control, no DLQ (events.dlq is created but
    # nothing routes to it from the consumer's default error path
    # unless the dlq-retry-budget candidate is flipped via env).
    FLOW_CONTROL=off docker compose up -d --force-recreate consumer >/dev/null
    sleep 8
    run_async_set "baseline" "baseline" "off"
    FLOW_CONTROL=on docker compose up -d --force-recreate consumer >/dev/null
    sleep 8
    run_async_set "candidate-dlq" "candidate" "on"
    FLOW_CONTROL=off docker compose up -d --force-recreate consumer >/dev/null
    ;;

  *)
    echo "Unknown CANDIDATE='${CANDIDATE}'. Valid: grpc-hot-path|async-side-effect|dlq-retry-budget" >&2
    exit 2
    ;;
esac

echo
echo "Done. Run 'make analyze CANDIDATE=${CANDIDATE}'."
