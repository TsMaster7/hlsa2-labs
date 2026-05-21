#!/usr/bin/env bash
# Clear a previously-injected fault on a chain hop.

set -euo pipefail

HOP="${HOP:?HOP=auth|pricing|inventory is required}"

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
echo "[clear-fault] hop=${HOP}"
curl -fsS -X POST -H 'content-type: application/json' -d '{"p99_ms": 0, "error_rate": 0}' "${URL}"
echo
