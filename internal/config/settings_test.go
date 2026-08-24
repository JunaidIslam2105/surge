package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/utils"
)

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()

	if settings == nil {
		t.Fatal("DefaultSettings returned nil")
	}

	// Verify General settings
	t.Run("GeneralSettings", func(t *testing.T) {
		if Resolve[string](settings.General.DefaultDownloadDir) != "" {
			if info, err := os.Stat(Resolve[string](settings.General.DefaultDownloadDir)); err != nil || !info.IsDir() {
				t.Errorf("DefaultDownloadDir set to invalid path: %s", Resolve[string](settings.General.DefaultDownloadDir))
			}
		}

		if !Resolve[bool](settings.General.WarnOnDuplicate) {
			t.Error("WarnOnDuplicate should be true by default")
		}
		if Resolve[bool](settings.General.AllowRemoteOpenActions) {
			t.Error("AllowRemoteOpenActions should be false by default")
		}
		if Resolve[bool](settings.General.AutoResume) {
			t.Error("AutoResume should be false by default")
		}
	})

	// Verify Connection settings
	t.Run("NetworkSettings", func(t *testing.T) {
		if Resolve[int](settings.Network.MaxConnectionsPerDownload) <= 0 {
			t.Errorf("MaxConnectionsPerDownload should be positive, got: %d", Resolve[int](settings.Network.MaxConnectionsPerDownload))
		}
		if Resolve[int](settings.Network.MaxConnectionsPerDownload) > 64 {
			t.Errorf("MaxConnectionsPerDownload shouldn't exceed 64, got: %d", Resolve[int](settings.Network.MaxConnectionsPerDownload))
		}

		if Resolve[bool](settings.Network.SequentialDownload) {
			t.Error("SequentialDownload should be false by default")
		}
		if Resolve[int](settings.Network.DialHedgeCount) != 4 {
			t.Errorf("DialHedgeCount should be 4 by default, got: %d", Resolve[int](settings.Network.DialHedgeCount))
		}
	})

	// Verify Chunk settings
	t.Run("NetworkChunkSettings", func(t *testing.T) {
		if Resolve[int64](settings.Network.MinChunkSize) <= 0 {
			t.Errorf("MinChunkSize should be positive, got: %d", Resolve[int64](settings.Network.MinChunkSize))
		}

		if Resolve[int](settings.Network.WorkerBufferSize) <= 0 {
			t.Errorf("WorkerBufferSize should be positive, got: %d", Resolve[int](settings.Network.WorkerBufferSize))
		}
	})

	// Verify Performance settings
	t.Run("PerformanceSettings", func(t *testing.T) {
		if Resolve[int](settings.Performance.MaxTaskRetries) < 0 {
			t.Errorf("MaxTaskRetries should be non-negative, got: %d", Resolve[int](settings.Performance.MaxTaskRetries))
		}
		if Resolve[float64](settings.Performance.SlowWorkerThreshold) < 0 || Resolve[float64](settings.Performance.SlowWorkerThreshold) > 1 {
			t.Errorf("SlowWorkerThreshold should be between 0 and 1, got: %f", Resolve[float64](settings.Performance.SlowWorkerThreshold))
		}
		if Resolve[time.Duration](settings.Performance.SlowWorkerGracePeriod) <= 0 {
			t.Errorf("SlowWorkerGracePeriod should be positive, got: %v", Resolve[time.Duration](settings.Performance.SlowWorkerGracePeriod))
		}
		if Resolve[time.Duration](settings.Performance.StallTimeout) <= 0 {
			t.Errorf("StallTimeout should be positive, got: %v", Resolve[time.Duration](settings.Performance.StallTimeout))
		}
		if Resolve[float64](settings.Performance.SpeedEmaAlpha) < 0 || Resolve[float64](settings.Performance.SpeedEmaAlpha) > 1 {
			t.Errorf("SpeedEmaAlpha should be between 0 and 1, got: %f", Resolve[float64](settings.Performance.SpeedEmaAlpha))
		}
		if !Resolve[bool](settings.Performance.AdaptiveConcurrency) {
			t.Error("AdaptiveConcurrency should be true by default")
		}
		if got := Resolve[time.Duration](settings.Performance.AdaptiveConcurrencyRecoveryWindow); got != 3*time.Second {
			t.Errorf("AdaptiveConcurrencyRecoveryWindow = %v, want 3s", got)
		}
	})

	// Verify Extension settings
	t.Run("ExtensionSettings", func(t *testing.T) {
		if !Resolve[bool](settings.Extension.ExtensionPrompt) {
			t.Error("ExtensionPrompt should be true by default")
		}
		if Resolve[string](settings.Extension.ChromeExtensionURL) == "" {
			t.Error("ChromeExtensionURL should not be empty")
		}
		if Resolve[string](settings.Extension.FirefoxExtensionURL) == "" {
			t.Error("FirefoxExtensionURL should not be empty")
		}
		if Resolve[string](settings.Extension.InstructionsURL) == "" {
			t.Error("InstructionsURL should not be empty")
		}
	})
}

func TestDefaultSettings_Consistency(t *testing.T) {
	s1 := DefaultSettings()
	s2 := DefaultSettings()

	if s1 == s2 {
		t.Error("DefaultSettings should return new instance each time")
	}

	if Resolve[int](s1.Network.MaxConnectionsPerDownload) != Resolve[int](s2.Network.MaxConnectionsPerDownload) {
		t.Error("Default settings should be consistent")
	}
}

func TestDefaultSettings_Validation(t *testing.T) {
	settings := DefaultSettings()
	for _, cat := range settings.CategoriesList {
		for _, s := range cat.Settings {
			if s.ValidateFunc != nil {
				if err := s.ValidateFunc(s.DefaultValue); err != nil {
					t.Errorf("Validation failed for default value of %s: %v", s.Key, err)
				}
				if err := s.ValidateFunc(s.Value); err != nil {
					t.Errorf("Validation failed for current value of %s: %v", s.Key, err)
				}
			}
		}
	}
}

// TestEveryValidatorIsInCategoriesList prevents a setting from being defined with a
// ValidateFunc but accidentally omitted from initializeCategoriesList, which would
// silently skip validation on startup.
func TestEveryValidatorIsInCategoriesList(t *testing.T) {
	settings := DefaultSettings()

	var allSettings []*Setting
	var collect func(v reflect.Value)
	collect = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			if v.Type() == reflect.TypeOf((*Setting)(nil)) {
				allSettings = append(allSettings, v.Interface().(*Setting))
				return
			}
			collect(v.Elem())
		case reflect.Struct:
			for i := range v.NumField() {
				collect(v.Field(i))
			}
		case reflect.Slice:
			for i := range v.Len() {
				collect(v.Index(i))
			}
		}
	}
	collect(reflect.ValueOf(settings))

	validated := make(map[*Setting]struct{})
	for _, s := range allSettings {
		if s != nil && s.ValidateFunc != nil {
			validated[s] = struct{}{}
		}
	}

	inCategories := make(map[*Setting]struct{})
	for _, cat := range settings.CategoriesList {
		for _, s := range cat.Settings {
			if s != nil {
				inCategories[s] = struct{}{}
			}
		}
	}

	for s := range validated {
		if _, ok := inCategories[s]; !ok {
			t.Errorf("Setting %q has a ValidateFunc but is missing from CategoriesList", s.Key)
		}
	}
}

func TestCategoriesListMatchesCategoryOrder(t *testing.T) {
	settings := DefaultSettings()
	order := CategoryOrder()

	// 1. Verify every category in CategoriesList is present in CategoryOrder
	orderMap := make(map[string]bool)
	for _, name := range order {
		orderMap[name] = true
	}
	for _, cat := range settings.CategoriesList {
		if !orderMap[cat.Name] {
			t.Errorf("Category %q is in CategoriesList but missing from CategoryOrder()", cat.Name)
		}
	}

	// 2. Verify every category in CategoryOrder is present in CategoriesList
	listMap := make(map[string]bool)
	for _, cat := range settings.CategoriesList {
		listMap[cat.Name] = true
	}
	for _, name := range order {
		if !listMap[name] {
			t.Errorf("Category %q is in CategoryOrder() but missing from CategoriesList", name)
		}
	}
}

func TestGetSettingsPath(t *testing.T) {
	path := GetSettingsPath()

	if path == "" {
		t.Error("GetSettingsPath returned empty string")
	}

	surgeDir := GetSurgeDir()
	if !strings.HasPrefix(path, surgeDir) {
		t.Errorf("Settings path should be under surge dir. Path: %s, SurgeDir: %s", path, surgeDir)
	}

	if !strings.HasSuffix(path, "settings.toml") {
		t.Errorf("Settings path should end with 'settings.toml', got: %s", path)
	}
}

func TestSaveAndLoadSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-settings-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	original := DefaultSettings()
	original.General.DefaultDownloadDir.Value = tmpDir
	original.General.WarnOnDuplicate.Value = false
	original.General.AutoResume.Value = true
	original.Network.MaxConnectionsPerDownload.Value = 16
	original.Network.MaxConcurrentDownloads.Value = 7
	original.Network.UserAgent.Value = "TestAgent/1.0"
	original.Network.MinChunkSize.Value = int64(1 * utils.MiB)
	original.Network.WorkerBufferSize.Value = 256 * utils.KiB
	original.Network.DialHedgeCount.Value = 6
	original.Performance.MaxTaskRetries.Value = 5
	original.Performance.SlowWorkerThreshold.Value = 0.5
	original.Performance.SlowWorkerGracePeriod.Value = 10 * time.Second
	original.Performance.StallTimeout.Value = 5 * time.Second
	original.Performance.SpeedEmaAlpha.Value = 0.5

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal settings: %v", err)
	}

	testPath := filepath.Join(tmpDir, "test_settings.json")
	if err := os.WriteFile(testPath, data, 0o644); err != nil {
		t.Fatalf("Failed to write settings file: %v", err)
	}

	readData, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("Failed to read settings file: %v", err)
	}

	loaded := DefaultSettings()
	if err := json.Unmarshal(readData, loaded); err != nil {
		t.Fatalf("Failed to unmarshal settings: %v", err)
	}

	if Resolve[string](loaded.General.DefaultDownloadDir) != Resolve[string](original.General.DefaultDownloadDir) {
		t.Errorf("DefaultDownloadDir mismatch: got %q, want %q", Resolve[string](loaded.General.DefaultDownloadDir), Resolve[string](original.General.DefaultDownloadDir))
	}
	if Resolve[bool](loaded.General.WarnOnDuplicate) != Resolve[bool](original.General.WarnOnDuplicate) {
		t.Error("WarnOnDuplicate mismatch")
	}
	if Resolve[int](loaded.Network.MaxConcurrentDownloads) != Resolve[int](original.Network.MaxConcurrentDownloads) {
		t.Error("MaxConcurrentDownloads mismatch")
	}
}

func TestLoadSettings_MissingFile(t *testing.T) {
	settings, err := LoadSettings()
	if err != nil {
		t.Logf("LoadSettings returned error (may be expected): %v", err)
	}

	if settings != nil {
		if Resolve[int](settings.Network.MaxConnectionsPerDownload) <= 0 {
			t.Error("Should return default settings with valid values")
		}
	}
}

func TestLoadSettings_CorruptedJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-corrupt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testPath := filepath.Join(tmpDir, "corrupt.json")
	if err := os.WriteFile(testPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	data, _ := os.ReadFile(testPath)
	settings := DefaultSettings()
	err = json.Unmarshal(data, settings)

	if err == nil {
		t.Error("Expected error when unmarshaling invalid JSON")
	}
}

func TestToRuntimeConfig(t *testing.T) {
	settings := DefaultSettings()
	settings.Performance.AdaptiveConcurrency.Value = false
	settings.Performance.AdaptiveConcurrencyRecoveryWindow.Value = 5 * time.Second
	runtime := settings.ToRuntimeConfig()

	if runtime == nil {
		t.Fatal("ToRuntimeConfig returned nil")
	}

	if runtime.MaxConnectionsPerDownload != Resolve[int](settings.Network.MaxConnectionsPerDownload) {
		t.Error("MaxConnectionsPerDownload not correctly mapped")
	}
	if runtime.IsAdaptiveConcurrencyEnabled() {
		t.Error("AdaptiveConcurrency not correctly mapped")
	}
	if runtime.GetAdaptiveConcurrencyRecoveryWindow() != 5*time.Second {
		t.Error("AdaptiveConcurrencyRecoveryWindow not correctly mapped")
	}
}

func TestGetSettingsMetadata(t *testing.T) {
	metadata := GetSettingsMetadata()

	if metadata == nil {
		t.Fatal("GetSettingsMetadata returned nil")
	}

	expectedCategories := CategoryOrder()
	for _, cat := range expectedCategories {
		if _, ok := metadata[cat]; !ok {
			t.Errorf("Missing metadata for category: %s", cat)
		}
	}
}

func TestCategoryOrder(t *testing.T) {
	order := CategoryOrder()

	if len(order) == 0 {
		t.Error("CategoryOrder returned empty slice")
	}
}

func TestSettingsJSON_Serialization(t *testing.T) {
	original := DefaultSettings()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Start with DefaultSettings() to ensure the struct schema is fully pre-populated
	loaded := DefaultSettings()
	if err := json.Unmarshal(data, loaded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if Resolve[int](loaded.Network.MaxConnectionsPerDownload) != Resolve[int](original.Network.MaxConnectionsPerDownload) {
		t.Error("Round-trip failed for MaxConnectionsPerDownload")
	}
}

func TestSaveSettings_RealFunction(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	original := DefaultSettings()
	original.Network.MaxConnectionsPerDownload.Value = 48
	original.General.AutoResume.Value = true

	err := SaveSettings(original)
	if err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}

	loaded, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}

	if Resolve[int](loaded.Network.MaxConnectionsPerDownload) != 48 {
		t.Errorf("MaxConnectionsPerDownload mismatch: got %d, want 48", Resolve[int](loaded.Network.MaxConnectionsPerDownload))
	}
	if !Resolve[bool](loaded.General.AutoResume) {
		t.Error("AutoResume should be true")
	}
}

func TestSettings_Validate(t *testing.T) {
	defaults := DefaultSettings()

	tests := []struct {
		name     string
		modify   func(*Settings)
		validate func(*testing.T, *Settings)
	}{
		{
			name: "Valid Settings Unchanged",
			modify: func(s *Settings) {
				s.Network.MaxConnectionsPerDownload.Value = 48
				s.General.LogRetentionCount.Value = 10
				s.Performance.SlowWorkerThreshold.Value = 0.5
			},
			validate: func(t *testing.T, s *Settings) {
				if Resolve[int](s.Network.MaxConnectionsPerDownload) != 48 {
					t.Errorf("Expected 48, got %d", Resolve[int](s.Network.MaxConnectionsPerDownload))
				}
				if Resolve[int](s.General.LogRetentionCount) != 10 {
					t.Errorf("Expected 10, got %d", Resolve[int](s.General.LogRetentionCount))
				}
				if Resolve[float64](s.Performance.SlowWorkerThreshold) != 0.5 {
					t.Errorf("Expected 0.5, got %f", Resolve[float64](s.Performance.SlowWorkerThreshold))
				}
			},
		},
		{
			name: "Invalid Connections High Reset",
			modify: func(s *Settings) {
				s.Network.MaxConnectionsPerDownload.Value = 999
			},
			validate: func(t *testing.T, s *Settings) {
				if Resolve[int](s.Network.MaxConnectionsPerDownload) != Resolve[int](defaults.Network.MaxConnectionsPerDownload) {
					t.Errorf("Expected default %d, got %d", Resolve[int](defaults.Network.MaxConnectionsPerDownload), Resolve[int](s.Network.MaxConnectionsPerDownload))
				}
			},
		},
		{
			name: "Invalid Connections Low Reset",
			modify: func(s *Settings) {
				s.Network.MaxConnectionsPerDownload.Value = 0
			},
			validate: func(t *testing.T, s *Settings) {
				if Resolve[int](s.Network.MaxConnectionsPerDownload) != Resolve[int](defaults.Network.MaxConnectionsPerDownload) {
					t.Errorf("Expected default %d, got %d", Resolve[int](defaults.Network.MaxConnectionsPerDownload), Resolve[int](s.Network.MaxConnectionsPerDownload))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			tt.modify(s)
			s.Validate()
			tt.validate(t, s)
		})
	}
}

func TestResolveGeneric(t *testing.T) {
	s := DefaultSettings()

	// 1. Verify int
	s.Network.MaxConcurrentDownloads.Value = float64(8)
	if val := Resolve[int](s.Network.MaxConcurrentDownloads); val != 8 {
		t.Errorf("Resolve[int] got %d, want 8", val)
	}

	// 2. Verify bool
	s.General.WarnOnDuplicate.Value = float64(1)
	if val := Resolve[bool](s.General.WarnOnDuplicate); !val {
		t.Errorf("Resolve[bool] got false, want true")
	}

	// 3. Verify string
	s.Network.UserAgent.Value = "Surge/1.0"
	if val := Resolve[string](s.Network.UserAgent); val != "Surge/1.0" {
		t.Errorf("Resolve[string] got %q, want \"Surge/1.0\"", val)
	}

	// 4. Verify duration
	s.Performance.SlowWorkerGracePeriod.Value = "15s"
	if val := Resolve[time.Duration](s.Performance.SlowWorkerGracePeriod); val != 15*time.Second {
		t.Errorf("Resolve[time.Duration] got %v, want 15s", val)
	}
}

func TestCategorySettings_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantErr     bool
		assertState func(t *testing.T, cs *CategorySettings)
	}{
		{
			name:    "Old array format",
			json:    `[{"name": "Videos", "pattern": ".*\\.mp4"}]`,
			wantErr: false,
			assertState: func(t *testing.T, cs *CategorySettings) {
				if len(cs.Categories) != 1 {
					t.Fatalf("Expected 1 category, got %d", len(cs.Categories))
				}
				if cs.Categories[0].Name != "Videos" {
					t.Errorf("Expected 'Videos', got '%s'", cs.Categories[0].Name)
				}
			},
		},
		{
			name:    "New struct format",
			json:    `{"category_enabled": true, "categories": [{"name": "Documents", "pattern": ".*\\.pdf"}]}`,
			wantErr: false,
			assertState: func(t *testing.T, cs *CategorySettings) {
				if len(cs.Categories) != 1 {
					t.Fatalf("Expected 1 category, got %d", len(cs.Categories))
				}
				if cs.Categories[0].Name != "Documents" {
					t.Errorf("Expected 'Documents', got '%s'", cs.Categories[0].Name)
				}
				if cs.CategoryEnabled == nil {
					t.Fatalf("Expected CategoryEnabled to be non-nil")
				}
				if Resolve[bool](cs.CategoryEnabled) != true {
					t.Errorf("Expected CategoryEnabled to be true")
				}
			},
		},
		{
			name:    "Null value",
			json:    `null`,
			wantErr: false,
			assertState: func(t *testing.T, cs *CategorySettings) {
				if len(cs.Categories) != 0 {
					t.Errorf("Expected 0 categories, got %d", len(cs.Categories))
				}
			},
		},
		{
			name:    "Empty array",
			json:    `[]`,
			wantErr: false,
			assertState: func(t *testing.T, cs *CategorySettings) {
				if len(cs.Categories) != 0 {
					t.Errorf("Expected 0 categories, got %d", len(cs.Categories))
				}
			},
		},
		{
			name:        "Invalid JSON array",
			json:        `[{"name": "Videos"`,
			wantErr:     true,
			assertState: nil,
		},
		{
			name:        "Invalid JSON object",
			json:        `{"categories": [{"name": "Videos"}`,
			wantErr:     true,
			assertState: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cs CategorySettings
			err := json.Unmarshal([]byte(tt.json), &cs)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.assertState != nil {
				tt.assertState(t, &cs)
			}
		})
	}
}

func TestCategorySettings_UnmarshalJSON_BackwardCompatibility(t *testing.T) {
	// Test the old array format
	oldJSON := []byte(`[
		{"name": "Videos", "pattern": ".*\\.mp4"},
		{"name": "Music", "pattern": ".*\\.mp3"}
	]`)

	var cs CategorySettings
	err := json.Unmarshal(oldJSON, &cs)
	if err != nil {
		t.Fatalf("Failed to unmarshal old array format: %v", err)
	}

	if len(cs.Categories) != 2 {
		t.Fatalf("Expected 2 categories, got %d", len(cs.Categories))
	}
	if cs.Categories[0].Name != "Videos" {
		t.Errorf("Expected first category to be 'Videos', got '%s'", cs.Categories[0].Name)
	}

	// Test the new struct format
	newJSON := []byte(`{
		"category_enabled": true,
		"categories": [
			{"name": "Documents", "pattern": ".*\\.pdf"}
		]
	}`)

	fullJSON := []byte(`{
		"categories": ` + string(newJSON) + `
	}`)

	s := DefaultSettings()
	err = json.Unmarshal(fullJSON, s)
	if err != nil {
		t.Fatalf("Failed to unmarshal new struct format: %v", err)
	}

	if len(s.Categories.Categories) != 1 {
		t.Fatalf("Expected 1 category, got %d", len(s.Categories.Categories))
	}
	if s.Categories.Categories[0].Name != "Documents" {
		t.Errorf("Expected 'Documents', got '%s'", s.Categories.Categories[0].Name)
	}
	if !Resolve[bool](s.Categories.CategoryEnabled) {
		t.Errorf("Expected category_enabled to be true")
	}

	// Test full settings with old format
	fullOldJSON := []byte(`{
		"categories": ` + string(oldJSON) + `
	}`)

	s2 := DefaultSettings()
	err = json.Unmarshal(fullOldJSON, s2)
	if err != nil {
		t.Fatalf("Failed to unmarshal full old format: %v", err)
	}

	if len(s2.Categories.Categories) != 2 {
		t.Fatalf("Expected 2 categories, got %d", len(s2.Categories.Categories))
	}
	if s2.Categories.Categories[0].Name != "Videos" {
		t.Errorf("Expected 'Videos', got '%s'", s2.Categories.Categories[0].Name)
	}
	// CategoryEnabled should remain default (false)
	if Resolve[bool](s2.Categories.CategoryEnabled) {
		t.Errorf("Expected category_enabled to remain false")
	}
}
