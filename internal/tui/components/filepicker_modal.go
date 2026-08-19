package components

import (
	"image/color"
	"strings"

	"github.com/SurgeDM/Surge/internal/tui/colors"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// FilePickerModal represents a styled file picker modal
type FilePickerModal struct {
	Title       string
	Picker      *filepicker.Model
	Help        help.Model
	HelpKeys    help.KeyMap
	BorderColor color.Color
	Width       int
	Height      int
}

// NewFilePickerModal creates a file picker modal with default styling
func NewFilePickerModal(title string, picker *filepicker.Model, helpModel help.Model, helpKeys help.KeyMap, borderColor color.Color) FilePickerModal {
	return FilePickerModal{
		Title:       title,
		Picker:      picker,
		Help:        helpModel,
		HelpKeys:    helpKeys,
		BorderColor: borderColor,
		Width:       90,
		Height:      20,
	}
}

// View returns the inner content of the file picker (without the box)
func (m FilePickerModal) View() string {
	pathStyle := lipgloss.NewStyle().Foreground(colors.LightGray())

	content := strings.Join([]string{
		"",
		pathStyle.Render(m.Picker.CurrentDirectory),
		"",
		m.Picker.View(),
		"",
		m.Help.View(m.HelpKeys),
	}, "\n")

	return lipgloss.NewStyle().Padding(0, 2).Render(content)
}

// RenderWithBtopBox renders the modal using the btop-style box
func (m FilePickerModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor color.Color) string,
	titleStyle lipgloss.Style,
) string {
	return renderBox(titleStyle.Render(m.Title), "", m.View(), m.Width, m.Height, m.BorderColor)
}
