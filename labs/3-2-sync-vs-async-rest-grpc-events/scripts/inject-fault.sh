#!/usr/bin/env bash
# Inject a fault into one of the chain hops. POSTs /admin/inject on
# the named hop (auth | pricing | inventory) with the configured
# latency tail and error rate.

set -euo pipefail

HOP="${HOP:?HOP=auth|pricing|inventory is required}"
P99_MS="${P99_MS:-400}"
ERROR_RATE="${ERROR_RATE:-0.02}"
MODE="${MODE:-latency}"

case "${HOP}" in
  auth)      PORT=8091 ;;
  pricing)   PORT=8092 ;;
  inventory) PORT=8093 ;;
  *)
    echo "HOP must be one of auth|pricing|inventory, got '${HOP}'" >&2
    exit 2
    ;;
esac

URL="http://localhost:${PORT}/admin/inject"
PAYLOAD="{\"p99_ms\": ${P99_MS}, \"error_rate\": ${ERROR_RATE}}"
echo "[inject-fault] hop=${HOP} mode=${MODE} p99_ms=${P99_MS} error_rate=${ERROR_RATE}"
echo "[inject-fault] POST ${URL} ${PAYLOAD}"
curl -fsS -X POST -H 'content-type: application/json' -d "${PAYLOAD}" "${URL}"
echo
