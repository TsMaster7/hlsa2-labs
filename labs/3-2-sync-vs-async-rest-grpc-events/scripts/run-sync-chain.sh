#!/usr/bin/env bash
# Drive the gateway's /sync-chain. LABEL distinguishes healthy/faulted
# runs; CIRCUIT_BREAKER=on restarts the gateway with the breaker on
# before the run.

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

LABEL="${LABEL:-unlabeled}"
CIRCUIT_BREAKER="${CIRCUIT_BREAKER:-off}"
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8090}"
TARGET_RPS="${TARGET_RPS:-100}"
WARMUP_S="${WARMUP_S:-20}"
MEASURED_S="${MEASURED_S:-120}"

if [[ "${CIRCUIT_BREAKER}" == "on" ]]; then
  echo "[sync-chain] restarting gateway with CIRCUIT_BREAKER=on"
  CIRCUIT_BREAKER=on docker compose up -d --force-recreate gateway
  sleep 8
fi

TS="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="perf/results/sync-chain/${TS}-${LABEL}-cb${CIRCUIT_BREAKER}"
mkdir -p "${RUN_DIR}"

k6 run \
  --summary-export "${RUN_DIR}/summary.json" \
  --out "json=${RUN_DIR}/raw.json" \
  -e GATEWAY_URL="${GATEWAY_URL}" \
  -e LABEL="${LABEL}" \
  -e TARGET_RPS="${TARGET_RPS}" \
  -e WARMUP_S="${WARMUP_S}" \
  -e MEASURED_S="${MEASURED_S}" \
  perf/k6/bench-sync-chain.js 2>&1 | tee "${RUN_DIR}/k6.log"

cat > "${RUN_DIR}/meta.json" <<EOF
{
  "label": "${LABEL}",
  "circuit_breaker": "${CIRCUIT_BREAKER}",
  "target_rps": ${TARGET_RPS},
  "captured_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

echo
echo "Done. Run another LABEL or `make analyze-sync-chain`."
