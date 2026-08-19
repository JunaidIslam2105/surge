# Development Guide

This guide covers the normal local workflow for contributing to Surge. Run commands from the repository root unless noted otherwise.

## Prerequisites

- Git
- Go **1.25 or newer** (the module declares `go 1.25.0`)
- A POSIX shell for the commands below; Windows contributors can use Git Bash or adapt the commands to PowerShell

Optional tools:

- Node.js 22+ and npm for the browser extension
- ShellCheck for shell scripts
- `actionlint` for GitHub Actions files
- `strace` and `perf` for Linux profiling
- Nix, if you use the flake-based build

## First-time setup

```bash
git clone https://github.com/SurgeDM/Surge.git
cd Surge
go mod download
go build -o ./surge .
```

Run the CLI without installing it globally:

```bash
go run . --help
go run . version
```

When running local commands that may write configuration or resume state, use an isolated configuration directory so development data does not mix with your normal Surge installation:

```bash
XDG_CONFIG_HOME="$(mktemp -d)" XDG_CACHE_HOME="$(mktemp -d)" go run .
```

On Windows, set equivalent temporary `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` directories in your shell before running tests or the TUI.

## Development loop

The main Go packages are organized as follows:

- `cmd/`: CLI commands and process startup
- `internal/orchestrator/`: lifecycle, enqueue, pause/resume, and event coordination
- `internal/scheduler/`: queued and active download scheduling, rate limits, and shutdown
- `internal/strategy/concurrent/`: ranged, mirrored, retried, and hedged downloads
- `internal/strategy/single/`: single-connection fallback downloads and throttling
- `internal/probe/` and `internal/transport/`: server probing and network/rate-limit primitives
- `internal/progress/` and `internal/store/`: progress state and persistence
- `internal/tui/`: Bubble Tea model, update loop, views, components, and rendering tests
- `internal/testutil/`: reusable HTTP servers and test fixtures
- `extension/`: WXT/Solid browser extension

After a change, format only the Go files you touched or format the package files directly:

```bash
gofmt -w path/to/changed.go path/to/changed_test.go
```

## Go tests and checks

Run the complete unit test suite:

```bash
go test ./...
```

Run focused tests while iterating:

```bash
go test ./internal/tui ./internal/tui/components -count=1
go test ./internal/strategy/concurrent -count=1
go test ./internal/transport -run 'RateLimiter|HostRateLimiter' -count=1
```

Run race coverage for all internal packages, matching the core CI coverage:

```bash
go test -race ./internal/...
```

Useful additional checks:

```bash
go test -cover ./...
go vet ./...
go build ./...
```

The Nix package redirects `HOME` during its checks because tests write configuration files. If you run tests through Nix, use the flake/package definitions rather than a normal home directory.

## Linting and workflow checks

The core lint workflow runs Go linting, Unicode checks, ShellCheck, and actionlint. The corresponding local commands are:

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
go test ./internal/lint/...
find scripts -type f -name '*.sh' -print0 | xargs -0 -r shellcheck
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
```

If ShellCheck is not installed locally, install it using your platform package manager or rely on the Ubuntu CI lint job, which installs it before running the check.

## TUI performance checks

The TUI budget tests are opt-in so ordinary test runs are not affected by shared-host timing variance:

```bash
GOMAXPROCS=2 SURGE_PERF_BUDGET=1 \
  go test ./internal/tui -run '^TestTUI.*RenderPerfBudget$' -count=3 -v
```

Run the rendering benchmarks and compare cached versus invalidated frames:

```bash
go test ./internal/tui -run '^$' \
  -bench '^BenchmarkCPU_(FullView_Old|FullView_New|DashboardPanes_Cached)$' \
  -benchmem -count=3 -benchtime=500ms
```

Capture and inspect profiles when changing rendering code:

```bash
go test ./internal/tui -run '^$' \
  -bench '^BenchmarkCPU_DashboardPanes_Cached$' -benchtime=5s \
  -cpuprofile=.tui.cpu.pprof -memprofile=.tui.mem.pprof

go tool pprof -top -cum .tui.cpu.pprof
go tool pprof -top -sample_index=alloc_space .tui.mem.pprof
rm -f .tui.cpu.pprof .tui.mem.pprof
```

On Linux, `strace -f -c` helps identify syscall overhead and `perf stat -d` helps compare CPU/cache behavior. Keep generated profiles and reports out of commits.

## Browser extension development

```bash
cd extension
npm ci
npm run check
npm run lint
npm test
npm run dev
```

Build extension artifacts with:

```bash
npm run build
npm run build:firefox
npm run zip
npm run zip:firefox
```

## CI behavior

- `Core Build and Release` runs build and race-tested Go tests on Linux, macOS, and Windows.
- The TUI performance job runs only when TUI or Go dependency files change. It reports cached and invalidated measurements in the job summary and uploads a 90-day artifact containing the raw report and metadata.
- The TUI regression job compares the current report with the previous successful baseline on the target branch. It allows normal hosted-runner variance but fails on a material latency or allocation regression.
- `Core Lint` runs Go linting, ShellCheck, actionlint, and the perf-comparison unit tests.

### Inspecting performance history

The TUI performance job uploads two artifacts for each run:

- `tui-perf-<run-number>`: raw test output plus runner/commit metadata
- `tui-perf-comparison-<run-number>`: the baseline comparison report

Find recent successful core-build runs and download an artifact with the GitHub CLI:

```bash
gh run list --workflow core-build.yml --status success --limit 10
gh run download <run-id> -n tui-perf-<run-number> -D .tmp/tui-perf
cat .tmp/tui-perf/tui-perf.txt
cat .tmp/tui-perf/tui-perf-metadata.txt
```

The regression job compares medians against the previous successful run on the target branch. It tolerates up to 25% latency growth and 10% allocation growth to avoid failing on ordinary hosted-runner noise; larger changes fail the job and are included in the step summary.

`act` is not required for local workflow validation. Running actionlint plus the commands above catches YAML, expression, workflow, shell, and test issues without starting a local Actions runner.
