package components

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// HelpModal renders a styled help overlay showing all keyboard shortcuts
type HelpModal struct {
	Title       string
	HelpKeys    help.KeyMap // Must implement FullHelp() [][]key.Binding
	Help        help.Model  // Help model for rendering
	BorderColor color.Color
	Width       int
	Height      int
}

// RenderWithBtopBox renders the help modal using a btop-style box with title in border
func (m HelpModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor color.Color) string,
	titleStyle lipgloss.Style,
) string {
	boxFrameX := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).GetHorizontalFrameSize()
	paddingX := lipgloss.NewStyle().Padding(0, 1).GetHorizontalFrameSize()
	innerWidth := m.Width - boxFrameX - paddingX

	// Render the full help view from the keybindings
	h := m.Help
	h.SetWidth(innerWidth)
	helpText := h.FullHelpView(m.HelpKeys.FullHelp())

	// Wrap to box width and center as a block, preserving column alignment
	helpContent := lipgloss.PlaceHorizontal(innerWidth, lipgloss.Center, helpText)

	boxFrameY := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).GetVerticalFrameSize()
	innerHeight := m.Height - boxFrameY
	contentHeight := lipgloss.Height(helpContent)
	topPadding := (innerHeight - contentHeight) / 2
	if topPadding < 0 {
		topPadding = 0
	}

	var lines []string
	for i := 0; i < topPadding; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, helpContent)

	spacingNeeded := innerHeight - topPadding - contentHeight
	for i := 0; i < spacingNeeded; i++ {
		lines = append(lines, "")
	}

	fullContent := "\n" + strings.Join(lines, "\n") + "\n"

	return renderBox("", fmt.Sprintf("  %s  ", titleStyle.Render(m.Title)), fullContent, m.Width, m.Height, m.BorderColor)
}
