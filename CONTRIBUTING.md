# Contributing

Thanks for contributing to Surge. Start with the [Development Guide](docs/DEVELOPMENT.md) for prerequisites and the complete local workflow.

## Quick start

From the repository root:

```bash
go mod download
go test ./...
go test -race ./internal/...
```

The core Go workflow uses the dependencies pinned in `go.mod` and `go.sum`. The Python performance scripts require Python 3.8+ but only use the standard library, so no pip install is needed. Browser-extension work additionally requires Node.js 22+ and `npm ci` in `extension/`; ShellCheck and actionlint are optional locally and are enforced by CI.

For local configuration isolation while running the TUI or server:

```bash
XDG_CONFIG_HOME="$(mktemp -d)" XDG_CACHE_HOME="$(mktemp -d)" go run .
```

## Codebase map

- `cmd/`: Cobra commands, startup wiring, and CLI/server entry points.
- `internal/orchestrator/`: lifecycle management, enqueueing, pause/resume, and event coordination.
- `internal/scheduler/`: queued/active download scheduling, rate-limit pools, and shutdown behavior.
- `internal/strategy/concurrent/`: ranged, mirrored, retried, hedged, and health-monitored downloads.
- `internal/strategy/single/`: single-connection fallback downloads and throttled streaming.
- `internal/probe/`: server capability and metadata probing.
- `internal/transport/`: network pools, host penalties, and byte rate limiters.
- `internal/progress/`: live download state, chunk maps, and progress aggregation.
- `internal/store/`: persisted download and resume state.
- `internal/tui/`: Bubble Tea model/update loop, dashboard panes, modal components, and render tests.
- `internal/testutil/`: mock servers, temporary directories, and test helpers.
- `extension/`: WXT/Solid browser extension source and tests.

## Focused checks

Use focused package tests while iterating:

```bash
go test ./internal/tui ./internal/tui/components -count=1
go test ./internal/strategy/concurrent -count=1
go test ./internal/strategy/single -count=1
go test ./internal/transport -run 'RateLimiter|HostRateLimiter' -count=1
```

For TUI rendering changes, also run the opt-in budgets and rendering benchmarks:

```bash
GOMAXPROCS=2 SURGE_PERF_BUDGET=1 \
  go test ./internal/tui -run '^TestTUI.*RenderPerfBudget$' -count=3 -v
go test ./internal/tui -run '^$' \
  -bench '^BenchmarkCPU_(FullView_Old|FullView_New|DashboardPanes_Cached)$' \
  -benchmem -count=3 -benchtime=500ms
```

For workflow or shell changes:

```bash
find scripts -type f -name '*.sh' -print0 | xargs -0 -r shellcheck
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
```

## Pull requests

- Keep each PR focused and explain the user-visible motivation.
- Add regression tests for behavior changes and golden tests for terminal rendering changes.
- Preserve existing platform behavior; avoid assuming Linux unless the code path is platform-specific.
- Run `gofmt` on changed Go files.
- Run `go test ./...` and, for core changes, `go test -race ./internal/...`.
- Update `README.md`, `docs/`, or CLI help when setup, behavior, or user-facing commands change.
- Do not commit generated binaries, profiles, perf reports, temporary config directories, or downloaded artifacts.

CI runs the full race-tested Go suite across the supported operating systems, TUI performance budgets when relevant files change, ShellCheck, actionlint, and extension checks when extension files change.
