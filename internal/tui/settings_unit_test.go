package tui

import (
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
)

func TestSettingsMetadataValidation(t *testing.T) {
	metadata := config.GetSettingsMetadata()
	categories := config.CategoryOrder()

	if len(metadata) == 0 {
		t.Fatal("Expected non-empty settings metadata")
	}

	for _, category := range categories {
		settings, ok := metadata[category]
		if !ok {
			t.Errorf("Category %s missing from metadata", category)
			continue
		}

		if len(settings) == 0 {
			t.Errorf("Category %s has no settings", category)
		}

		for _, s := range settings {
			if s.Key == "" {
				t.Errorf("Setting in category %s has empty Key", category)
			}
			if s.Label == "" {
				t.Errorf("Setting %q in category %s has empty Label", s.Key, category)
			}
			if s.Description == "" {
				t.Errorf("Setting %q in category %s has empty Description", s.Key, category)
			}
			if s.Type == "" {
				t.Errorf("Setting %q in category %s has empty Type", s.Key, category)
			}
		}
	}
}

func TestSettingsFloatResilience(t *testing.T) {
	// Verify that float64 values (e.g. from JSON deserialization) format cleanly
	valInt := formatSettingValue(float64(5), "int", false)
	if valInt != "5" {
		t.Errorf("Expected float64(5) as int to format as \"5\", got %q", valInt)
	}

	valDuration := formatSettingValue(float64(5*time.Second), "duration", false)
	if valDuration != "5s" {
		t.Errorf("Expected float64(5s) as duration to format as \"5s\", got %q", valDuration)
	}
}

func TestDurationSettingsDeclareAnEditorUnit(t *testing.T) {
	settings := config.DefaultSettings()
	metadata := config.GetSettingsMetadata()

	for category, settingMetadata := range metadata {
		for _, meta := range settingMetadata {
			if meta.Type != config.TypeDuration {
				continue
			}

			setting := settings.FindSetting(category, meta.Key)
			if setting == nil {
				t.Fatalf("duration setting %q is missing from category %q", meta.Key, category)
			}
			var want string
			switch meta.Unit {
			case config.UnitSeconds:
				setting.Value, want = 7*time.Second, "7"
			case config.UnitMinutes:
				setting.Value, want = 2*time.Minute, "2"
			default:
				t.Errorf("duration setting %q has unsupported editor unit %q", meta.Key, meta.Unit)
				continue
			}
			if got := settingUnitSuffix(meta.Unit); got == "" {
				t.Errorf("duration setting %q has no display suffix", meta.Key)
			}

			if got := formatSettingValueForEdit(setting.Value, meta.Type, meta.Unit, meta.Key, false); got != want {
				t.Errorf("duration setting %q edit value = %q, want %q", meta.Key, got, want)
			}
		}
	}
}

func TestDurationSettingsCanUseMinutesInTheEditor(t *testing.T) {
	m := &RootModel{Settings: config.DefaultSettings()}
	setting := m.Settings.Performance.SlowWorkerGracePeriod
	setting.Unit = config.UnitMinutes

	if got := formatSettingValueForEdit(2*time.Minute, setting.Type, setting.Unit, setting.Key, false); got != "2" {
		t.Fatalf("minute duration edit value = %q, want %q", got, "2")
	}
	if err := m.setSettingValue("Performance", setting.Key, "2"); err != nil {
		t.Fatalf("setSettingValue failed: %v", err)
	}
	if got := config.Resolve[time.Duration](setting); got != 2*time.Minute {
		t.Errorf("minute duration = %v, want %v", got, 2*time.Minute)
	}
}

func TestSetSettingValueConversions(t *testing.T) {
	m := &RootModel{
		Settings: config.DefaultSettings(),
	}

	// 1. Test worker_buffer_size (float -> KB-scaled int)
	// Default is 32KB. Let's set it to 64 (representing 64KB, which should become 64 * 1024 = 65536)
	err := m.setSettingValue("Network", "worker_buffer_size", "64")
	if err != nil {
		t.Fatalf("setSettingValue failed: %v", err)
	}
	val := config.Resolve[int](m.Settings.Network.WorkerBufferSize)
	if val != 64*1024 {
		t.Errorf("Expected worker_buffer_size to be %d, got %d", 64*1024, val)
	}

	// 2. Test min_chunk_size (float -> MB-scaled int64)
	// Default is 4MB. Let's set it to 8 (representing 8MB, which should become 8 * 1024 * 1024 = 8388608)
	err = m.setSettingValue("Network", "min_chunk_size", "8")
	if err != nil {
		t.Fatalf("setSettingValue failed: %v", err)
	}
	val64 := config.Resolve[int64](m.Settings.Network.MinChunkSize)
	if val64 != 8*1024*1024 {
		t.Errorf("Expected min_chunk_size to be %d, got %d", 8*1024*1024, val64)
	}

	// 3. Test slow_worker_grace_period / stall_timeout (number string -> time.Duration via "s" suffix injection)
	// Let's set stall_timeout to "15" (which should parse as 15s)
	err = m.setSettingValue("Performance", "stall_timeout", "15")
	if err != nil {
		t.Fatalf("setSettingValue failed: %v", err)
	}
	dur := config.Resolve[time.Duration](m.Settings.Performance.StallTimeout)
	if dur != 15*time.Second {
		t.Errorf("Expected stall_timeout to be %v, got %v", 15*time.Second, dur)
	}

	// Let's set slow_worker_grace_period to "45s" (already has "s" suffix)
	err = m.setSettingValue("Performance", "slow_worker_grace_period", "45s")
	if err != nil {
		t.Fatalf("setSettingValue failed: %v", err)
	}
	dur = config.Resolve[time.Duration](m.Settings.Performance.SlowWorkerGracePeriod)
	if dur != 45*time.Second {
		t.Errorf("Expected slow_worker_grace_period to be %v, got %v", 45*time.Second, dur)
	}
}

func TestSetSpeedLimitValue_AllowsInheritedDownloadRateLimit(t *testing.T) {
	// Initialize a RootModel with non-nil maps and a dummy Service to avoid panics
	m := &RootModel{
		Settings:  config.DefaultSettings(),
		downloads: []*DownloadModel{{ID: "test-id"}},
	}

	for _, value := range []string{"inherit", "default"} {
		// Expect nil error because "inherit" or "default" is a valid rate limit for a specific download
		if err := m.setSpeedLimitValue("dl:test-id", value); err != nil {
			t.Fatalf("setSpeedLimitValue rejected %q for dl: key: %v", value, err)
		}
	}
}
