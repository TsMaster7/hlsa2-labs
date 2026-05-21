#!/usr/bin/env bash
# Environment fingerprint capture for lab 3-2.
#
# Writes:
#   perf/results/env.txt   - human-readable summary
#   perf/results/meta.json - machine-readable (consumed by analyzers)
#
# Run via `make env-fingerprint`. Re-run if you move to a different
# machine; every comparison run must share one fingerprint.

set -u

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${LAB_ROOT}/perf/results"
mkdir -p "${OUT_DIR}"

ENV_TXT="${OUT_DIR}/env.txt"
META_JSON="${OUT_DIR}/meta.json"

print_kv()      { printf "%-22s %s\n" "$1" "$2"; }
print_section() { printf '\n=== %s ===\n' "$1"; }

{
  print_section "host"
  print_kv "captured_at"     "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  print_kv "uname"           "$(uname -a)"
  print_kv "os_release"      "$( { [ -f /etc/os-release ] && (. /etc/os-release; echo "${PRETTY_NAME:-unknown}"); } || echo "${OSTYPE:-unknown}")"
  print_kv "cpu_count"       "$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo "?")"
  print_kv "memory_total_kb" "$(awk '/MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || sysctl -n hw.memsize 2>/dev/null | awk '{print int($1/1024)}' || echo "?")"

  print_section "kernel_cpu"
  {
    if [ -r /proc/cpuinfo ]; then
      grep -m1 'model name' /proc/cpuinfo || true
      grep -m1 'cpu MHz' /proc/cpuinfo    || true
    else
      sysctl -n machdep.cpu.brand_string 2>/dev/null || true
      sysctl -n hw.cpufrequency 2>/dev/null || true
    fi
  } | sed 's/^/  /'

  print_section "tooling"
  print_kv "docker"          "$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'missing')"
  print_kv "docker_compose"  "$(docker compose version --short 2>/dev/null || echo 'missing')"
  print_kv "k6"              "$(k6 version 2>/dev/null | head -n1 || echo 'missing')"
  print_kv "python"          "$(python3 --version 2>/dev/null || echo 'missing')"

  print_section "compose_image_versions"
  grep -E '^\s*image:' "${LAB_ROOT}/docker-compose.yml" | sed 's/^[[:space:]]*image:[[:space:]]*/  /'

  print_section "container_limits"
  {
    for c in hlsa2-l32-lookup hlsa2-l32-gateway hlsa2-l32-auth hlsa2-l32-pricing hlsa2-l32-inventory hlsa2-l32-producer hlsa2-l32-consumer hlsa2-l32-redpanda hlsa2-l32-postgres; do
      docker inspect -f '  '"$c"' cpus={{.HostConfig.NanoCpus}} mem={{.HostConfig.Memory}}' "$c" 2>/dev/null || true
    done
  }

  print_section "workload_model_sha256"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${LAB_ROOT}/perf/workload.json" 2>/dev/null | sed 's/^/  /'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${LAB_ROOT}/perf/workload.json" 2>/dev/null | sed 's/^/  /'
  fi
} | tee "${ENV_TXT}"

WORKLOAD_SHA=""
if [ -f "${LAB_ROOT}/perf/workload.json" ]; then
  WORKLOAD_SHA="$( { command -v shasum >/dev/null && shasum -a 256 "${LAB_ROOT}/perf/workload.json"; } || sha256sum "${LAB_ROOT}/perf/workload.json" )"
  WORKLOAD_SHA="${WORKLOAD_SHA%% *}"
fi

cat > "${META_JSON}" <<EOF
{
  "captured_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "cpu_count": $(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 0),
  "memory_total_kb": $(awk '/MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || sysctl -n hw.memsize 2>/dev/null | awk '{print int($1/1024)}' || echo 0),
  "docker": "$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo unknown)",
  "k6": "$(k6 version 2>/dev/null | head -n1 || echo missing)",
  "workload_sha256": "${WORKLOAD_SHA}"
}
EOF

echo
echo "Wrote ${ENV_TXT} and ${META_JSON}"
