package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/SurgeDM/Surge/internal/config"
)

func TestViewsUseConfiguredKeysInContextualHints(t *testing.T) {
	m := InitialRootModel(1701, "test", nil, nil, nil, false)
	m.width, m.height = 120, 35
	m.keys = config.DefaultKeyMap()

	m.keys.Dashboard.Search = key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "search"))
	m.searchQuery = "release"
	if got := ansiEscapeRE.ReplaceAllString(m.View().Content, ""); !strings.Contains(got, "[ctrl+f] Clear") {
		t.Fatalf("search hint does not use configured key: %q", got)
	}

	m.keys.Input.Tab = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "browse/next"))
	m.state = InputState
	if got := ansiEscapeRE.ReplaceAllString(m.View().Content, ""); !strings.Contains(got, "[ctrl+b] Browse") {
		t.Fatalf("browse hint does not use configured key: %q", got)
	}

	m.keys.Settings.PrevTab = key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "previous tab"))
	if got := ansiEscapeRE.ReplaceAllString(m.renderSettingsHelp(55), ""); !strings.Contains(got, "z previous tab") {
		t.Fatalf("compact settings help does not use configured key: %q", got)
	}
}

func TestHelpModalShowsAndTogglesItsShortcut(t *testing.T) {
	m := InitialRootModel(1701, "test", nil, nil, nil, false)
	m.width, m.height = 120, 35
	m.state = HelpModalState
	m.keys = config.DefaultKeyMap()

	plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(plain, "h") || !strings.Contains(plain, "toggle help") {
		t.Fatalf("help modal does not show its toggle shortcut: %q", plain)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if got := updated.(RootModel).state; got != DashboardState {
		t.Fatalf("help toggle did not close modal: state %d", got)
	}
}

func TestSettingsActionsUseConfiguredKeys(t *testing.T) {
	m := InitialRootModel(1701, "test", nil, nil, config.DefaultSettings(), false)
	m.keys = config.DefaultKeyMap()
	m.keys.Settings.Edit = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	m.keys.Settings.Browse = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "browse"))

	link := []config.SettingMeta{{Key: "test", Label: "Test", Type: config.TypeLink}}
	if got := ansiEscapeRE.ReplaceAllString(m.renderSettingsDetailBlock(link, 0, map[string]interface{}{}, 40, 6), ""); !strings.Contains(got, "[e] Open") {
		t.Fatalf("settings action does not use configured edit key: %q", got)
	}

	directory := []config.SettingMeta{{Key: "default_download_dir", Label: "Directory", Type: config.TypeString}}
	values := map[string]interface{}{"default_download_dir": "/tmp"}
	if got := ansiEscapeRE.ReplaceAllString(m.renderSettingsDetailBlock(directory, 0, values, 40, 6), ""); !strings.Contains(got, "[b] Browse") {
		t.Fatalf("settings browse action does not use configured key: %q", got)
	}
}
