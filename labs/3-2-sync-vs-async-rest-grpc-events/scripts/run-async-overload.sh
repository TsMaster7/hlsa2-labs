#!/usr/bin/env bash
# Drive the async overload run via k6. LABEL=no-bp|with-bp,
# FLOW_CONTROL=off|on, DURATION (default 5m).

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

LABEL="${LABEL:-unlabeled}"
FLOW_CONTROL="${FLOW_CONTROL:-off}"
DURATION="${DURATION:-5m}"
PRODUCER_URL="${PRODUCER_URL:-http://localhost:8094}"
CONSUMER_URL="${CONSUMER_URL:-http://localhost:8095}"
PRODUCER_RATE="${PRODUCER_RATE:-5000}"
CONSUMER_RATE="${CONSUMER_RATE:-1500}"

# Parse k6-style duration like 5m, 30s, 1h into seconds.
parse_dur() {
  local d="$1"
  if [[ "$d" =~ ^([0-9]+)h$ ]]; then echo "$(( ${BASH_REMATCH[1]} * 3600 ))"; return; fi
  if [[ "$d" =~ ^([0-9]+)m$ ]]; then echo "$(( ${BASH_REMATCH[1]} * 60 ))"; return; fi
  if [[ "$d" =~ ^([0-9]+)s?$ ]]; then echo "${BASH_REMATCH[1]}"; return; fi
  echo 300
}
DURATION_S="$(parse_dur "${DURATION}")"

# Reset audit tables before each run so the event-to-effect histogram
# starts clean.
curl -fsS -X POST "${CONSUMER_URL}/reset" >/dev/null || true

TS="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="perf/results/async/${TS}-${LABEL}-fc${FLOW_CONTROL}"
mkdir -p "${RUN_DIR}"

if ! command -v k6 >/dev/null 2>&1; then
  echo "k6 is not installed on PATH." >&2
  exit 4
fi

k6 run \
  --summary-export "${RUN_DIR}/summary.json" \
  --out "json=${RUN_DIR}/raw.json" \
  -e PRODUCER_URL="${PRODUCER_URL}" \
  -e CONSUMER_URL="${CONSUMER_URL}" \
  -e PRODUCER_RATE="${PRODUCER_RATE}" \
  -e CONSUMER_RATE="${CONSUMER_RATE}" \
  -e DURATION_S="${DURATION_S}" \
  -e FLOW_CONTROL="${FLOW_CONTROL}" \
  -e LABEL="${LABEL}" \
  perf/k6/bench-async-overload.js 2>&1 | tee "${RUN_DIR}/k6.log"

# Pull a final consumer state snapshot for the meta record.
SNAPSHOT="$(curl -fsS "${CONSUMER_URL}/state" 2>/dev/null || echo '{}')"
cat > "${RUN_DIR}/meta.json" <<EOF
{
  "label": "${LABEL}",
  "flow_control": "${FLOW_CONTROL}",
  "duration_s": ${DURATION_S},
  "producer_rate": ${PRODUCER_RATE},
  "consumer_rate": ${CONSUMER_RATE},
  "final_state": ${SNAPSHOT},
  "captured_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

echo
echo "Done. Run another LABEL or 'make analyze-async'."
