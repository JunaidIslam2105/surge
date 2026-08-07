package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/SurgeDM/Surge/internal/tui/colors"
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
	width, height := 10, 5
	dataWithValues := []float64{100, 100, 100, 100, 100} // Full height bars

	// The lowest visual row should be height-1 (the baseline), which uses colors.ProgressStart()
	out := g.Render(dataWithValues, width, height, 100.0, false)

	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Fatalf("Expected %d lines, got %d", height, len(lines))
	}

	// Top row (visual index 0) should use ProgressEnd
	// Bottom row (visual index height-1) should use ProgressStart
	topStyle := lipgloss.NewStyle().Foreground(colors.ProgressEnd())
	bottomStyle := lipgloss.NewStyle().Foreground(colors.ProgressStart())

	topExpected := topStyle.Render(strings.Repeat("\u2588", width))
	// The bottom row is drawn on top of the grid base, but the whole block uses the row style
	bottomExpected := bottomStyle.Render(strings.Repeat("\u2588", width))

	if lines[0] != topExpected {
		t.Errorf("Top row rendered incorrectly.\nGot: %q\nWant: %q", lines[0], topExpected)
	}

	if lines[height-1] != bottomExpected {
		t.Errorf("Bottom row rendered incorrectly.\nGot: %q\nWant: %q", lines[height-1], bottomExpected)
	}
}

func TestGraphRenderer_Downsampling(t *testing.T) {
	g := NewGraphRenderer()

	data := make([]float64, 120)
	for i := range data {
		data[i] = float64(i)
	}

	out := g.Render(data, 10, 5, 120.0, false)
	lines := strings.Split(out, "\n")

	topExpected := lipgloss.NewStyle().Foreground(colors.ProgressEnd()).Render("█")
	if !strings.HasSuffix(lines[0], topExpected) {
		t.Errorf("Tail data point (119) was lost during downsampling! Expected max height on rightmost column.")
	}
}

func TestGraphRenderer_ResizeCache(t *testing.T) {
	g := NewGraphRenderer()

	data := []float64{10, 20, 30}
	out1 := g.Render(data, 10, 5, 50.0, false)

	out2 := g.Render(data, 10, 5, 50.0, true)

	if out1 != out2 {
		t.Errorf("Cached render output during resize does not match initial render!")
	}
}
