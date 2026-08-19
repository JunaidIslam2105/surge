package components

import (
	"regexp"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

var modalANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainModal(s string) string {
	return modalANSI.ReplaceAllString(s, "")
}

func TestModalGoldens(t *testing.T) {
	tests := []struct {
		name   string
		render func() string
		want   string
	}{
		{
			name: "confirmation narrow",
			render: func() string {
				modal := ConfirmationModal{
					Title:            "Quit",
					Message:          "Quit Surge?",
					Detail:           "1 active download will be paused",
					Keys:             NoKeys{},
					Help:             help.New(),
					BorderColor:      lipgloss.Color("99"),
					Width:            36,
					Height:           10,
					ShowYesNoButtons: true,
					YesLabel:         "Yes",
					NoLabel:          "No",
				}
				return plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
			},
			want: `╭─ Quit ───────────────────────────╮
│          Quit Surge?             │
│                                  │
│1 active download will be paused  │
│                                  │
│        Yes           No          │
│                                  │
│                                  │
│                                  │
╰──────────────────────────────────╯`,
		},
		{
			name: "add download narrow",
			render: func() string {
				url := textinput.New()
				url.SetValue("https://example.com/file.zip")
				path := textinput.New()
				path.SetValue(".")
				modal := AddDownloadModal{
					Title:           "Add",
					Inputs:          []textinput.Model{url, path},
					Labels:          []string{"URL:", "Path:"},
					FocusedInput:    0,
					ShowURL:         false,
					BrowseHintIndex: -1,
					Help:            help.New(),
					HelpKeys:        NoKeys{},
					BorderColor:     lipgloss.Color("99"),
					Width:           36,
					Height:          10,
				}
				return plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
			},
			want: `╭─ Add ────────────────────────────╮
│                                  │
│  URL:      > https://example.com/│
│                                  │
│  Path:     > .                   │
│                                  │
│                                  │
│                                  │
│                                  │
╰──────────────────────────────────╯`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.render()
			if got != tt.want {
				t.Fatalf("modal golden mismatch:\n got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
