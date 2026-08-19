package tui

import (
	"os"
	"testing"
	"time"
)

const (
	stableDashboardRenderBudget = 2 * time.Millisecond
	stableDashboardAllocsBudget = 850
)

// TestTUIRenderPerfBudget is opt-in because wall-clock budgets are sensitive to
// shared CI hosts. Run it with SURGE_PERF_BUDGET=1 to enforce both the stable
// cached-frame latency and allocation budgets.
func TestTUIRenderPerfBudget(t *testing.T) {
	if os.Getenv("SURGE_PERF_BUDGET") != "1" {
		t.Skip("set SURGE_PERF_BUDGET=1 to enforce TUI performance budgets")
	}

	model := fullBenchModel(8)
	_ = model.View() // warm all pane caches

	allocs := testing.AllocsPerRun(20, func() {
		_ = model.View()
	})
	if allocs > stableDashboardAllocsBudget {
		t.Fatalf("stable dashboard frame allocated %.0f objects, budget is %d", allocs, stableDashboardAllocsBudget)
	}

	const iterations = 100
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = model.View()
	}
	perFrame := time.Since(start) / iterations
	if perFrame > stableDashboardRenderBudget {
		t.Fatalf("stable dashboard frame took %v, budget is %v", perFrame, stableDashboardRenderBudget)
	}
}
