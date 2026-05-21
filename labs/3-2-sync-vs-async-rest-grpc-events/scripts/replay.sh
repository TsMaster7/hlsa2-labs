#!/usr/bin/env bash
# Spin a fresh consumer group from offset 0 and measure replay
# throughput + downstream impact. Truncates the audit tables first so
# the replay starts from a known state.

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

WINDOW="${WINDOW:-24h}"
CONSUMER_MODE="${CONSUMER_MODE:-idempotent}"
CONSUMER_URL="${CONSUMER_URL:-http://localhost:8095}"

parse_dur() {
  local d="$1"
  if [[ "$d" =~ ^([0-9]+)h$ ]]; then echo "$(( ${BASH_REMATCH[1]} * 3600 ))"; return; fi
  if [[ "$d" =~ ^([0-9]+)m$ ]]; then echo "$(( ${BASH_REMATCH[1]} * 60 ))"; return; fi
  if [[ "$d" =~ ^([0-9]+)s?$ ]]; then echo "${BASH_REMATCH[1]}"; return; fi
  echo 86400
}
WINDOW_S="$(parse_dur "${WINDOW}")"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="perf/results/replay/${TS}-${CONSUMER_MODE}-${WINDOW}"
mkdir -p "${RUN_DIR}"

echo "[replay] mode=${CONSUMER_MODE} window=${WINDOW} (${WINDOW_S}s)"

curl -fsS -X POST -H 'content-type: application/json' -d '{}' "${CONSUMER_URL}/reset" >/dev/null
PAYLOAD=$(printf '{"window_s": %d, "mode": "%s"}' "${WINDOW_S}" "${CONSUMER_MODE}")
START_EPOCH="$(date -u +%s)"
curl -fsS -X POST -H 'content-type: application/json' -d "${PAYLOAD}" "${CONSUMER_URL}/replay" | tee "${RUN_DIR}/replay-ack.json"
echo

# Poll consumer state every 2 seconds until the queue stays at 0 for a
# while - approximates "replay complete".
events_csv="${RUN_DIR}/events.csv"
echo "ts,queue_depth,shed,aborted" > "${events_csv}"

STABLE=0
for i in $(seq 1 1800); do
  s="$(curl -fsS "${CONSUMER_URL}/state")"
  q="$(echo "$s" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("queue_depth", 0))')"
  shed="$(echo "$s" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("shed", False))')"
  aborted="$(echo "$s" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("aborted", False))')"
  printf "%s,%s,%s,%s\n" "$(date -u +%s)" "$q" "$shed" "$aborted" >> "${events_csv}"

  if [[ "$q" == "0" ]]; then
    STABLE=$((STABLE + 1))
    if [[ "$STABLE" -ge 5 ]]; then
      break
    fi
  else
    STABLE=0
  fi
  sleep 2
done

END_EPOCH="$(date -u +%s)"
DURATION=$((END_EPOCH - START_EPOCH))

cat > "${RUN_DIR}/meta.json" <<EOF
{
  "mode": "${CONSUMER_MODE}",
  "window": "${WINDOW}",
  "window_s": ${WINDOW_S},
  "duration_s": ${DURATION},
  "captured_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

echo
echo "Done. Wall-clock replay ~${DURATION}s. Run 'make analyze-replay' or 'make assert-idempotent CONSUMER_MODE=${CONSUMER_MODE}'."
