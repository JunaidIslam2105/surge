package tui

import (
	"os"
	"testing"
	"time"
)

const (
	stableDashboardRenderBudget      = 2 * time.Millisecond
	stableDashboardAllocsBudget      = 850
	invalidatedDashboardRenderBudget = 3 * time.Millisecond
	invalidatedDashboardAllocsBudget = 2200
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
	t.Logf("cached frame: %.0f allocs/op (budget %d)", allocs, stableDashboardAllocsBudget)
	if allocs > stableDashboardAllocsBudget {
		t.Fatalf("stable dashboard frame allocated %.0f objects, budget is %d", allocs, stableDashboardAllocsBudget)
	}

	const iterations = 100
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = model.View()
	}
	perFrame := time.Since(start) / iterations
	t.Logf("cached frame: %v/op (budget %v)", perFrame, stableDashboardRenderBudget)
	if perFrame > stableDashboardRenderBudget {
		t.Fatalf("stable dashboard frame took %v, budget is %v", perFrame, stableDashboardRenderBudget)
	}
}

// TestTUIInvalidatedRenderPerfBudget covers the slower structural-update path
// so a future list rebuild or pane invalidation regression is still visible.
// It shares the opt-in gate with TestTUIRenderPerfBudget because timing budgets
// are intentionally not enforced on ordinary local test runs.
func TestTUIInvalidatedRenderPerfBudget(t *testing.T) {
	if os.Getenv("SURGE_PERF_BUDGET") != "1" {
		t.Skip("set SURGE_PERF_BUDGET=1 to enforce TUI performance budgets")
	}

	model := fullBenchModel(8)
	_ = model.View()

	allocs := testing.AllocsPerRun(10, func() {
		model.cachedTotalSpeed = 0
		model.UpdateListItems()
		_ = model.View()
	})
	t.Logf("invalidated frame: %.0f allocs/op (budget %d)", allocs, invalidatedDashboardAllocsBudget)
	if allocs > invalidatedDashboardAllocsBudget {
		t.Fatalf("invalidated dashboard frame allocated %.0f objects, budget is %d", allocs, invalidatedDashboardAllocsBudget)
	}

	const iterations = 30
	start := time.Now()
	for i := 0; i < iterations; i++ {
		model.cachedTotalSpeed = 0
		model.UpdateListItems()
		_ = model.View()
	}
	perFrame := time.Since(start) / iterations
	t.Logf("invalidated frame: %v/op (budget %v)", perFrame, invalidatedDashboardRenderBudget)
	if perFrame > invalidatedDashboardRenderBudget {
		t.Fatalf("invalidated dashboard frame took %v, budget is %v", perFrame, invalidatedDashboardRenderBudget)
	}
}
