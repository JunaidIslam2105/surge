package components

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

var modalANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainModal(s string) string {
	return modalANSI.ReplaceAllString(s, "")
}

type goldenHelpKeys struct{}

func (goldenHelpKeys) ShortHelp() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))}
}

func (goldenHelpKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))}}
}

func TestFilePickerModalGolden(t *testing.T) {
	tmpDir := t.TempDir()
	picker := filepicker.New()
	picker.CurrentDirectory = tmpDir
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
	lines := strings.Split(got, "\n")
	for i, line := range lines {
		if strings.Contains(line, filepath.Base(tmpDir)) || strings.Contains(line, "TestFilePickerModalGolden") {
			lines[i] = "│  <tmp>" + strings.Repeat(" ", 35) + "│"
		}
	}
	got = strings.Join(lines, "\n")
	want := `╭─ Select Directory ───────────────────────╮
│                                          │
│  <tmp>                                   │
│                                          │
│    Bummer. No Files Found.               │
│                                          │
│                                          │
│                                          │
│                                          │
╰──────────────────────────────────────────╯`
	if got != want {
		t.Fatalf("file-picker golden mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
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
		{
			name: "help narrow",
			render: func() string {
				modal := HelpModal{
					Title:       "Help",
					HelpKeys:    goldenHelpKeys{},
					Help:        help.New(),
					BorderColor: lipgloss.Color("99"),
					Width:       40,
					Height:      10,
				}
				return plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
			},
			want: `╭─────────────────────────────  Help  ─╮
│                                      │
│                                      │
│                                      │
│                                      │
│            enter select              │
│                                      │
│                                      │
│                                      │
╰──────────────────────────────────────╯`,
		},
		{
			name: "list input narrow",
			render: func() string {
				input := textinput.New()
				input.SetValue("42")
				modal := ListInputModal{
					Title:       "Limits",
					Items:       []ListInputItem{{Label: "Global", Value: "10 MB/s"}, {Label: "Workers", Value: "42", IsEditing: true}},
					Cursor:      1,
					Input:       input,
					Help:        help.New(),
					HelpKeys:    NoKeys{},
					BorderColor: lipgloss.Color("99"),
					Width:       40,
					Height:      12,
				}
				return plainModal(modal.RenderWithBtopBox(RenderBtopBox, lipgloss.NewStyle()))
			},
			want: `╭─ Limits ─────────────────────────────╮
│                                      │
│    Global                            │
│    10 MB/s                           │
│                                      │
│  ▸ Workers                           │
│    > 42                              │
│                                      │
│                                      │
│                                      │
│                                      │
╰──────────────────────────────────────╯`,
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
