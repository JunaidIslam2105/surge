package components

import (
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

func confirmationModalPlain(width, height int) string {
	modal := ConfirmationModal{
		Title:            "Quit",
		Message:          "Quit Surge?",
		Detail:           "1 active download will be paused",
		Keys:             NoKeys{},
		Help:             help.New(),
		BorderColor:      lipgloss.Color("99"),
		Width:            width,
		Height:           height,
		ShowYesNoButtons: true,
		YesLabel:         "Yes",
		NoLabel:          "No",
	}
	return plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
}

func addDownloadModalPlain(width, height int) string {
	url := textinput.New()
	url.SetValue("https://example.com/file.zip")
	path := textinput.New()
	path.SetValue(".")
	modal := AddDownloadModal{
		Title:           "Add",
		Inputs:          []textinput.Model{url, path},
		Labels:          []string{"URL:", "Path:"},
		FocusedInput:    0,
		BrowseHintIndex: -1,
		Help:            help.New(),
		HelpKeys:        NoKeys{},
		BorderColor:     lipgloss.Color("99"),
		Width:           width,
		Height:          height,
	}
	return plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
}

func TestModalResizeGoldens(t *testing.T) {
	tests := []struct {
		name   string
		render func() string
		want   string
	}{
		{
			name:   "confirmation wide",
			render: func() string { return confirmationModalPlain(48, 12) },
			want: `╭─ Quit ───────────────────────────────────────╮
│                                              │
│                Quit Surge?                   │
│                                              │
│      1 active download will be paused        │
│                                              │
│              Yes           No                │
│                                              │
│                                              │
│                                              │
│                                              │
╰──────────────────────────────────────────────╯`,
		},
		{
			name:   "add download wide",
			render: func() string { return addDownloadModalPlain(48, 12) },
			want: `╭─ Add ────────────────────────────────────────╮
│                                              │
│  URL:      > https://example.com/file.zip    │
│                                              │
│  Path:     > .                               │
│                                              │
│                                              │
│                                              │
│                                              │
│                                              │
│                                              │
╰──────────────────────────────────────────────╯`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.render()
			if got != tt.want {
				t.Fatalf("modal resize golden mismatch:\n got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
