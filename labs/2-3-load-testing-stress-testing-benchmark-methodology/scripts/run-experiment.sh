#!/usr/bin/env bash
# Run a single k6 experiment, capture the env fingerprint, and persist
# everything under perf/results/<kind>/<RUN_ID>/.
#
# Usage:
#   bash scripts/run-experiment.sh baseline      # open-loop baseline
#   bash scripts/run-experiment.sh closed-loop   # for the omission demo
#   bash scripts/run-experiment.sh soak          # 4-hour soak
#
# Env overrides (all optional):
#   SUT_URL          default http://localhost:8080
#   TARGET_RPS       overrides workload.json target_rps
#   WARMUP_S         overrides workload.json warmup_seconds
#   MEASURED_S       overrides workload.json measured_seconds
#   SOAK_RPS         overrides default (target_rps / 2)
#   K6_VUS           closed-loop only (default 1)
#   RUN_LABEL        appended to RUN_ID, useful for "candidate-pool40"

set -euo pipefail

KIND="${1:-}"
case "${KIND}" in
  baseline|closed-loop|soak) : ;;
  *)
    echo "Usage: $0 <baseline|closed-loop|soak>" >&2
    exit 2
    ;;
esac

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

if ! docker compose ps --format json sut >/dev/null 2>&1; then
  echo "Stack is not up. Run 'make run-sut' first." >&2
  exit 3
fi

if ! command -v k6 >/dev/null 2>&1; then
  echo "k6 is not installed on PATH. See https://k6.io/docs/getting-started/installation/" >&2
  exit 4
fi

RESULT_PARENT_DIR="perf/results/${KIND}"
case "${KIND}" in
  closed-loop) RESULT_PARENT_DIR="perf/results/coordinated-omission" ;;
esac
mkdir -p "${RESULT_PARENT_DIR}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
LABEL="${RUN_LABEL:-}"
RUN_ID="${KIND}-${TS}${LABEL:+-${LABEL}}"
RUN_DIR="${RESULT_PARENT_DIR}/${RUN_ID}"
mkdir -p "${RUN_DIR}"

bash perf/env.sh > "${RUN_DIR}/env.txt"

case "${KIND}" in
  baseline)
    SCRIPT="perf/k6/baseline.js"
    SUMMARY_NAME="summary-open-loop.json"
    JSON_NAME="raw-open-loop.json"
    ;;
  closed-loop)
    SCRIPT="perf/k6/closed-loop.js"
    SUMMARY_NAME="summary-closed-loop.json"
    JSON_NAME="raw-closed-loop.json"
    ;;
  soak)
    SCRIPT="perf/k6/soak.js"
    SUMMARY_NAME="summary-soak.json"
    JSON_NAME="raw-soak.json"
    ;;
esac

START_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
START_EPOCH="$(date -u +%s)"
echo "[run-experiment] kind=${KIND} run_id=${RUN_ID} start=${START_ISO}"

# k6 emits a streaming JSON file (one event per line). For very long
# soaks this can grow large; the summary export is what the analysis
# scripts actually read.
k6 run \
  --summary-export "${RUN_DIR}/${SUMMARY_NAME}" \
  --out "json=${RUN_DIR}/${JSON_NAME}" \
  -e SUT_URL="${SUT_URL:-http://localhost:8080}" \
  ${TARGET_RPS:+-e TARGET_RPS=${TARGET_RPS}} \
  ${WARMUP_S:+-e WARMUP_S=${WARMUP_S}} \
  ${MEASURED_S:+-e MEASURED_S=${MEASURED_S}} \
  ${SOAK_RPS:+-e SOAK_RPS=${SOAK_RPS}} \
  ${K6_VUS:+-e K6_VUS=${K6_VUS}} \
  "${SCRIPT}" 2>&1 | tee "${RUN_DIR}/k6.log"

END_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
END_EPOCH="$(date -u +%s)"
DURATION=$((END_EPOCH - START_EPOCH))

WORKLOAD_SHA="$( { command -v shasum >/dev/null && shasum -a 256 perf/workload.json; } || sha256sum perf/workload.json )"
WORKLOAD_SHA="${WORKLOAD_SHA%% *}"

cat > "${RUN_DIR}/meta.json" <<EOF
{
  "kind": "${KIND}",
  "run_id": "${RUN_ID}",
  "label": "${LABEL}",
  "start_iso": "${START_ISO}",
  "end_iso": "${END_ISO}",
  "duration_seconds": ${DURATION},
  "summary_path": "${SUMMARY_NAME}",
  "raw_path": "${JSON_NAME}",
  "log_path": "k6.log",
  "env_path": "env.txt",
  "workload_sha256": "${WORKLOAD_SHA}",
  "sut_db_pool_size": "${SUT_DB_POOL_SIZE:-default-or-compose-default}"
}
EOF

echo
echo "[run-experiment] done."
echo "[run-experiment] run_dir=${RUN_DIR}"
