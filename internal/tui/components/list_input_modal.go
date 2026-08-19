package components

import (
	"image/color"
	"strings"

	"github.com/SurgeDM/Surge/internal/tui/colors"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// ListInputItem represents a single key-value row in the modal.
type ListInputItem struct {
	Label       string
	Value       string
	IsEditing   bool
	InputSuffix string
}

// ListInputModal renders a list of items where the active item can be edited inline.
type ListInputModal struct {
	Title       string
	Subtitle    string
	Items       []ListInputItem
	Cursor      int
	Input       textinput.Model
	Help        help.Model
	HelpKeys    help.KeyMap
	BorderColor color.Color
	Width       int
	Height      int
	Error       string
}

// viewContent renders the list items (without box wrapper or help text).
func (m ListInputModal) viewContent() string {
	rows := make([]string, 0, 3*len(m.Items))

	for i, item := range m.Items {
		var prefix string
		var labelStyle lipgloss.Style
		var valueStyle lipgloss.Style

		if i == m.Cursor {
			prefix = lipgloss.NewStyle().Foreground(colors.Pink()).Bold(true).Render("\u25B8 ")
			labelStyle = lipgloss.NewStyle().Foreground(colors.Pink()).Bold(true)
			valueStyle = lipgloss.NewStyle().Foreground(colors.LightGray())
		} else {
			prefix = "  "
			labelStyle = lipgloss.NewStyle().Foreground(colors.Gray())
			valueStyle = lipgloss.NewStyle().Foreground(colors.Gray())
		}

		labelRow := prefix + labelStyle.Render(item.Label)

		var valueStr string
		if item.IsEditing {
			// Show the text input, aligned under the label
			valueStr = "  " + m.Input.View()
			if item.InputSuffix != "" {
				valueStr += " " + lipgloss.NewStyle().Foreground(colors.Gray()).Render(item.InputSuffix)
			}
		} else {
			// Show the text value, aligned under the label
			valueStr = valueStyle.Render("  " + item.Value)
		}

		rows = append(rows, labelRow, valueStr, "")
	}

	return strings.Join(rows, "\n")
}

// RenderWithBtopBox renders the modal using the btop-style box with title in border
// Help text is pushed to the last line of the modal.
func (m ListInputModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor color.Color) string,
	titleStyle lipgloss.Style,
) string {
	boxFrameX := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).GetHorizontalFrameSize()
	paddingX := lipgloss.NewStyle().Padding(0, 1).GetHorizontalFrameSize()
	innerWidth := m.Width - boxFrameX - paddingX

	boxFrameY := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).GetVerticalFrameSize()
	innerHeight := m.Height - boxFrameY

	// Style help text
	helpStyle := lipgloss.NewStyle().
		Foreground(colors.Gray()).
		Width(innerWidth) // Left aligned by default, which fits the design better

	var helpText string
	if m.HelpKeys != nil {
		helpText = helpStyle.Render(m.Help.View(m.HelpKeys))
	}

	mainContent := lipgloss.NewStyle().Padding(1, 2).Render(m.viewContent())

	// Calculate heights
	mainContentHeight := lipgloss.Height(mainContent)
	helpHeight := lipgloss.Height(helpText)

	var errorLine string
	var errorHeight int
	if m.Error != "" {
		errorLine = lipgloss.NewStyle().
			Foreground(colors.StateError()).
			Bold(true).
			Render("  \u2716 " + m.Error)
		errorHeight = lipgloss.Height(errorLine)
	}

	// Ensure we don't overflow unexpectedly, though we assume the caller provided enough height
	var subtitleLine string
	var subtitleHeight int
	if m.Subtitle != "" {
		subtitleLine = lipgloss.NewStyle().
			Foreground(colors.LightGray()).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colors.Magenta()).
			Padding(0, 1).
			MarginLeft(2).
			MarginBottom(1).
			Render(m.Subtitle)
		subtitleHeight = lipgloss.Height(subtitleLine)
	}

	// Add padding to push help to bottom
	spacingNeeded := innerHeight - mainContentHeight - errorHeight - subtitleHeight - helpHeight
	if spacingNeeded < 0 {
		spacingNeeded = 0
	}

	var lines []string
	if subtitleLine != "" {
		lines = append(lines, subtitleLine)
	}
	lines = append(lines, mainContent)

	for i := 0; i < spacingNeeded; i++ {
		lines = append(lines, "")
	}
	if errorLine != "" {
		lines = append(lines, errorLine)
	}
	if helpText != "" {
		lines = append(lines, helpText)
	}

	fullContent := strings.Join(lines, "\n")

	return renderBox(titleStyle.Render(" "+m.Title+" "), "", fullContent, m.Width, m.Height, m.BorderColor)
}
