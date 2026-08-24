package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"strings"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
	"github.com/pelletier/go-toml/v2"
)

var ErrCategoryExists = errors.New("category already exists")

type Settings struct {
	General     GeneralSettings     `json:"general"`
	Network     NetworkSettings     `json:"network"`
	Performance PerformanceSettings `json:"performance"`
	Categories  CategorySettings    `json:"categories"`
	Extension   ExtensionSettings   `json:"extension"`

	// Schema-driven categories list populated on initialization
	CategoriesList []*SettingsCategory `json:"-"`

	StartupWarnings []string `json:"-"`
}

type GeneralSettings struct {
	DefaultDownloadDir           *Setting `json:"default_download_dir"`
	WarnOnDuplicate              *Setting `json:"warn_on_duplicate"`
	DownloadCompleteNotification *Setting `json:"download_complete_notification"`
	AutoShutdownAfterDownloads   *Setting `json:"auto_shutdown_after_downloads"`
	AllowRemoteOpenActions       *Setting `json:"allow_remote_open_actions"`
	AutoResume                   *Setting `json:"auto_resume"`
	AutoStart                    *Setting `json:"auto_start"`
	SkipUpdateCheck              *Setting `json:"skip_update_check"`
	ClipboardMonitor             *Setting `json:"clipboard_monitor"`
	Theme                        *Setting `json:"theme"`
	ThemePath                    *Setting `json:"theme_path"`
	LogRetentionCount            *Setting `json:"log_retention_count"`
	LiveSpeedGraph               *Setting `json:"live_speed_graph"`
	ShowSpeedGraph               *Setting `json:"show_speed_graph"`
}

type NetworkSettings struct {
	MaxConnectionsPerDownload *Setting `json:"max_connections_per_host"`
	MaxConcurrentDownloads    *Setting `json:"max_concurrent_downloads"`
	MaxConcurrentProbes       *Setting `json:"max_concurrent_probes"`
	UserAgent                 *Setting `json:"user_agent"`
	ProxyURL                  *Setting `json:"proxy_url"`
	CustomDNS                 *Setting `json:"custom_dns"`
	SequentialDownload        *Setting `json:"sequential_download"`
	MinChunkSize              *Setting `json:"min_chunk_size"`
	WorkerBufferSize          *Setting `json:"worker_buffer_size"`
	DialHedgeCount            *Setting `json:"dial_hedge_count"`
	GlobalRateLimit           *Setting `json:"global_rate_limit"`
	DefaultDownloadRateLimit  *Setting `json:"default_download_rate_limit"`
}

type PerformanceSettings struct {
	MaxTaskRetries                    *Setting `json:"max_task_retries"`
	SlowWorkerThreshold               *Setting `json:"slow_worker_threshold"`
	SlowWorkerGracePeriod             *Setting `json:"slow_worker_grace_period"`
	StallTimeout                      *Setting `json:"stall_timeout"`
	SpeedEmaAlpha                     *Setting `json:"speed_ema_alpha"`
	AdaptiveConcurrency               *Setting `json:"adaptive_concurrency"`
	AdaptiveConcurrencyRecoveryWindow *Setting `json:"adaptive_concurrency_recovery_window"`
}

type CategorySettings struct {
	CategoryEnabled *Setting   `json:"category_enabled"`
	Categories      []Category `json:"categories"`
}

// UnmarshalJSON handles both the new struct format and the old array format for Categories
// to prevent config wipes when upgrading from older versions.
func (c *CategorySettings) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		// Old format: direct array of categories
		var oldCats []Category
		if err := json.Unmarshal(data, &oldCats); err != nil {
			return err
		}
		c.Categories = oldCats
		return nil
	}

	// New format: CategorySettings object
	type alias CategorySettings
	aux := (*alias)(c)
	return json.Unmarshal(data, aux)
}

type ExtensionSettings struct {
	ExtensionPrompt     *Setting `json:"extension_prompt"`
	ChromeExtensionURL  *Setting `json:"chrome_extension_url"`
	FirefoxExtensionURL *Setting `json:"firefox_extension_url"`
	AuthToken           *Setting `json:"auth_token"`
	InstructionsURL     *Setting `json:"instructions_url"`
}

// UnmarshalJSON updates only the Value field of the initialized pointer.
func (s *Setting) UnmarshalJSON(data []byte) error {
	var val any
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	s.Value = val
	return nil
}

// MarshalJSON serializes only the primitive value of this setting.
func (s *Setting) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Value)
}

// Resolve retrieves the value of a setting converted to the expected generic type T.
// This is a unified, caller-agnostic function that handles all dynamic type conversions safely.
func Resolve[T any](s *Setting) T {
	var zero T
	if s == nil {
		return zero
	}

	var anyVal = s.Value
	if anyVal == nil {
		anyVal = s.DefaultValue
		if anyVal == nil {
			return zero
		}
	}

	// Try direct type assertion first
	if val, ok := anyVal.(T); ok {
		return val
	}

	// Dynamic conversions based on requested generic type T
	switch any(zero).(type) {
	case bool:
		var b bool
		switch v := anyVal.(type) {
		case bool:
			b = v
		case int:
			b = v != 0
		case int64:
			b = v != 0
		case float64:
			b = v != 0
		}
		return any(b).(T)

	case int:
		var i int
		switch v := anyVal.(type) {
		case int:
			i = v
		case int64:
			i = int(v)
		case float64:
			i = int(v)
		}
		return any(i).(T)

	case int64:
		var i int64
		switch v := anyVal.(type) {
		case int64:
			i = v
		case int:
			i = int64(v)
		case float64:
			i = int64(v)
		}
		return any(i).(T)

	case float64:
		var f float64
		switch v := anyVal.(type) {
		case float64:
			f = v
		case float32:
			f = float64(v)
		case int:
			f = float64(v)
		case int64:
			f = float64(v)
		}
		return any(f).(T)

	case string:
		if str, ok := anyVal.(string); ok {
			return any(str).(T)
		}

	case time.Duration:
		var d time.Duration
		switch v := anyVal.(type) {
		case time.Duration:
			d = v
		case string:
			if parsed, err := time.ParseDuration(v); err == nil {
				d = parsed
			}
		case float64:
			d = time.Duration(v)
		case int64:
			d = time.Duration(v)
		case int:
			d = time.Duration(v)
		}
		return any(d).(T)
	}

	return zero
}

// Resolve returns the setting's value dynamically converted to its schema-defined target type.
// This ensures that unmarshaled types (like float64) are resolved back to their correct Go types (int, duration, etc.)
// and can be accessed safely as any.
func (s *Setting) Resolve() any {
	if s == nil {
		return nil
	}
	switch s.Type {
	case TypeBool:
		return Resolve[bool](s)
	case TypeInt:
		return Resolve[int](s)
	case TypeInt64:
		return Resolve[int64](s)
	case TypeFloat64:
		return Resolve[float64](s)
	case TypeString, TypeAuthToken, TypeLink:
		return Resolve[string](s)
	case TypeDuration:
		return Resolve[time.Duration](s)
	}
	return s.Value
}

func (s *Settings) initializeCategoriesList() {
	s.CategoriesList = []*SettingsCategory{
		{
			Name: "General",
			Settings: []*Setting{
				s.General.DefaultDownloadDir,
				s.General.WarnOnDuplicate,
				s.General.DownloadCompleteNotification,
				s.General.AutoShutdownAfterDownloads,
				s.General.AllowRemoteOpenActions,
				s.General.AutoResume,
				s.General.AutoStart,
				s.General.SkipUpdateCheck,
				s.General.ClipboardMonitor,
				s.General.Theme,
				s.General.ThemePath,
				s.General.LogRetentionCount,
				s.General.LiveSpeedGraph,
				s.General.ShowSpeedGraph,
			},
		},
		{
			Name: "Network",
			Settings: []*Setting{
				s.Network.MaxConnectionsPerDownload,
				s.Network.MaxConcurrentDownloads,
				s.Network.MaxConcurrentProbes,
				s.Network.UserAgent,
				s.Network.ProxyURL,
				s.Network.CustomDNS,
				s.Network.SequentialDownload,
				s.Network.MinChunkSize,
				s.Network.WorkerBufferSize,
				s.Network.DialHedgeCount,
				s.Network.GlobalRateLimit,
				s.Network.DefaultDownloadRateLimit,
			},
		},

		{
			Name: "Performance",
			Settings: []*Setting{
				s.Performance.MaxTaskRetries,
				s.Performance.SlowWorkerThreshold,
				s.Performance.SlowWorkerGracePeriod,
				s.Performance.StallTimeout,
				s.Performance.SpeedEmaAlpha,
				s.Performance.AdaptiveConcurrency,
				s.Performance.AdaptiveConcurrencyRecoveryWindow,
			},
		},
		{
			Name: "Categories",
			Settings: []*Setting{
				s.Categories.CategoryEnabled,
			},
		},
		{
			Name: "Extension",
			Settings: []*Setting{
				s.Extension.ExtensionPrompt,
				s.Extension.ChromeExtensionURL,
				s.Extension.FirefoxExtensionURL,
				s.Extension.AuthToken,
				s.Extension.InstructionsURL,
			},
		},
	}
}

// FindSetting returns a setting by its category and JSON key without using reflection.
func (s *Settings) FindSetting(categoryName, key string) *Setting {
	for _, cat := range s.CategoriesList {
		if cat.Name == categoryName {
			for _, set := range cat.Settings {
				if set.Key == key {
					return set
				}
			}
			return nil
		}
	}
	return nil
}

func (s *Settings) FindSettingsCategory(name string) *SettingsCategory {
	for _, cat := range s.CategoriesList {
		if cat.Name == name {
			return cat
		}
	}
	return nil
}

// GetSettingsPath returns the path to the settings TOML file.
func GetSettingsPath() string {
	return filepath.Join(GetSurgeDir(), "settings.toml")
}

// LoadSettings loads settings from disk. Returns defaults if file doesn't exist
// or if the file is corrupt, so the application can always start.
func LoadSettings() (*Settings, error) {
	path := GetSettingsPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			settings := DefaultSettings()
			// Check if old settings.json exists
			jsonPath := filepath.Join(GetSurgeDir(), "settings.json")
			if _, statErr := os.Stat(jsonPath); statErr == nil {
				settings.StartupWarnings = append(settings.StartupWarnings,
					"Config: 'settings.json' is no longer supported. Settings have been reset to defaults. Please re-configure your options via 'surge config' or by editing 'settings.toml'.")
			}
			return settings, nil
		}
		return nil, err
	}

	settings := DefaultSettings() // Start with defaults to fill any missing fields

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		utils.Debug("Warning: corrupt settings file %s: %v - using defaults", path, err)
		settings.StartupWarnings = append(settings.StartupWarnings,
			fmt.Sprintf("Config: settings file is corrupt (%v) - all settings reset to defaults", err))
		return settings, nil
	}

	// Assign mapped values to settings
	for catName, catRaw := range raw {
		if strings.EqualFold(catName, "categories") {
			if catMap, ok := catRaw.(map[string]any); ok {
				if enabled, ok := catMap["category_enabled"]; ok {
					if set := settings.FindSetting("Categories", "category_enabled"); set != nil {
						set.Value = enabled
					}
				}
				if catsRaw, ok := catMap["categories"]; ok {
					if catsList, ok := catsRaw.([]any); ok {
						var parsedCats []Category
						for _, cAny := range catsList {
							if cMap, ok := cAny.(map[string]any); ok {
								parsed := Category{}
								if n, ok := cMap["name"].(string); ok {
									parsed.Name = n
								}
								if d, ok := cMap["description"].(string); ok {
									parsed.Description = d
								}
								if pt, ok := cMap["pattern"].(string); ok {
									parsed.Pattern = pt
								}
								if p, ok := cMap["path"].(string); ok {
									parsed.Path = p
								}
								parsedCats = append(parsedCats, parsed)
							}
						}
						settings.Categories.Categories = parsedCats
					}
				}
			}
			continue
		}

		if catMap, ok := catRaw.(map[string]any); ok {
			for _, cat := range settings.CategoriesList {
				if strings.EqualFold(cat.Name, catName) {
					for key, val := range catMap {
						if set := settings.FindSetting(cat.Name, key); set != nil {
							set.Value = val
						}
					}
					break
				}
			}
		}
	}

	// Normalize loaded values to their proper types (handles TOML type coercion
	// such as durations stored as strings, ints stored as int64, etc.)
	for _, cat := range settings.CategoriesList {
		for _, set := range cat.Settings {
			set.Value = set.Resolve()
		}
	}

	// Validate settings and roll back individual invalid fields to defaults
	settings.Validate()

	return settings, nil
}

// SettingMeta provides metadata for a single setting (for UI rendering).
type SettingMeta struct {
	Key             string      // JSON key name
	Label           string      // Human-readable label
	Description     string      // Help text displayed in right pane
	Type            SettingType // "string", "int", "int64", "bool", "duration", "float64", "auth_token", "link"
	RequiresRestart bool        // Whether changing this setting requires an application restart
}

var (
	cachedSettingsMetadata map[string][]SettingMeta
	settingsMetadataOnce   sync.Once
)

// GetSettingsMetadata returns metadata for all settings organized by category.
// The result is computed once and cached since metadata is static.
func GetSettingsMetadata() map[string][]SettingMeta {
	settingsMetadataOnce.Do(func() {
		s := DefaultSettings()
		cachedSettingsMetadata = make(map[string][]SettingMeta)
		for _, cat := range s.CategoriesList {
			list := make([]SettingMeta, 0, len(cat.Settings))
			for _, set := range cat.Settings {
				list = append(list, SettingMeta{
					Key:             set.Key,
					Label:           set.Label,
					Description:     set.Description,
					Type:            set.Type,
					RequiresRestart: set.NeedsRestart,
				})
			}
			cachedSettingsMetadata[cat.Name] = list
		}
	})
	return cachedSettingsMetadata
}

// CategoryOrder returns the display order of settings categories.
func CategoryOrder() []string {
	return []string{
		"General",
		"Network",
		"Performance",
		"Categories",
		"Extension",
	}
}

const (
	ThemeAdaptive = 0
	ThemeLight    = 1
	ThemeDark     = 2
)

func parseAnyInt(val any) (int, error) {
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid type: %T", val)
	}
}

// DefaultSettings returns a new Settings instance with sensible defaults.
func DefaultSettings() *Settings {
	defaultDir := GetDownloadsDir()

	s := &Settings{
		General: GeneralSettings{
			DefaultDownloadDir: &Setting{
				Key:          "default_download_dir",
				Label:        "Default Download Dir",
				Description:  "Default directory for new downloads. Leave empty to use current directory.",
				Type:         TypeString,
				DefaultValue: defaultDir,
				Value:        defaultDir,
				ValidateFunc: func(val any) error {
					sVal, ok := val.(string)
					if !ok {
						return fmt.Errorf("must be a string")
					}
					trimmed := strings.TrimSpace(sVal)
					if trimmed != "" {
						if info, err := os.Stat(trimmed); err != nil {
							return fmt.Errorf("directory %q is inaccessible", trimmed)
						} else if !info.IsDir() {
							return fmt.Errorf("directory %q is not a folder", trimmed)
						}
					}
					return nil
				},
			},
			WarnOnDuplicate: &Setting{
				Key:          "warn_on_duplicate",
				Label:        "Warn on Duplicate",
				Description:  "Show warning when adding a download that already exists.",
				Type:         TypeBool,
				DefaultValue: true,
				Value:        true,
			},
			DownloadCompleteNotification: &Setting{
				Key:          "download_complete_notification",
				Label:        "Download Complete Notification",
				Description:  "Show system notification when a download finishes.",
				Type:         TypeBool,
				DefaultValue: true,
				Value:        true,
			},
			AutoShutdownAfterDownloads: &Setting{
				Key:          "auto_shutdown_after_downloads",
				Label:        "Auto Shutdown",
				Description:  "Shut down the computer after all downloads finish. Active, queued, and paused downloads block shutdown.",
				Type:         TypeBool,
				DefaultValue: false,
				Value:        false,
			},
			AllowRemoteOpenActions: &Setting{
				Key:          "allow_remote_open_actions",
				Label:        "Allow Remote Open Actions",
				Description:  "Allow /open-file and /open-folder API calls from non-loopback clients. Disabled by default for security.",
				Type:         TypeBool,
				NeedsRestart: true,
				DefaultValue: false,
				Value:        false,
			},
			AutoResume: &Setting{
				Key:          "auto_resume",
				Label:        "Auto Resume",
				Description:  "Automatically resume paused downloads on startup.",
				Type:         TypeBool,
				NeedsRestart: true,
				DefaultValue: false,
				Value:        false,
			},
			AutoStart: &Setting{
				Key:          "auto_start",
				Label:        "Automatic Startup",
				Description:  "Start Surge automatically when the system boots (requires service installation).",
				Type:         TypeBool,
				DefaultValue: false,
				Value:        false,
			},
			SkipUpdateCheck: &Setting{
				Key:          "skip_update_check",
				Label:        "Skip Update Check",
				Description:  "Disable automatic check for new versions on startup.",
				Type:         TypeBool,
				NeedsRestart: true,
				DefaultValue: false,
				Value:        false,
			},
			ClipboardMonitor: &Setting{
				Key:          "clipboard_monitor",
				Label:        "Clipboard Monitor",
				Description:  "Watch clipboard for URLs and prompt to download them.",
				Type:         TypeBool,
				NeedsRestart: true,
				DefaultValue: true,
				Value:        true,
			},
			Theme: &Setting{
				Key:          "theme",
				Label:        "App Theme",
				Description:  "UI Theme (System, Light, Dark).",
				Type:         TypeInt,
				DefaultValue: ThemeAdaptive,
				Value:        ThemeAdaptive,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 0 || v > 2 {
						return fmt.Errorf("theme must be 0, 1, or 2")
					}
					return nil
				},
			},
			ThemePath: &Setting{
				Key:          "theme_path",
				Label:        "Theme File",
				Description:  "Path to a custom .toml color scheme.",
				Type:         TypeString,
				DefaultValue: "",
				Value:        "",
			},
			LogRetentionCount: &Setting{
				Key:          "log_retention_count",
				Label:        "Log Retention Count",
				Description:  "Number of recent log files to keep.",
				Type:         TypeInt,
				NeedsRestart: true,
				DefaultValue: 5,
				Value:        5,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 1 || v > 100 {
						return fmt.Errorf("must be between 1 and 100")
					}
					return nil
				},
			},
			LiveSpeedGraph: &Setting{
				Key:          "live_speed_graph",
				Label:        "Live Speed Graph",
				Description:  "Use live speed for graph instead of EMA smoothed speed.",
				Type:         TypeBool,
				DefaultValue: false,
				Value:        false,
			},
			ShowSpeedGraph: &Setting{
				Key:          "show_speed_graph",
				Label:        "Show Speed Graph",
				Description:  "Display the network activity graph on the dashboard. Disable to reduce CPU usage.",
				Type:         TypeBool,
				DefaultValue: true,
				Value:        true,
			},
		},
		Network: NetworkSettings{
			MaxConnectionsPerDownload: &Setting{
				Key:          "max_connections_per_host",
				Label:        "Max Connections/Download",
				Description:  "Maximum concurrent connections per download (1-64).",
				Type:         TypeInt,
				DefaultValue: 32,
				Value:        32,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 1 || v > 64 {
						return fmt.Errorf("must be between 1 and 64")
					}
					return nil
				},
			},
			MaxConcurrentDownloads: &Setting{
				Key:          "max_concurrent_downloads",
				Label:        "Max Concurrent Downloads",
				Description:  "Maximum number of downloads running at once (1-10).",
				Type:         TypeInt,
				NeedsRestart: true,
				DefaultValue: 3,
				Value:        3,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 1 || v > 10 {
						return fmt.Errorf("must be between 1 and 10")
					}
					return nil
				},
			},
			MaxConcurrentProbes: &Setting{
				Key:          "max_concurrent_probes",
				Label:        "Max Concurrent Probes",
				Description:  "Maximum number of simultaneous server probes when adding many downloads at once (1-10).",
				Type:         TypeInt,
				NeedsRestart: true,
				DefaultValue: 3,
				Value:        3,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 1 || v > 10 {
						return fmt.Errorf("must be between 1 and 10")
					}
					return nil
				},
			},
			UserAgent: &Setting{
				Key:          "user_agent",
				Label:        "User Agent",
				Description:  "Custom User-Agent string for HTTP requests. Leave empty for default.",
				Type:         TypeString,
				DefaultValue: "",
				Value:        "",
			},
			ProxyURL: &Setting{
				Key:          "proxy_url",
				Label:        "Proxy URL",
				Description:  "HTTP/HTTPS proxy URL (e.g. http://127.0.0.1:1700). Leave empty to use system default.",
				Type:         TypeString,
				DefaultValue: "",
				Value:        "",
				ValidateFunc: func(val any) error {
					sVal, ok := val.(string)
					if !ok {
						return fmt.Errorf("must be a string")
					}
					if sVal != "" {
						u, err := url.Parse(sVal)
						if err != nil || u.Scheme == "" || u.Host == "" {
							return fmt.Errorf("invalid proxy URL")
						}
					}
					return nil
				},
			},
			CustomDNS: &Setting{
				Key:          "custom_dns",
				Label:        "Custom DNS Server",
				Description:  "Set custom DNS (e.g., 1.1.1.1:53, 94.140.14.14:53). Leave empty for system.",
				Type:         TypeString,
				DefaultValue: "",
				Value:        "",
				ValidateFunc: func(val any) error {
					sVal, ok := val.(string)
					if !ok {
						return fmt.Errorf("must be a string")
					}
					return ValidateDNSList(sVal)
				},
			},
			SequentialDownload: &Setting{
				Key:          "sequential_download",
				Label:        "Sequential Download",
				Description:  "Download pieces in order (Streaming Mode). May be slower.",
				Type:         TypeBool,
				DefaultValue: false,
				Value:        false,
			},
			MinChunkSize: &Setting{
				Key:          "min_chunk_size",
				Label:        "Min Chunk Size",
				Description:  "Minimum download chunk size in MiB (e.g., 2).",
				Type:         TypeInt64,
				DefaultValue: int64(2 * utils.MiB),
				Value:        int64(2 * utils.MiB),
				ValidateFunc: func(val any) error {
					vInt, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					v := int64(vInt)
					if v < 100*utils.KiB {
						return fmt.Errorf("min chunk size must be at least 100KiB")
					}
					return nil
				},
			},
			WorkerBufferSize: &Setting{
				Key:          "worker_buffer_size",
				Label:        "Worker Buffer Size",
				Description:  "I/O buffer size per worker in KiB (e.g., 512).",
				Type:         TypeInt,
				DefaultValue: int(512 * utils.KiB),
				Value:        int(512 * utils.KiB),
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 1*utils.KiB {
						return fmt.Errorf("worker buffer size must be at least 1KiB")
					}
					return nil
				},
			},
			DialHedgeCount: &Setting{
				Key:          "dial_hedge_count",
				Label:        "Dial Hedge Count",
				Description:  "Number of extra connections to dial pre-emptively; 0 disables connection prewarming and late duplicate range requests (0-16).",
				Type:         TypeInt,
				DefaultValue: 4,
				Value:        4,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 0 || v > 16 {
						return fmt.Errorf("must be between 0 and 16")
					}
					return nil
				},
			},
			GlobalRateLimit: &Setting{
				Key:          "global_rate_limit",
				Label:        "Global Rate Limit",
				Description:  "Cap total download bandwidth (e.g., 10MB/s, 80Mbps). Use 0 to disable.",
				Type:         TypeString,
				DefaultValue: "0",
				Value:        "0",
				ValidateFunc: func(val any) error {
					_, err := utils.ParseRateLimitValue(val)
					return err
				},
			},
			DefaultDownloadRateLimit: &Setting{
				Key:          "default_download_rate_limit",
				Label:        "Default Download Rate Limit",
				Description:  "Default cap per download (e.g., 2MB/s). Use 0 to disable.",
				Type:         TypeString,
				DefaultValue: "0",
				Value:        "0",
				ValidateFunc: func(val any) error {
					_, err := utils.ParseRateLimitValue(val)
					return err
				},
			},
		},
		Performance: PerformanceSettings{
			MaxTaskRetries: &Setting{
				Key:          "max_task_retries",
				Label:        "Max Task Retries",
				Description:  "Number of times to retry a failed chunk before giving up.",
				Type:         TypeInt,
				DefaultValue: 3,
				Value:        3,
				ValidateFunc: func(val any) error {
					v, err := parseAnyInt(val)
					if err != nil {
						return err
					}
					if v < 0 || v > 10 {
						return fmt.Errorf("must be between 0 and 10")
					}
					return nil
				},
			},
			SlowWorkerThreshold: &Setting{
				Key:          "slow_worker_threshold",
				Label:        "Slow Worker Threshold",
				Description:  "Restart workers slower than this fraction of mean speed (0.0-1.0, 0 disables relative slow-worker checks).",
				Type:         TypeFloat64,
				DefaultValue: 0.3,
				Value:        0.3,
				ValidateFunc: func(val any) error {
					var v float64
					switch actual := val.(type) {
					case float64:
						v = actual
					case int:
						v = float64(actual)
					case int64:
						v = float64(actual)
					default:
						return fmt.Errorf("invalid type")
					}
					if v < 0.0 || v > 1.0 {
						return fmt.Errorf("must be between 0.0 and 1.0")
					}
					return nil
				},
			},
			SlowWorkerGracePeriod: &Setting{
				Key:          "slow_worker_grace_period",
				Label:        "Slow Worker Grace",
				Description:  "Grace period before checking worker speed (e.g., 5s, 0 checks immediately).",
				Type:         TypeDuration,
				DefaultValue: 5 * time.Second,
				Value:        5 * time.Second,
				ValidateFunc: func(val any) error {
					var v int64
					switch actual := val.(type) {
					case time.Duration:
						v = int64(actual)
					case float64:
						v = int64(actual)
					case int64:
						v = actual
					default:
						return fmt.Errorf("invalid type")
					}
					if v < 0 {
						return fmt.Errorf("must be non-negative")
					}
					return nil
				},
			},
			StallTimeout: &Setting{
				Key:          "stall_timeout",
				Label:        "Stall Timeout",
				Description:  "Restart workers with no data for this duration (e.g., 5s, 0 disables stall detection).",
				Type:         TypeDuration,
				DefaultValue: 3 * time.Second,
				Value:        3 * time.Second,
				ValidateFunc: func(val any) error {
					var v int64
					switch actual := val.(type) {
					case time.Duration:
						v = int64(actual)
					case float64:
						v = int64(actual)
					case int64:
						v = actual
					default:
						return fmt.Errorf("invalid type")
					}
					if v < 0 {
						return fmt.Errorf("must be non-negative")
					}
					return nil
				},
			},
			SpeedEmaAlpha: &Setting{
				Key:          "speed_ema_alpha",
				Label:        "Speed EMA Alpha",
				Description:  "Exponential moving average smoothing factor (0.0-1.0, 0 disables smoothing).",
				Type:         TypeFloat64,
				DefaultValue: 0.3,
				Value:        0.3,
				ValidateFunc: func(val any) error {
					var v float64
					switch actual := val.(type) {
					case float64:
						v = actual
					case int:
						v = float64(actual)
					case int64:
						v = float64(actual)
					default:
						return fmt.Errorf("invalid type")
					}
					if v < 0.0 || v > 1.0 {
						return fmt.Errorf("must be between 0.0 and 1.0")
					}
					return nil
				},
			},
			AdaptiveConcurrency: &Setting{
				Key:          "adaptive_concurrency",
				Label:        "Adaptive Concurrency",
				Description:  "Reduce per-download connections after server throttling and recover gradually. Disable to keep the connection cap fixed.",
				Type:         TypeBool,
				DefaultValue: true,
				Value:        true,
			},
			AdaptiveConcurrencyRecoveryWindow: &Setting{
				Key:          "adaptive_concurrency_recovery_window",
				Label:        "Concurrency Recovery Window",
				Description:  "Healthy time required before each one-connection recovery step (1s-60s).",
				Type:         TypeDuration,
				DefaultValue: types.DefaultAdaptiveConcurrencyRecoveryWindow,
				Value:        types.DefaultAdaptiveConcurrencyRecoveryWindow,
				ValidateFunc: func(val any) error {
					var v time.Duration
					switch actual := val.(type) {
					case time.Duration:
						v = actual
					case float64:
						v = time.Duration(actual)
					case int64:
						v = time.Duration(actual)
					default:
						return fmt.Errorf("invalid type")
					}
					if v < time.Second || v > time.Minute {
						return fmt.Errorf("must be between 1s and 60s")
					}
					return nil
				},
			},
		},
		Categories: CategorySettings{
			CategoryEnabled: &Setting{
				Key:          "category_enabled",
				Label:        "Manage Categories",
				Description:  "Sort downloads into subfolders by file type. Press Enter to open Category Manager.",
				Type:         TypeBool,
				DefaultValue: false,
				Value:        false,
			},
			Categories: DefaultCategories(),
		},
		Extension: ExtensionSettings{
			ExtensionPrompt: &Setting{
				Key:          "extension_prompt",
				Label:        "Extension Prompt",
				Description:  "Prompt for confirmation when adding downloads via browser extension.",
				Type:         TypeBool,
				DefaultValue: true,
				Value:        true,
			},
			ChromeExtensionURL: &Setting{
				Key:          "chrome_extension_url",
				Label:        "Get Chrome Extension",
				Description:  "Open the Surge Chrome extension page.",
				Type:         TypeLink,
				DefaultValue: "https://chromewebstore.google.com/detail/surge-download-manager/cakjmkhlofkhjmfkjlclgbfdklhdnkgl",
				Value:        "https://chromewebstore.google.com/detail/surge-download-manager/cakjmkhlofkhjmfkjlclgbfdklhdnkgl",
			},
			FirefoxExtensionURL: &Setting{
				Key:          "firefox_extension_url",
				Label:        "Get Firefox Extension",
				Description:  "Open the Surge Firefox extension page.",
				Type:         TypeLink,
				DefaultValue: "https://addons.mozilla.org/en-US/firefox/addon/surge/",
				Value:        "https://addons.mozilla.org/en-US/firefox/addon/surge/",
			},
			AuthToken: &Setting{
				Key:          "auth_token",
				Label:        "Auth Token",
				Description:  "Your authentication token. Use this to connect the Browser Extension to Surge.",
				Type:         TypeAuthToken,
				DefaultValue: "",
				Value:        "",
			},
			InstructionsURL: &Setting{
				Key:          "instructions_url",
				Label:        "Setup Instructions",
				Description:  "View detailed instructions on how to set up the Surge browser extension.",
				Type:         TypeLink,
				DefaultValue: "https://github.com/SurgeDM/Surge#browser-extension",
				Value:        "https://github.com/SurgeDM/Surge#browser-extension",
			},
		},
	}

	s.initializeCategoriesList()
	return s
}

func (s *Settings) Validate() []string {
	s.StartupWarnings = nil

	// Loop over all settings in all categories
	for _, cat := range s.CategoriesList {
		for _, set := range cat.Settings {
			// If validation fails, log a warning and rollback to DefaultValue
			if err := set.Validate(set.Value); err != nil {
				set.Value = set.DefaultValue
				s.StartupWarnings = append(s.StartupWarnings, fmt.Sprintf("Reset setting '%s' to default: %v", set.Key, err))
			}
		}
	}

	// Dynamic extra validations for categories list in CategorySettings
	validCats := make([]Category, 0, len(s.Categories.Categories))
	for _, cat := range s.Categories.Categories {
		if err := cat.Validate(); err == nil {
			// Extra path check for each category
			catPath := strings.TrimSpace(cat.Path)
			if catPath != "" {
				if info, err := os.Stat(catPath); err != nil || !info.IsDir() {
					// Fallback to default download dir
					cat.Path = Resolve[string](s.General.DefaultDownloadDir)
					s.StartupWarnings = append(s.StartupWarnings, fmt.Sprintf("Category %q path is broken; reset to default", cat.Name))
				}
			}
			validCats = append(validCats, cat)
		} else {
			s.StartupWarnings = append(s.StartupWarnings, fmt.Sprintf("Removed invalid category %q: %v", cat.Name, err))
			utils.Debug("Config: Removing invalid category %q: %v", cat.Name, err)
		}
	}
	s.Categories.Categories = validCats

	return s.StartupWarnings
}

// ValidateDNSList checks if a comma-separated list of DNS servers (IP or IP:port) is valid.
func ValidateDNSList(s string) error {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		host, _, err := net.SplitHostPort(p)
		if err != nil {
			if net.ParseIP(p) == nil {
				return fmt.Errorf("invalid DNS: %s", p)
			}
		} else {
			if net.ParseIP(host) == nil {
				return fmt.Errorf("invalid DNS IP: %s", host)
			}
		}
	}
	return nil
}

var jsonWriteMutex sync.Mutex

// writeJSONAtomic marshals v as indented JSON and writes it to path atomically
// using a temp-file-then-rename strategy.
func writeJSONAtomic(path string, v any) error {
	jsonWriteMutex.Lock()
	defer jsonWriteMutex.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "json-*.tmp")
	if err != nil {
		return err
	}
	tempPath := f.Name()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Rename(tempPath, path)
}

var settingsWriteMutex sync.Mutex

// writeTOMLAtomic marshals v as TOML and writes it to path atomically
// using a temp-file-then-rename strategy.
func writeTOMLAtomic(path string, v any) error {
	settingsWriteMutex.Lock()
	defer settingsWriteMutex.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*.tmp")
	if err != nil {
		return err
	}
	tempPath := f.Name()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Rename(tempPath, path)
}

// SaveSettings saves settings to disk atomically.
func SaveSettings(s *Settings) error {
	raw := make(map[string]any)
	for _, cat := range s.CategoriesList {
		catKey := strings.ToLower(cat.Name)
		if cat.Name == "Categories" {
			catMap := make(map[string]any)
			if set := s.FindSetting("Categories", "category_enabled"); set != nil {
				catMap["category_enabled"] = set.Value
			}
			catsRaw := make([]map[string]any, 0, len(s.Categories.Categories))
			for _, c := range s.Categories.Categories {
				cMap := map[string]any{
					"name":        c.Name,
					"description": c.Description,
					"pattern":     c.Pattern,
					"path":        c.Path,
				}
				catsRaw = append(catsRaw, cMap)
			}
			catMap["categories"] = catsRaw
			raw[catKey] = catMap
			continue
		}

		catMap := make(map[string]any)
		for _, set := range cat.Settings {
			if set.Type == TypeDuration {
				if d, ok := set.Value.(time.Duration); ok {
					catMap[set.Key] = d.String()
					continue
				}
			}
			catMap[set.Key] = set.Value
		}
		raw[catKey] = catMap
	}
	return writeTOMLAtomic(GetSettingsPath(), raw)
}

// AddCategory adds a new custom download category and saves settings.
func (s *Settings) AddCategory(name, pattern, path string) error {
	for _, cat := range s.Categories.Categories {
		if strings.EqualFold(cat.Name, name) {
			return fmt.Errorf("%w: %q", ErrCategoryExists, name)
		}
	}
	s.Categories.Categories = append(s.Categories.Categories, Category{
		Name:    name,
		Pattern: pattern,
		Path:    path,
	})
	return SaveSettings(s)
}

// UpdateCategory updates an existing custom download category and saves settings.
func (s *Settings) UpdateCategory(name, pattern, path string) error {
	for i, cat := range s.Categories.Categories {
		if strings.EqualFold(cat.Name, name) {
			s.Categories.Categories[i].Pattern = pattern
			s.Categories.Categories[i].Path = path
			return SaveSettings(s)
		}
	}
	return fmt.Errorf("category %q not found", name)
}

// RemoveCategory removes a custom download category by name and saves settings.
func (s *Settings) RemoveCategory(name string) error {
	found := false
	var newCats []Category
	for _, cat := range s.Categories.Categories {
		if strings.EqualFold(cat.Name, name) {
			found = true
			continue
		}
		newCats = append(newCats, cat)
	}

	if !found {
		return fmt.Errorf("category %q not found", name)
	}

	s.Categories.Categories = newCats
	return SaveSettings(s)
}

// ToRuntimeConfig creates the engine runtime config from validated settings.
func (s *Settings) ToRuntimeConfig() *types.RuntimeConfig {
	var globalRate, defaultRate int64
	if s.Network.GlobalRateLimit != nil {
		var err error
		globalRate, err = utils.ParseRateLimitValue(s.Network.GlobalRateLimit.Value)
		if err != nil {
			globalRate, _ = utils.ParseRateLimitValue(s.Network.GlobalRateLimit.DefaultValue)
		}
	}
	if s.Network.DefaultDownloadRateLimit != nil {
		var err error
		defaultRate, err = utils.ParseRateLimitValue(s.Network.DefaultDownloadRateLimit.Value)
		if err != nil {
			defaultRate, _ = utils.ParseRateLimitValue(s.Network.DefaultDownloadRateLimit.DefaultValue)
		}
	}
	return &types.RuntimeConfig{
		MaxConnectionsPerDownload:         Resolve[int](s.Network.MaxConnectionsPerDownload),
		UserAgent:                         Resolve[string](s.Network.UserAgent),
		ProxyURL:                          Resolve[string](s.Network.ProxyURL),
		CustomDNS:                         Resolve[string](s.Network.CustomDNS),
		SequentialDownload:                Resolve[bool](s.Network.SequentialDownload),
		MinChunkSize:                      Resolve[int64](s.Network.MinChunkSize),
		GlobalRateLimitBps:                globalRate,
		DefaultDownloadRateLimitBps:       defaultRate,
		WorkerBufferSize:                  Resolve[int](s.Network.WorkerBufferSize),
		DialHedgeCount:                    Resolve[int](s.Network.DialHedgeCount),
		MaxTaskRetries:                    Resolve[int](s.Performance.MaxTaskRetries),
		SlowWorkerThreshold:               Resolve[float64](s.Performance.SlowWorkerThreshold),
		SlowWorkerGracePeriod:             Resolve[time.Duration](s.Performance.SlowWorkerGracePeriod),
		StallTimeout:                      Resolve[time.Duration](s.Performance.StallTimeout),
		SpeedEmaAlpha:                     Resolve[float64](s.Performance.SpeedEmaAlpha),
		DisableAdaptiveConcurrency:        !Resolve[bool](s.Performance.AdaptiveConcurrency),
		AdaptiveConcurrencyRecoveryWindow: Resolve[time.Duration](s.Performance.AdaptiveConcurrencyRecoveryWindow),
	}
}

// Clone returns a deep copy of the settings.
func (s *Settings) Clone() *Settings {
	if s == nil {
		return nil
	}
	cloned := DefaultSettings()

	for _, clonedCat := range cloned.CategoriesList {
		sourceCat := s.FindSettingsCategory(clonedCat.Name)
		if sourceCat == nil {
			continue
		}
		for _, clonedSetting := range clonedCat.Settings {
			if sourceSetting := s.FindSetting(sourceCat.Name, clonedSetting.Key); sourceSetting != nil {
				clonedSetting.Value = sourceSetting.Value
			}
		}
	}

	// Deep copy custom categories
	if len(s.Categories.Categories) > 0 {
		cloned.Categories.Categories = make([]Category, len(s.Categories.Categories))
		copy(cloned.Categories.Categories, s.Categories.Categories)
	}

	// Deep copy startup warnings
	if len(s.StartupWarnings) > 0 {
		cloned.StartupWarnings = append([]string(nil), s.StartupWarnings...)
	}

	return cloned
}
