#!/usr/bin/env bash
# Sidecar to collect the "log disk usage" soak metric.
#
# k6 + Prometheus already give you four of the five soak signals
# (memory, FDs, TCP states, goroutines). Container log size is the
# odd one out: `docker logs` grows on disk independently of any of
# the in-process gauges.
#
# This script polls every 60s, writes the value to a node-exporter
# textfile, and exposes it as soak_container_logs_bytes{container=...}.
# Run it alongside the k6 soak in a separate terminal.
#
# Usage:
#   bash scripts/soak-collect.sh                    # uses defaults
#   POLL_S=30 bash scripts/soak-collect.sh          # faster polling
#
# The textfile must be readable by node-exporter; for the dev compose
# stack we mount /var/lib/node_exporter/textfile_collector. If your
# node-exporter does not expose --collector.textfile.directory, this
# script still produces the file but Prometheus will not scrape it -
# the soak panel will then be empty.

set -euo pipefail

POLL_S="${POLL_S:-60}"
TEXTFILE_DIR="${TEXTFILE_DIR:-/tmp/hlsa2-textfile}"
mkdir -p "${TEXTFILE_DIR}"

CONTAINERS=("hlsa2-sut" "hlsa2-downstream")

echo "[soak-collect] polling every ${POLL_S}s, writing to ${TEXTFILE_DIR}/soak_container_logs.prom"
echo "[soak-collect] mount this directory into node-exporter via:"
echo "    --collector.textfile.directory=/var/lib/node_exporter/textfile_collector"
echo "[soak-collect] (or read the .prom file directly with cat)"

while true; do
  out="${TEXTFILE_DIR}/soak_container_logs.prom.tmp"
  : > "${out}"
  echo "# HELP soak_container_logs_bytes Size of the container's docker-logs file." >> "${out}"
  echo "# TYPE soak_container_logs_bytes gauge" >> "${out}"
  for c in "${CONTAINERS[@]}"; do
    log_path=$(docker inspect --format '{{.LogPath}}' "${c}" 2>/dev/null || true)
    if [ -z "${log_path}" ] || [ ! -f "${log_path}" ]; then
      continue
    fi
    bytes=$(stat -c %s "${log_path}" 2>/dev/null || stat -f %z "${log_path}" 2>/dev/null || echo 0)
    echo "soak_container_logs_bytes{container=\"${c}\"} ${bytes}" >> "${out}"
  done
  mv "${out}" "${TEXTFILE_DIR}/soak_container_logs.prom"
  sleep "${POLL_S}"
done
