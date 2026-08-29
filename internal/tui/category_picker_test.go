package tui

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
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
		categoryPickerCursor: 2,
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

func TestCategoryPickerOptionsPutSpecialChoicesFirst(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Categories.Categories = []config.Category{{Name: "Videos"}, {Name: "Archives"}}
	m := RootModel{Settings: settings}
	want := []string{"", "Uncategorized", "Videos", "Archives"}
	if got := m.categoryPickerOptions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}
}

func TestCategoryPickerAssignsSelectedDownload(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Categories.CategoryEnabled.Value = true
	settings.Categories.Categories = []config.Category{{Name: "Videos", Pattern: `\.mp4$`}}
	d := NewDownloadModel("id", "https://example.com/file.zip", "file.zip", 0)
	m := RootModel{
		state: CategoryPickerState, Settings: settings, keys: config.DefaultKeyMap(),
		list: NewDownloadList(80, 20), downloads: []*DownloadModel{d}, SelectedDownloadID: d.ID,
		categoryPickerAssign: true, categoryPickerTarget: d.ID, categoryPickerCursor: 1,
	}

	updated, _ := m.updateCategoryPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(RootModel)
	if d.categoryOverride == nil || *d.categoryOverride != "Videos" {
		t.Fatalf("category override = %v, want Videos", d.categoryOverride)
	}
	m2.categoryFilter = "Videos"
	if m2.categoryLabel(d) != "Videos" || !m2.matchesCategoryFilter(d) {
		t.Fatalf("assignment not reflected: filter=%q label=%q", m2.categoryFilter, m2.categoryLabel(d))
	}
	if !strings.Contains(m2.logEntries[0], "Category: Videos") {
		t.Fatalf("unexpected assignment log: %#v", m2.logEntries)
	}
}

func TestCategoryPickerCompactViewHasNoBlankOptionRows(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Categories.Categories = []config.Category{{Name: "Videos", Description: "Movie files"}}
	m := RootModel{Settings: settings, width: 80, height: 30, keys: config.DefaultKeyMap()}
	view := testAnsiEscapeRE.ReplaceAllString(m.viewCategoryPicker(), "")
	if strings.Contains(view, "Show every download\n\n") || strings.Contains(view, "No matching category\n\n") {
		t.Fatalf("category picker contains blank rows between options:\n%s", view)
	}
}

func TestDashboardUsesConfiguredCategoryFilterKey(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Categories.CategoryEnabled.Value = true
	settings.Categories.Categories = []config.Category{{Name: "Videos"}}
	keys := config.DefaultKeyMap()
	keys.Dashboard.CategoryFilter = key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "category"))
	m := RootModel{state: DashboardState, Settings: settings, keys: keys, list: NewDownloadList(80, 20)}

	updated, _ := m.updateDashboard(tea.KeyPressMsg{Code: 'z', Text: "z"})
	m2 := updated.(RootModel)
	if m2.state != CategoryPickerState || m2.categoryPickerAssign {
		t.Fatalf("custom category key opened state=%v assign=%t", m2.state, m2.categoryPickerAssign)
	}
}

func TestCategoryLabelMatchesFilterFallbackAndDisabledState(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Categories.CategoryEnabled.Value = true
	settings.Categories.Categories = []config.Category{{Name: "Archives", Pattern: `\.zip$`}}
	d := NewDownloadModel("id", "https://example.com/releases/file.zip", "Queued", 0)
	m := RootModel{Settings: settings, categoryFilter: "Archives"}
	if got := m.categoryLabel(d); got != "Archives" || !m.matchesCategoryFilter(d) {
		t.Fatalf("URL fallback label=%q match=%t", got, m.matchesCategoryFilter(d))
	}
	settings.Categories.CategoryEnabled.Value = false
	if got := m.categoryLabel(d); got != "" {
		t.Fatalf("disabled categories produced label %q", got)
	}
}
