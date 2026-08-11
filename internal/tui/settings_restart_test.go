package tui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/config"
)

func TestRestartRequirementDetection(t *testing.T) {
	m := RootModel{
		Settings: config.DefaultSettings(),
	}

	// 1. Initial state: baseline is nil
	if m.SettingsBaseline != nil {
		t.Fatal("SettingsBaseline should be nil initially")
	}

	// 2. Snapshot
	m.snapshotSettings()
	if m.SettingsBaseline == nil {
		t.Fatal("SettingsBaseline should be set after snapshot")
	}

	// 3. No changes
	if m.checkRestartRequirement() {
		t.Error("checkRestartRequirement() should be false when no changes")
	}

	// 4. Change non-restart setting (e.g. Theme)
	originalTheme := config.Resolve[int](m.Settings.General.Theme)
	m.Settings.General.Theme.Value = (originalTheme + 1) % 3
	if m.checkRestartRequirement() {
		t.Error("checkRestartRequirement() should be false when only non-restart settings changed")
	}

	// 5. Change restart-required setting (e.g. MaxConcurrentDownloads)
	m.Settings.Network.MaxConcurrentDownloads.Value = config.Resolve[int](m.Settings.Network.MaxConcurrentDownloads) + 1
	if !m.checkRestartRequirement() {
		t.Error("checkRestartRequirement() should be true when restart-required setting changed")
	}

	// 6. Reverting should make it false again
	m.Settings.Network.MaxConcurrentDownloads.Value = config.Resolve[int](m.Settings.Network.MaxConcurrentDownloads) - 1
	if m.checkRestartRequirement() {
		t.Error("checkRestartRequirement() should be false when settings are reverted to baseline")
	}
}

func TestDefensiveSnapshotting(t *testing.T) {
	m := RootModel{
		Settings: config.DefaultSettings(),
		state:    SettingsState,
	}
	m.keys = config.DefaultKeyMap()

	// 1. baseline is missing
	if m.SettingsBaseline != nil {
		t.Fatal("SettingsBaseline should be nil for this test")
	}

	// 2. Call updateSettings with a no-op key
	msg := tea.KeyPressMsg{Text: "j"}
	newModel, _ := m.updateSettings(msg)
	res := newModel.(RootModel)

	if res.SettingsBaseline == nil {
		t.Error("updateSettings should have defensively called snapshotSettings")
	}
}

func TestBaselineCleanup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	m := RootModel{
		Settings:         config.DefaultSettings(),
		SettingsBaseline: config.DefaultSettings(),
		state:            SettingsState,
	}
	m.keys = config.DefaultKeyMap()

	// 1. Exit settings using the 'Close' key (e.g. 'esc')
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	if !key.Matches(msg, m.keys.Settings.Close) {
		t.Fatal("Key 'esc' should match Settings.Close")
	}

	newModel, _ := m.updateSettings(msg)
	res := newModel.(RootModel)

	if res.SettingsBaseline != nil {
		t.Error("SettingsBaseline should be cleared when exiting settings to DashboardState")
	}
}
