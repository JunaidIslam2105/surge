package components

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

func TestFilePickerWindowsPathGolden(t *testing.T) {
	picker := filepicker.New()
	picker.CurrentDirectory = `C:\Users\Test\Downloads`
	picker.SetHeight(3)

	modal := FilePickerModal{
		Title:       " Select Directory ",
		Picker:      &picker,
		Help:        help.New(),
		HelpKeys:    NoKeys{},
		BorderColor: lipgloss.Color("99"),
		Width:       44,
		Height:      10,
	}
	got := plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
	got = strings.ReplaceAll(got, `\`, "/")
	want := `╭─ Select Directory ───────────────────────╮
│                                          │
│  C:/Users/Test/Downloads                 │
│                                          │
│    Bummer. No Files Found.               │
│                                          │
│                                          │
│                                          │
│                                          │
╰──────────────────────────────────────────╯`
	if got != want {
		t.Fatalf("Windows path golden mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}
