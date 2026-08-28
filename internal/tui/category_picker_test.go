package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/config"
)

func TestCategoryPickerAppliesSelectedFilter(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Categories.CategoryEnabled.Value = true
	settings.Categories.Categories = []config.Category{{Name: "Videos", Pattern: `\.mp4$`, Path: t.TempDir()}}
	m := RootModel{
		state:                CategoryPickerState,
		Settings:             settings,
		keys:                 config.DefaultKeyMap(),
		list:                 NewDownloadList(80, 20),
		categoryPickerCursor: 1,
	}

	updated, _ := m.updateCategoryPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(RootModel)
	if m2.state != DashboardState || m2.categoryFilter != "Videos" {
		t.Fatalf("picker result = state %v, filter %q", m2.state, m2.categoryFilter)
	}
	if len(m2.logEntries) != 1 || !strings.Contains(m2.logEntries[0], "Filter: Videos") || strings.Contains(m2.logEntries[0], "\U0001F4C2") {
		t.Fatalf("unexpected filter log: %#v", m2.logEntries)
	}
}
