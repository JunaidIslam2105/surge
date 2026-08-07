package tui

import (
	"fmt"
	"testing"
	"time"

	"charm.land/bubbles/v2/progress"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/types"
)

// benchModel builds a realistic RootModel with n active downloads for benchmarking.
func benchModel(n int) *RootModel {
	InitializeTUI()
	IsTestMode = true

	settings := config.DefaultSettings()
	downloads := make([]*DownloadModel, n)
	for i := range downloads {
		id := fmt.Sprintf("dl-%d", i)
		downloads[i] = &DownloadModel{
			ID:         id,
			Filename:   fmt.Sprintf("file-%d.iso", i),
			Total:      1024 * 1024 * 1024, // 1 GiB
			Downloaded: int64(i) * 100 * 1024 * 1024,
			Speed:      float64((i + 1)) * 5 * 1024 * 1024, // 5-40 MiB/s
			started:    true,
			progress: progress.New(
				progress.WithSpringOptions(0.5, 0.1),
				progress.WithColors(colors.ProgressStart(), colors.ProgressEnd()),
				progress.WithScaled(true),
			),
		}
	}

	m := &RootModel{
		downloads:    downloads,
		width:        120,
		height:       35,
		activeTab:    TabActive,
		pinnedTab:    -1,
		SpeedHistory: make([]float64, GraphHistoryPoints),
		Settings:      settings,
		list:          NewDownloadList(80, 20),
		graphRenderer: NewGraphRenderer(),
		// Seed some history so graph has data to render
		lastSpeedHistoryUpdate: time.Now().Add(-time.Second),
	}
	for i := range m.SpeedHistory {
		m.SpeedHistory[i] = float64(i) * 1024 * 1024
	}
	m.cachedTotalSpeed = m.calcTotalSpeedBps()
	m.UpdateListItems()
	return m
}

func benchProgressMsg(m *RootModel) types.DownloadEvent {
	return types.DownloadEvent{
		Type:        types.EventProgress,
		DownloadID:  m.downloads[0].ID,
		Downloaded:  m.downloads[0].Downloaded + 1024*1024,
		Total:       m.downloads[0].Total,
		Speed:       m.downloads[0].Speed,
		Elapsed:     10 * time.Second,
		Connections: 8,
	}
}

// --- Benchmark 1: processProgressMsg ---
// Old path called UpdateListItems() on every progress event.
// New path skips it (list items hold live pointers).

func BenchmarkCPU_ProgressMsg_Old(b *testing.B) {
	m := benchModel(8)
	msg := benchProgressMsg(m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.processProgressMsg(msg)
		m.UpdateListItems() // OLD: was called every progress event
	}
}

func BenchmarkCPU_ProgressMsg_New(b *testing.B) {
	m := benchModel(8)
	msg := benchProgressMsg(m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.processProgressMsg(msg) // NEW: no UpdateListItems
	}
}

// --- Benchmark 2: calcTotalSpeedBps called 3x vs 1x per frame ---

func BenchmarkCPU_SpeedCalc_Old(b *testing.B) {
	m := benchModel(8)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// OLD: computed in processProgressMsg, footer, and renderGraphBox
		_ = m.calcTotalSpeedBps()
		_ = m.calcTotalSpeedBps()
		_ = m.calcTotalSpeedBps()
	}
}

func BenchmarkCPU_SpeedCalc_New(b *testing.B) {
	m := benchModel(8)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// NEW: computed once, cached
		m.cachedTotalSpeed = m.calcTotalSpeedBps()
		_ = m.cachedTotalSpeed
		_ = m.cachedTotalSpeed
	}
}

// --- Benchmark 3: renderGraphBox highly optimized ---

func BenchmarkCPU_GraphRender_New(b *testing.B) {
	m := fullBenchModel(8)
	stats := m.ComputeViewStats()
	layout := CalculateDashboardLayout(m.width, m.height)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// NEW: renderGraphBox uses highly optimized GraphRenderer with RLE
		_ = m.renderGraphBox(layout.RightWidth, layout.GraphHeight, stats)
	}
}

// --- Benchmark 4: Full View() render (the combined effect) ---

// fullBenchModel returns a properly-initialized RootModel (with help, keys, etc.)
// populated with n active downloads.
func fullBenchModel(n int) RootModel {
	IsTestMode = true
	InitializeTUI()
	m := InitialRootModel(1701, "1.0.0", nil, nil, config.DefaultSettings(), false)
	m.width = 120
	m.height = 35
	m.activeTab = TabActive

	downloads := make([]*DownloadModel, n)
	for i := range downloads {
		downloads[i] = &DownloadModel{
			ID:         fmt.Sprintf("dl-%d", i),
			Filename:   fmt.Sprintf("file-%d.iso", i),
			Total:      1024 * 1024 * 1024,
			Downloaded: int64(i) * 100 * 1024 * 1024,
			Speed:      float64(i+1) * 5 * 1024 * 1024,
			started:    true,
			progress: progress.New(
				progress.WithSpringOptions(0.5, 0.1),
				progress.WithColors(colors.ProgressStart(), colors.ProgressEnd()),
				progress.WithScaled(true),
			),
		}
	}
	m.downloads = downloads
	for i := range m.SpeedHistory {
		m.SpeedHistory[i] = float64(i) * 1024 * 1024
	}
	m.cachedTotalSpeed = m.calcTotalSpeedBps()
	m.UpdateListItems()
	return m
}

func BenchmarkCPU_FullView_Old(b *testing.B) {
	m := fullBenchModel(8)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate old per-event overhead before each View
		m.cachedTotalSpeed = 0 // force stale (simulates no cache)
		m.UpdateListItems()    // OLD: called per progress event
		_ = m.View()
	}
}

func BenchmarkCPU_FullView_New(b *testing.B) {
	m := fullBenchModel(8)
	m.cachedTotalSpeed = m.calcTotalSpeedBps()
	// Pre-warm graph cache (as would happen after first render)
	_ = m.View()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// NEW: no list rebuild, speed cached, graph cached
		_ = m.View()
	}
}
