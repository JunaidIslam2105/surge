#!/usr/bin/env bash
set -euo pipefail

if (( $# > 1 )); then
  echo "usage: $0 [baseline-report.json]" >&2
  exit 2
fi

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
result_dir="$repo_dir/benchmark-results/$timestamp"
bench_bin=$(mktemp /tmp/surge-benchmark.XXXXXX)
trap 'rm -f -- "$bench_bin"' EXIT
mkdir -p -- "$result_dir"

timeout 30s go build -o "$bench_bin" ./cmd/benchmark

baseline_args=()
if (( $# == 1 )); then
  baseline_args=(--baseline "$1")
fi

echo "Running stable benchmark (one warm-up, five measured transfers)..."
timeout 150s "$bench_bin" --output "$result_dir" "${baseline_args[@]}"

echo "Running one separately-instrumented Go profile transfer..."
timeout 30s "$bench_bin" --output "$result_dir" --report profile-run.json --runs 1 --warmup 0 --profiles

if command -v strace >/dev/null 2>&1 && strace -f -c true >/dev/null 2>&1; then
  echo "Running one syscall-counted 64 MiB diagnostic transfer..."
  if ! timeout 25s strace -f -c -o "$result_dir/syscalls.txt" \
    "$bench_bin" --output "$result_dir" --report syscall-run.json --runs 1 --warmup 0 --size-mib 64; then
    rm -f -- "$result_dir/syscalls.txt"
    printf '%s\n' 'strace: diagnostic transfer failed or timed out; syscall counts were skipped' >> "$result_dir/telemetry-gaps.txt"
  fi
else
  printf '%s\n' 'strace: unavailable or ptrace is blocked; syscall counts were skipped' >> "$result_dir/telemetry-gaps.txt"
fi

perf_probe=$(mktemp /tmp/surge-perf-probe.XXXXXX)
if command -v perf >/dev/null 2>&1 && perf stat -x, -o "$perf_probe" true >/dev/null 2>&1 && ! grep -q '<not supported>' "$perf_probe"; then
  echo "Running one hardware-counter 64 MiB diagnostic transfer..."
  if ! timeout 15s perf stat -x, -o "$result_dir/perf-stat.csv" -- \
    "$bench_bin" --output "$result_dir" --report perf-run.json --runs 1 --warmup 0 --size-mib 64; then
    rm -f -- "$result_dir/perf-stat.csv"
    printf '%s\n' 'perf: diagnostic transfer failed or timed out; hardware counters were skipped' >> "$result_dir/telemetry-gaps.txt"
  fi
else
  printf '%s\n' 'perf: unavailable or kernel counters are blocked; hardware counters were skipped' >> "$result_dir/telemetry-gaps.txt"
fi
rm -f -- "$perf_probe"

printf '%s\n' 'syscalls and perf cover a 64 MiB combined benchmark process (Surge client plus loopback server); the stable comparison workload remains 512 MiB' > "$result_dir/telemetry-scope.txt"
echo "Artifacts: $result_dir"
