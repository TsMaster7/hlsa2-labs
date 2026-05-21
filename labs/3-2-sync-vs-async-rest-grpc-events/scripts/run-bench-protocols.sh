#!/usr/bin/env bash
# Run the REST/JSON and gRPC/Protobuf benches RUNS times each, with
# the same arrival rate and payload distribution.
#
# Reads: TARGET_RPS (default from workload.json), PAYLOAD (small|large),
#        REST_URL, GRPC_ADDR, RUNS (default 3).
# Writes per-run directories under perf/results/{rest,grpc}/.

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

if ! command -v k6 >/dev/null 2>&1; then
  echo "k6 is not installed on PATH. See https://k6.io/docs/getting-started/installation/" >&2
  exit 4
fi
if ! docker compose ps lookup-svc >/dev/null 2>&1; then
  echo "Stack is not up. Run 'make up' first." >&2
  exit 3
fi

RUNS="${RUNS:-3}"
TARGET_RPS="${TARGET_RPS:-200}"
PAYLOAD="${PAYLOAD:-small}"
REST_URL="${REST_URL:-http://localhost:8080}"
GRPC_ADDR="${GRPC_ADDR:-localhost:9000}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"

run_one() {
  local proto="$1" idx="$2"
  local run_dir="perf/results/${proto}/${TS}-rps${TARGET_RPS}-${PAYLOAD}-run${idx}"
  mkdir -p "${run_dir}"
  echo "[bench-protocols] proto=${proto} run=${idx} target_rps=${TARGET_RPS} payload=${PAYLOAD}"

  if [[ "${proto}" == "rest" ]]; then
    k6 run \
      --summary-export "${run_dir}/summary.json" \
      --out "json=${run_dir}/raw.json" \
      -e REST_URL="${REST_URL}" \
      -e TARGET_RPS="${TARGET_RPS}" \
      -e PAYLOAD="${PAYLOAD}" \
      perf/k6/bench-protocols-rest.js 2>&1 | tee "${run_dir}/k6.log"
  else
    k6 run \
      --summary-export "${run_dir}/summary.json" \
      --out "json=${run_dir}/raw.json" \
      -e GRPC_ADDR="${GRPC_ADDR}" \
      -e TARGET_RPS="${TARGET_RPS}" \
      -e PAYLOAD="${PAYLOAD}" \
      perf/k6/bench-protocols-grpc.js 2>&1 | tee "${run_dir}/k6.log"
  fi

  cat > "${run_dir}/meta.json" <<EOF
{
  "protocol": "${proto}",
  "run": ${idx},
  "target_rps": ${TARGET_RPS},
  "payload": "${PAYLOAD}",
  "captured_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
}

for i in $(seq 1 "${RUNS}"); do
  run_one rest "${i}"
  # Settle period between protocols to avoid cross-pollution.
  sleep 5
  run_one grpc "${i}"
  sleep 5
done

echo
echo "Done. Inspect with: make analyze-protocols"
