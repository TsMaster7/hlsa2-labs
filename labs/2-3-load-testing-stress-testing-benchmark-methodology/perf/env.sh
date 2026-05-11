#!/usr/bin/env bash
# Environment fingerprint capture.
#
# Run on the same host where you run the load generator. Output is
# meant to live alongside each result run so a reviewer can compare
# numbers across runs without ambiguity. Pipe to a file:
#
#   bash perf/env.sh > perf/results/baseline/<RUN_ID>/env.txt
#
# scripts/run-experiment.sh does this automatically.

set -u

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

print_kv() { printf "%-22s %s\n" "$1" "$2"; }
print_section() { printf '\n=== %s ===\n' "$1"; }

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
    grep -m1 'cpu MHz' /proc/cpuinfo   || true
  else
    sysctl -n machdep.cpu.brand_string 2>/dev/null || true
    sysctl -n hw.cpufrequency 2>/dev/null || true
  fi
} | sed 's/^/  /'

print_section "tooling"
print_kv "docker"          "$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'missing')"
print_kv "docker_compose"  "$(docker compose version --short 2>/dev/null || echo 'missing')"
print_kv "k6"              "$(k6 version 2>/dev/null | head -n1 || echo 'missing')"
print_kv "go"              "$(go version 2>/dev/null || echo 'missing on host (built in container)')"
print_kv "python"          "$(python3 --version 2>/dev/null || echo 'missing')"

print_section "sut_dependencies"
if [ -f "${LAB_ROOT}/sut/go.mod" ]; then
  awk '/^require/,/^\)/' "${LAB_ROOT}/sut/go.mod" | grep -v '^require' | grep -v '^)' | sed 's/^[[:space:]]*/  /'
fi

print_section "compose_image_versions"
{
  grep -E '^\s*image:' "${LAB_ROOT}/docker-compose.yml" | sed 's/^[[:space:]]*image:[[:space:]]*/  /'
} 2>/dev/null

print_section "workload_model_sha256"
if command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "${LAB_ROOT}/perf/workload.json" 2>/dev/null | sed 's/^/  /'
elif command -v sha256sum >/dev/null 2>&1; then
  sha256sum "${LAB_ROOT}/perf/workload.json" 2>/dev/null | sed 's/^/  /'
fi
