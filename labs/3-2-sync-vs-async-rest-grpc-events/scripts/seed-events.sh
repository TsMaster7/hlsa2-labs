#!/usr/bin/env bash
# Populate the events topic with WINDOW (default 24h) worth of events.
# Uses the producer's /start endpoint with window_s so the simulated
# history packs into a short wall-clock burst.

set -euo pipefail

WINDOW="${WINDOW:-24h}"
PRODUCER_URL="${PRODUCER_URL:-http://localhost:8094}"
SEED_RATE="${SEED_RATE:-2000}"
SEED_DURATION_S="${SEED_DURATION_S:-60}"

parse_dur() {
  local d="$1"
  if [[ "$d" =~ ^([0-9]+)h$ ]]; then echo "$(( ${BASH_REMATCH[1]} * 3600 ))"; return; fi
  if [[ "$d" =~ ^([0-9]+)m$ ]]; then echo "$(( ${BASH_REMATCH[1]} * 60 ))"; return; fi
  if [[ "$d" =~ ^([0-9]+)s?$ ]]; then echo "${BASH_REMATCH[1]}"; return; fi
  echo 86400
}
WINDOW_S="$(parse_dur "${WINDOW}")"

PAYLOAD=$(printf '{"rate": %d, "duration_s": %d, "window_s": %d, "source": "seed"}' \
  "${SEED_RATE}" "${SEED_DURATION_S}" "${WINDOW_S}")

echo "[seed-events] window=${WINDOW} (${WINDOW_S}s) rate=${SEED_RATE}/s burst=${SEED_DURATION_S}s"
curl -fsS -X POST -H 'content-type: application/json' -d "${PAYLOAD}" "${PRODUCER_URL}/start"
echo
echo "Producer started. Wait ~${SEED_DURATION_S}s for the seed to drain."
sleep "${SEED_DURATION_S}"
echo
echo "Seed complete."
