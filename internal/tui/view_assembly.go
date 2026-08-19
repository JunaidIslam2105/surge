package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// joinVerticalFixed joins panes whose widths and heights were already resolved
// by the dashboard layout. Unlike lipgloss.JoinVertical, it does not remeasure
// every line to calculate padding that the box renderers already supplied.
func joinVerticalFixed(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// joinHorizontalFixed joins panes with equal, precomputed heights. Dashboard
// boxes are rendered at exact widths, so line-wise concatenation is sufficient.
// Keep Lipgloss as a correctness fallback for callers that pass uneven panes.
func joinHorizontalFixed(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}

	lines := make([][]string, len(parts))
	height := -1
	for i, part := range parts {
		lines[i] = strings.Split(part, "\n")
		if height == -1 {
			height = len(lines[i])
		} else if len(lines[i]) != height {
			return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
		}
	}

	var builder strings.Builder
	for row := 0; row < height; row++ {
		if row > 0 {
			builder.WriteByte('\n')
		}
		for _, partLines := range lines {
			builder.WriteString(partLines[row])
		}
	}
	return builder.String()
}
