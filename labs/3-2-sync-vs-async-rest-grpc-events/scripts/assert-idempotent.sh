#!/usr/bin/env bash
# Replay the same window twice in the given mode and compare the
# final-state hash. PASS (exit 0) when the hash is unchanged
# (idempotent mode); FAIL (exit 1) when it changes (naive mode).

set -euo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

CONSUMER_MODE="${CONSUMER_MODE:-idempotent}"
WINDOW="${WINDOW:-24h}"
CONSUMER_URL="${CONSUMER_URL:-http://localhost:8095}"

case "${CONSUMER_MODE}" in
  idempotent|naive) : ;;
  *) echo "CONSUMER_MODE must be idempotent or naive, got '${CONSUMER_MODE}'" >&2; exit 2 ;;
esac

hash_fn() {
  if [[ "${CONSUMER_MODE}" == "idempotent" ]]; then
    docker compose exec -T postgres psql -U lab -d lab -tA -c "SELECT events_audit_hash();"
  else
    docker compose exec -T postgres psql -U lab -d lab -tA -c "SELECT events_audit_naive_hash();"
  fi
}

count_fn() {
  if [[ "${CONSUMER_MODE}" == "idempotent" ]]; then
    docker compose exec -T postgres psql -U lab -d lab -tA -c "SELECT count(*) FROM events_audit;"
  else
    docker compose exec -T postgres psql -U lab -d lab -tA -c "SELECT count(*) FROM events_audit_naive;"
  fi
}

echo "[assert-idempotent] mode=${CONSUMER_MODE} window=${WINDOW}"

# First replay
CONSUMER_MODE="${CONSUMER_MODE}" WINDOW="${WINDOW}" bash scripts/replay.sh >/dev/null
H1="$(hash_fn | tr -d '\r\n[:space:]')"
C1="$(count_fn | tr -d '\r\n[:space:]')"
echo "  pass 1: count=${C1} hash=${H1}"

# Second replay - do NOT reset audit tables between passes; replay.sh
# truncates on entry, so we cheat by NOT calling it and instead hit
# /replay directly to keep the existing rows.
PAYLOAD=$(printf '{"window_s": 86400, "mode": "%s"}' "${CONSUMER_MODE}")
curl -fsS -X POST -H 'content-type: application/json' -d "${PAYLOAD}" "${CONSUMER_URL}/replay" >/dev/null

# Wait for the replay goroutine to finish (queue back to 0).
for i in $(seq 1 600); do
  q="$(curl -fsS "${CONSUMER_URL}/state" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("queue_depth", 0))')"
  if [[ "$q" == "0" ]]; then break; fi
  sleep 2
done
# Settle so any in-flight handlers commit.
sleep 5

H2="$(hash_fn | tr -d '\r\n[:space:]')"
C2="$(count_fn | tr -d '\r\n[:space:]')"
echo "  pass 2: count=${C2} hash=${H2}"

OUT_DIR="perf/results/replay"
mkdir -p "${OUT_DIR}"
RESULT_FILE="${OUT_DIR}/assert-idempotent-${CONSUMER_MODE}.json"
cat > "${RESULT_FILE}" <<EOF
{
  "mode": "${CONSUMER_MODE}",
  "window": "${WINDOW}",
  "pass1": { "count": ${C1:-0}, "hash": "${H1}" },
  "pass2": { "count": ${C2:-0}, "hash": "${H2}" },
  "identical": $([[ "${H1}" == "${H2}" ]] && echo true || echo false)
}
EOF

if [[ "${CONSUMER_MODE}" == "idempotent" ]]; then
  if [[ "${H1}" == "${H2}" ]]; then
    echo "PASS: idempotent mode held the final state hash across two replays."
    exit 0
  else
    echo "FAIL: idempotent mode should hold but did not. Inspect ${RESULT_FILE}." >&2
    exit 1
  fi
else
  if [[ "${H1}" != "${H2}" ]]; then
    echo "EXPECTED-CHANGE: naive mode CHANGED final state across replays (this is the at-least-once hazard the topic warns about)."
    exit 0
  else
    echo "UNEXPECTED: naive mode did NOT change state - the second replay had no new rows. Did the topic backfill drain before pass 2?" >&2
    exit 1
  fi
fi
