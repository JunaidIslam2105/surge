#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
result_dir="$repo_dir/benchmark-results/$timestamp-throttle"
bench_bin=$(mktemp /tmp/surge-throttle-benchmark.XXXXXX)
trap 'rm -f -- "$bench_bin"' EXIT
mkdir -p -- "$result_dir"

timeout 30s go build -o "$bench_bin" ./cmd/benchmark-throttle
timeout 300s "$bench_bin" --output "$result_dir" "$@"

echo "Artifacts: $result_dir"
