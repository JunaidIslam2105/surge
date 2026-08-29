package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()
	if km == nil {
		t.Fatal("DefaultKeyMap returned nil")
	}
	if len(km.Dashboard.Quit.Keys()) == 0 {
		t.Error("Default Dashboard.Quit keys should not be empty")
	}

	// Verify OpenFolder default binding
	if len(km.Dashboard.OpenFolder.Keys()) == 0 || km.Dashboard.OpenFolder.Keys()[0] != "O" {
		t.Errorf("Default Dashboard.OpenFolder should have key 'O', got %v", km.Dashboard.OpenFolder.Keys())
	}

	// Verify OpenFolder is in FullHelp
	foundOpenFolder := false
	for _, row := range km.Dashboard.FullHelp() {
		for _, b := range row {
			if b.Help().Desc == "open folder" {
				foundOpenFolder = true
				break
			}
		}
	}
	if !foundOpenFolder {
		t.Error("Dashboard.OpenFolder missing from FullHelp")
	}
}

func TestShiftedDashboardBindingsUseReadableHelpLabels(t *testing.T) {
	km := DefaultKeyMap()
	for name, binding := range map[string]key.Binding{
		"add from clipboard": km.Dashboard.AddFromClipboard,
		"purge":              km.Dashboard.PurgeFile,
		"speed limits":       km.Dashboard.SpeedLimits,
		"open folder":        km.Dashboard.OpenFolder,
		"category":           km.Dashboard.CategoryFilter,
	} {
		if !strings.HasPrefix(binding.Help().Key, "shift+") {
			t.Errorf("%s help key = %q; want shift+ form", name, binding.Help().Key)
		}
	}
}

func TestApplyConfigFormatsUppercaseKeysAsShift(t *testing.T) {
	km := DefaultKeyMap()
	km.ApplyConfig(&KeyMapConfig{Dashboard: map[string]KeyBindingConfig{
		"PurgeFile": {Keys: []string{"X", "shift+x"}, Help: "purge"},
	}})
	if got := km.Dashboard.PurgeFile.Help().Key; got != "shift+x" {
		t.Fatalf("loaded uppercase help key = %q, want shift+x", got)
	}
}

func TestApplyConfigMigratesLegacyCategoryFilterDefault(t *testing.T) {
	km := DefaultKeyMap()
	km.ApplyConfig(&KeyMapConfig{Dashboard: map[string]KeyBindingConfig{
		"CategoryFilter": {Keys: []string{"c"}, Help: "category"},
	}})
	if got := km.Dashboard.CategoryFilter.Help().Key; got != "shift+c" {
		t.Fatalf("legacy category filter remained %q, want shift+c", got)
	}
	if got := km.Dashboard.AssignCategory.Help().Key; got != "c" {
		t.Fatalf("assign category key = %q, want c", got)
	}
}

func TestKeyMapConversion(t *testing.T) {
	km := DefaultKeyMap()
	cfg := km.ToConfig()

	if cfg == nil {
		t.Fatal("ToConfig returned nil")
	}

	// Verify some fields
	if len(cfg.Dashboard["Quit"].Keys) == 0 {
		t.Error("Config Dashboard.Quit keys should not be empty")
	}

	// Verify reflection-based conversion
	km2 := DefaultKeyMap()

	// Remove original exact-case key to test case-insensitive matching
	delete(cfg.Dashboard, "Quit")

	// Change a key in config using mixed case
	cfg.Dashboard["qUiT"] = KeyBindingConfig{
		Keys: []string{"ctrl+x"},
		Help: "exit",
	}
	// Case-collision testing
	cfg.Dashboard["quit"] = KeyBindingConfig{
		Keys: []string{"ctrl+z"},
		Help: "exit alt",
	}

	km2.ApplyConfig(cfg)

	appliedKey := km2.Dashboard.Quit.Keys()[0]
	if appliedKey != "ctrl+x" && appliedKey != "ctrl+z" {
		t.Errorf("Expected Quit key to be ctrl+x or ctrl+z (from mixed-case configs), got %v", km2.Dashboard.Quit.Keys())
	}
}

func TestSaveAndLoadKeyMap(t *testing.T) {
	// Mock SurgeDir
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// We need to override GetSurgeDir or similar if it's used.
	// Since I can't easily override the function, I'll test the inner logic.

	km := DefaultKeyMap()
	cfg := km.ToConfig()
	cfg.Dashboard["Quit"] = KeyBindingConfig{
		Keys: []string{"q"},
		Help: "quit app",
	}

	path := filepath.Join(tmpDir, "keymap.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Test loading logic manually since LoadKeyMap uses a fixed path
	var loadedCfg KeyMapConfig
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = json.Unmarshal(data, &loadedCfg)
	if err != nil {
		t.Fatal(err)
	}

	kmLoaded := DefaultKeyMap()
	kmLoaded.ApplyConfig(&loadedCfg)

	if kmLoaded.Dashboard.Quit.Keys()[0] != "q" {
		t.Errorf("Expected loaded Quit key to be q, got %v", kmLoaded.Dashboard.Quit.Keys())
	}
}

func TestValidateKeyMap(t *testing.T) {
	km := &KeyMap{}
	km.Validate()

	defaults := DefaultKeyMap()
	if !reflect.DeepEqual(km.Dashboard, defaults.Dashboard) {
		t.Error("Validate should have filled Dashboard with defaults")
	}
}

func TestReportBugAndToggleHelpKeymaps(t *testing.T) {
	km := DefaultKeyMap()

	// 1. Dashboard.ToggleHelp
	toggleHelpKeys := km.Dashboard.ToggleHelp.Keys()
	if len(toggleHelpKeys) != 1 || toggleHelpKeys[0] != "h" {
		t.Errorf("Expected Dashboard.ToggleHelp default keys to be ['h'], got %v", toggleHelpKeys)
	}
	if km.Dashboard.ToggleHelp.Help().Key != "h" {
		t.Errorf("Expected Dashboard.ToggleHelp help key to be 'h', got %q", km.Dashboard.ToggleHelp.Help().Key)
	}

	// 2. Dashboard.ReportBug
	reportBugKeys := km.Dashboard.ReportBug.Keys()
	if len(reportBugKeys) != 1 || reportBugKeys[0] != "?" {
		t.Errorf("Expected Dashboard.ReportBug default keys to be ['?'], got %v", reportBugKeys)
	}
	if km.Dashboard.ReportBug.Help().Key != "?" {
		t.Errorf("Expected Dashboard.ReportBug help key to be '?', got %q", km.Dashboard.ReportBug.Help().Key)
	}
	if km.Dashboard.ReportBug.Help().Desc != "bug report" {
		t.Errorf("Expected Dashboard.ReportBug help desc to be 'bug report', got %q", km.Dashboard.ReportBug.Help().Desc)
	}

	// 3. Settings.ReportBug
	settingsReportBugKeys := km.Settings.ReportBug.Keys()
	if len(settingsReportBugKeys) != 1 || settingsReportBugKeys[0] != "?" {
		t.Errorf("Expected Settings.ReportBug default keys to be ['?'], got %v", settingsReportBugKeys)
	}
	if km.Settings.ReportBug.Help().Key != "?" {
		t.Errorf("Expected Settings.ReportBug help key to be '?', got %q", km.Settings.ReportBug.Help().Key)
	}
	if km.Settings.ReportBug.Help().Desc != "bug report" {
		t.Errorf("Expected Settings.ReportBug help desc to be 'bug report', got %q", km.Settings.ReportBug.Help().Desc)
	}
}
