package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestGraphRenderer_Aliasing(t *testing.T) {
	g := NewGraphRenderer()
	width, height := 10, 5
	dataWithValues := []float64{10, 20, 30, 40, 50}
	maxVal := 50.0

	// Render with data to mutate buffers
	g.Render(dataWithValues, width, height, maxVal, false)

	// Render with empty data
	emptyData := []float64{}
	emptyOutput := g.Render(emptyData, width, height, maxVal, false)

	// The empty output should contain the grid lines, not blocks
	// If aliasing occurred, the emptyGrid would have been mutated and blocks would show up
	if strings.Contains(emptyOutput, "\u2588") || strings.Contains(emptyOutput, "\u2584") {
		t.Errorf("GraphRenderer aliased its base grid! Empty render contains blocks:\n%s", emptyOutput)
	}
}

func TestGraphRenderer_GradientOutput(t *testing.T) {
	g := NewGraphRenderer()
	width, height := 10, 11
	dataWithValues := []float64{100, 100, 100, 100, 100} // Full height bars

	// The lowest visual row should be height-1 (the baseline), which uses graphColors()[0]
	out := g.Render(dataWithValues, width, height, 100.0, false)

	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Fatalf("Expected %d lines, got %d", height, len(lines))
	}

	gColors := graphColors()

	tests := []struct {
		visualRow int
		colorIdx  int
		name      string
	}{
		{height - 1, 0, "0% (Bottom)"},
		{height - 1 - 1, 0, "10% threshold"},
		{height - 1 - 2, 1, "20% (Above 10%)"},
		{height - 1 - 3, 1, "30% threshold"},
		{height - 1 - 4, 2, "40% (Above 30%)"},
		{height - 1 - 6, 2, "60% threshold"},
		{height - 1 - 7, 3, "70% (Above 60%)"},
		{0, 3, "100% (Top)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedStyle := lipgloss.NewStyle().Foreground(gColors[tt.colorIdx])
			expectedLine := expectedStyle.Render(strings.Repeat("█", width))
			if lines[tt.visualRow] != expectedLine {
				t.Errorf("Row %d rendered incorrectly.\nGot: %q\nWant: %q", tt.visualRow, lines[tt.visualRow], expectedLine)
			}
		})
	}
}

func TestGraphRenderer_NormalizesNonPositiveMaxForCache(t *testing.T) {
	g := NewGraphRenderer()
	data := []float64{1, 2, 3}

	first := g.Render(data, 10, 5, 0, false)
	if g.lastMax != 1 {
		t.Fatalf("effective max = %v, want 1", g.lastMax)
	}

	second := g.Render(data, 10, 5, 1, false)
	if first != second {
		t.Fatal("equivalent renders with fallback and effective max differ")
	}
	if g.lastMax != 1 {
		t.Fatalf("cached max = %v, want 1", g.lastMax)
	}
}

func TestGraphRenderer_Downsampling(t *testing.T) {
	g := NewGraphRenderer()

	data := make([]float64, 120)
	data[119] = 120.0

	out := g.Render(data, 10, 5, 120.0, false)
	lines := strings.Split(out, "\n")

	gColors := graphColors()
	topExpected := lipgloss.NewStyle().Foreground(gColors[len(gColors)-1]).Render("█")
	if !strings.HasSuffix(lines[0], topExpected) {
		t.Errorf("Tail data point (119) was lost during downsampling! Expected max height on rightmost column.")
	}
}

func TestGraphRenderer_ResizeCache(t *testing.T) {
	g := NewGraphRenderer()

	data := []float64{10, 20, 30}
	out1 := g.Render(data, 10, 5, 50.0, false)

	out2 := g.Render([]float64{99, 99}, 20, 10, 100.0, true)

	if out1 != out2 {
		t.Errorf("Cached render output during resize does not match initial render!")
	}
}

func TestGraphBoxCacheExpiresWhenResizeSettles(t *testing.T) {
	m := fullBenchModel(1)
	stats := m.ComputeViewStats()
	const oldBoxWidth, newBoxWidth, boxHeight = 80, 100, 12

	m.lastResizeTime = time.Time{}
	m.renderGraphBox(oldBoxWidth, boxHeight, stats)
	oldWidth := m.graphRenderer.lastWidth

	m.lastResizeTime = time.Now()
	m.renderGraphBox(newBoxWidth, boxHeight, stats)
	if m.graphRenderer.lastWidth != oldWidth {
		t.Fatal("graph renderer did not reuse its previous render during resize")
	}

	m.lastResizeTime = time.Now().Add(-time.Second)
	m.renderGraphBox(newBoxWidth, boxHeight, stats)
	wantWidth, _ := GetGraphAreaDimensions(newBoxWidth, newBoxWidth < MinGraphStatsWidth)
	if m.graphRenderer.lastWidth != wantWidth {
		t.Fatalf("settled graph width = %d, want %d", m.graphRenderer.lastWidth, wantWidth)
	}
}
