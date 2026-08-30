package components

import (
	"image/color"
	"strings"

	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/utils"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// AddDownloadModal renders input-driven download forms (add download / extension prompt).
type AddDownloadModal struct {
	Title           string
	Inputs          []textinput.Model
	Labels          []string
	FocusedInput    int
	ShowURL         bool
	URL             string
	BrowseHintIndex int
	BrowseHintKey   string
	Help            help.Model
	HelpKeys        help.KeyMap
	BorderColor     color.Color
	Width           int
	Height          int
}

// View renders the inner content (without border box).
func (m AddDownloadModal) View() string {
	labelStyle := lipgloss.NewStyle().Width(10).Foreground(colors.LightGray())
	hintBase := lipgloss.NewStyle().MarginLeft(1).Foreground(colors.LightGray())
	content := []string{""}

	if m.ShowURL && m.URL != "" {
		horizontalPadding := lipgloss.NewStyle().Padding(0, 2).GetHorizontalFrameSize()
		innerWidth := m.Width - BorderFrameWidth - horizontalPadding
		wrappedURL := utils.TruncateTwoLines(m.URL, innerWidth-5) // Offset for "URL: "
		content = append(content,
			lipgloss.NewStyle().Foreground(colors.LightGray()).Render("URL: "+wrappedURL),
			"",
		)
	}

	for i := 0; i < len(m.Inputs) && i < len(m.Labels); i++ {
		// Calculate available width for inputs
		// Accounts for: horizontal padding (4), label (10), and box borders (2)
		const labelWidth = 10
		horizontalPadding := lipgloss.NewStyle().Padding(0, 2).GetHorizontalFrameSize()
		inputW := m.Width - BorderFrameWidth - horizontalPadding - labelWidth

		if m.BrowseHintIndex == i {
			inputW -= lipgloss.Width(m.browseHint()) + 1
		}
		if inputW < 10 {
			inputW = 10
		}
		ti := m.Inputs[i]
		ti.SetWidth(inputW)

		rowParts := []string{labelStyle.Render(m.Labels[i]), ti.View()}
		if m.BrowseHintIndex == i {
			hintStyle := hintBase
			if m.FocusedInput == i {
				hintStyle = hintStyle.Foreground(colors.Pink())
			}
			rowParts = append(rowParts, hintStyle.Render(m.browseHint()))
		}
		content = append(content, lipgloss.JoinHorizontal(lipgloss.Left, rowParts...), "")
	}

	content = append(content, m.Help.View(m.HelpKeys))
	return lipgloss.NewStyle().Padding(0, 2).Render(strings.Join(content, "\n"))
}

func (m AddDownloadModal) browseHint() string {
	key := m.BrowseHintKey
	if key == "" {
		key = "tab"
	}
	return "[" + key + "] Browse"
}

// RenderWithBtopBox renders the modal with btop-style border.
func (m AddDownloadModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor color.Color) string,
	titleStyle lipgloss.Style,
) string {
	return renderBox(titleStyle.Render(" "+m.Title+" "), "", m.View(), m.Width, m.Height, m.BorderColor)
}
