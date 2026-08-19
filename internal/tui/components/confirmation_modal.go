package components

import (
	"image/color"
	"strings"

	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/utils"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// ConfirmationModal renders a styled confirmation dialog box
type ConfirmationModal struct {
	Title       string
	Message     string
	Detail      string      // Optional additional detail line (e.g., filename, URL)
	Keys        help.KeyMap // Key bindings to show in help
	Help        help.Model  // Help model for rendering keys
	BorderColor color.Color // Border color for the box
	Width       int
	Height      int

	ShowYesNoButtons bool
	YesNoFocused     int
	YesLabel         string
	NoLabel          string
	ButtonColor      color.Color // Active button background color; defaults to colors.Pink() when nil
}

// NoKeys satisfies help.KeyMap for informational modals with no interactive bindings.
type NoKeys struct{}

func (NoKeys) ShortHelp() []key.Binding  { return nil }
func (NoKeys) FullHelp() [][]key.Binding { return nil }

// renderBody handles joining message and detail with a gap and optional wrapping.
func (m ConfirmationModal) renderBody(width int) string {
	msg := m.Message
	det := m.Detail

	if width > 0 {
		msg = utils.WrapText(msg, width)
		if det != "" {
			det = utils.TruncateTwoLines(det, width)
		}
	}

	content := msg
	if det != "" {
		if !strings.Contains(det, "\x1b") {
			det = getDetailStyle().Render(det)
		}
		content = strings.Join([]string{content, "", det}, "\n")
	}

	if m.ShowYesNoButtons {
		yesLabel := m.YesLabel
		if yesLabel == "" {
			yesLabel = "Yep!"
		}
		noLabel := m.NoLabel
		if noLabel == "" {
			noLabel = "Nope"
		}

		content = strings.Join([]string{content, "", renderYesNoButtons(yesLabel, noLabel, m.YesNoFocused, m.ButtonColor)}, "\n")
	}

	return content
}

func renderYesNoButtons(yesLabel, noLabel string, focused int, btnColor color.Color) string {
	pad := "   "

	if btnColor == nil {
		btnColor = colors.Pink()
	}

	activeFirst := lipgloss.NewStyle().Foreground(colors.White()).Background(btnColor).Bold(true).Underline(true)
	activeRest := lipgloss.NewStyle().Foreground(colors.White()).Background(btnColor).Bold(true)
	activePad := lipgloss.NewStyle().Background(btnColor)

	inactiveFirst := lipgloss.NewStyle().Foreground(colors.LightGray()).Background(lipgloss.Color("236")).Underline(true)
	inactiveRest := lipgloss.NewStyle().Foreground(colors.LightGray()).Background(lipgloss.Color("236"))
	inactivePad := lipgloss.NewStyle().Background(lipgloss.Color("236"))

	yesFirst, yesRest := splitFirst(yesLabel)
	noFirst, noRest := splitFirst(noLabel)

	yesFirstStyle, yesRestStyle, yesPadStyle := activeFirst, activeRest, activePad
	noFirstStyle, noRestStyle, noPadStyle := inactiveFirst, inactiveRest, inactivePad
	if focused == 1 {
		yesFirstStyle, yesRestStyle, yesPadStyle = inactiveFirst, inactiveRest, inactivePad
		noFirstStyle, noRestStyle, noPadStyle = activeFirst, activeRest, activePad
	}

	renderBtn := func(padStyle, firstStyle, restStyle lipgloss.Style, first, rest string) string {
		return padStyle.Render(pad) + firstStyle.Render(first) + restStyle.Render(rest) + padStyle.Render(pad)
	}

	yesBtn := renderBtn(yesPadStyle, yesFirstStyle, yesRestStyle, yesFirst, yesRest)
	noBtn := renderBtn(noPadStyle, noFirstStyle, noRestStyle, noFirst, noRest)

	return yesBtn + "     " + noBtn
}

func splitFirst(label string) (string, string) {
	runes := []rune(label)
	if len(runes) == 0 {
		return "", ""
	}
	if len(runes) == 1 {
		return string(runes[0]), ""
	}
	return string(runes[0]), string(runes[1:])
}

// RenderWithBtopBox renders the modal using the btop-style box with title in border
// Help text is pushed to the last line of the modal
func (m ConfirmationModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor color.Color) string,
	titleStyle lipgloss.Style,
) string {
	boxFrameX := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).GetHorizontalFrameSize()
	paddingX := lipgloss.NewStyle().Padding(0, 1).GetHorizontalFrameSize()
	innerWidth := m.Width - boxFrameX - paddingX

	boxFrameY := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).GetVerticalFrameSize()
	innerHeight := m.Height - boxFrameY

	// Get content without help
	// mainContent is defined and populated lower down after wrapping

	// Style and center help text
	helpStyle := lipgloss.NewStyle().
		Foreground(colors.Gray()).
		Width(innerWidth).
		Align(lipgloss.Center)
	helpText := helpStyle.Render(m.Help.View(m.Keys))

	// Ensure message and detail are wrapped to innerWidth and joined
	mainContent := m.renderBody(innerWidth)

	// Calculate heights
	mainContentHeight := lipgloss.Height(mainContent)
	helpHeight := lipgloss.Height(helpText)

	// Space above content to vertically center the main content in remaining space
	spacingStyle := lipgloss.NewStyle().MarginBottom(1)
	remainingHeight := innerHeight - helpHeight - spacingStyle.GetVerticalFrameSize()
	topPadding := (remainingHeight - mainContentHeight) / 2
	if topPadding < 0 {
		topPadding = 0
	}

	// Center main content horizontally
	centeredMain := lipgloss.NewStyle().Width(innerWidth).Align(lipgloss.Center).Render(mainContent)

	// Build final content with help at bottom
	var lines []string
	for i := 0; i < topPadding; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, centeredMain)

	// Add padding to push help to bottom
	spacingNeeded := innerHeight - topPadding - mainContentHeight - helpHeight
	for i := 0; i < spacingNeeded; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, helpText)

	fullContent := strings.Join(lines, "\n")

	// Title goes in the box border
	return renderBox(titleStyle.Render(" "+m.Title+" "), "", fullContent, m.Width, m.Height, m.BorderColor)
}

func getDetailStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(colors.Magenta()).
		Bold(true)
}
