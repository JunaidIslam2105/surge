package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/SurgeDM/Surge/internal/utils"
)

// KeyMap defines the keybindings for the entire application
type KeyMap struct {
	Dashboard      DashboardKeyMap       `json:"dashboard"`
	Input          InputKeyMap           `json:"input"`
	FilePicker     FilePickerKeyMap      `json:"file_picker"`
	Duplicate      DuplicateKeyMap       `json:"duplicate"`
	Extension      ExtensionKeyMap       `json:"extension"`
	Settings       SettingsKeyMap        `json:"settings"`
	SettingsEditor SettingsEditorKeyMap  `json:"settings_editor"`
	BatchConfirm   BatchConfirmKeyMap    `json:"batch_confirm"`
	Update         UpdateKeyMap          `json:"update"`
	BugReport      BugReportKeyMap       `json:"bug_report"`
	CategoryMgr    CategoryManagerKeyMap `json:"category_mgr"`
	SpeedLimits    SpeedLimitsKeyMap     `json:"speed_limits"`
	QuitConfirm    QuitConfirmKeyMap     `json:"quit_confirm"`

	// StartupWarnings holds validation messages from the most recent LoadKeyMap call.
	// It is ignored during JSON serialization.
	StartupWarnings []string `json:"-"`
}

// DashboardKeyMap defines keybindings for the main dashboard
type DashboardKeyMap struct {
	TabQueued        key.Binding
	TabActive        key.Binding
	TabDone          key.Binding
	NextTab          key.Binding
	PrevTab          key.Binding
	Add              key.Binding
	AddFromClipboard key.Binding
	BatchImport      key.Binding
	Search           key.Binding
	Pause            key.Binding
	Refresh          key.Binding
	Delete           key.Binding
	PurgeFile        key.Binding
	Settings         key.Binding
	SpeedLimits      key.Binding
	Log              key.Binding
	ToggleHelp       key.Binding
	ReportBug        key.Binding
	OpenFile         key.Binding
	OpenFolder       key.Binding
	Quit             key.Binding
	ForceQuit        key.Binding
	CategoryFilter   key.Binding
	PinTab           key.Binding
	// Navigation
	Up   key.Binding
	Down key.Binding
	// Log Navigation
	LogUp     key.Binding
	LogDown   key.Binding
	LogTop    key.Binding
	LogBottom key.Binding
	LogClose  key.Binding
}

// InputKeyMap defines keybindings for the add download input
type InputKeyMap struct {
	Tab    key.Binding
	Enter  key.Binding
	Esc    key.Binding
	Up     key.Binding
	Down   key.Binding
	Cancel key.Binding
}

// FilePickerKeyMap defines keybindings for the file picker
type FilePickerKeyMap struct {
	UseDir   key.Binding
	GotoHome key.Binding
	Back     key.Binding
	Forward  key.Binding
	Open     key.Binding
	Jump     key.Binding
	Cancel   key.Binding
}

// DuplicateKeyMap defines keybindings for duplicate warning
type DuplicateKeyMap struct {
	Continue key.Binding
	Focus    key.Binding
	Cancel   key.Binding
}

// ExtensionKeyMap defines keybindings for extension confirmation
type ExtensionKeyMap struct {
	Confirm key.Binding
	Browse  key.Binding
	Next    key.Binding
	Prev    key.Binding
	Cancel  key.Binding
}

// SettingsKeyMap defines keybindings for the settings view
type SettingsKeyMap struct {
	Tab1      key.Binding
	Tab2      key.Binding
	Tab3      key.Binding
	Tab4      key.Binding
	Tab5      key.Binding
	NextTab   key.Binding
	PrevTab   key.Binding
	Browse    key.Binding
	Edit      key.Binding
	Up        key.Binding
	Down      key.Binding
	Reset     key.Binding
	Close     key.Binding
	ReportBug key.Binding
}

// SettingsEditorKeyMap defines keybindings for editing a setting
type SettingsEditorKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// BatchConfirmKeyMap defines keybindings for batch import confirmation
type BatchConfirmKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// UpdateKeyMap defines keybindings for update notification
type UpdateKeyMap struct {
	Install    key.Binding
	OpenGitHub key.Binding
	IgnoreNow  key.Binding
}

// BugReportKeyMap defines keybindings for selecting bug report target.
type BugReportKeyMap struct {
	Core      key.Binding
	Extension key.Binding
	Cancel    key.Binding
}

// QuitConfirmKeyMap defines keybindings for the quit confirmation modal
type QuitConfirmKeyMap struct {
	Left   key.Binding
	Right  key.Binding
	Yes    key.Binding
	No     key.Binding
	Select key.Binding
	Cancel key.Binding
}

// CategoryManagerKeyMap defines keybindings for the category manager
type CategoryManagerKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Edit   key.Binding
	Add    key.Binding
	Delete key.Binding
	Toggle key.Binding // toggle enable/disable
	Tab    key.Binding
	Close  key.Binding
}

// SpeedLimitsKeyMap defines keybindings for speed limits
type SpeedLimitsKeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Edit  key.Binding
	Reset key.Binding
	Close key.Binding
}

// KeyBindingConfig represents a single key binding.
type KeyBindingConfig struct {
	Keys []string `json:"keys"`
	Help string   `json:"help"`
}

// KeyMapConfig mirrors the structure of KeyMap for configuration.
type KeyMapConfig struct {
	Dashboard      map[string]KeyBindingConfig `json:"dashboard"`
	Input          map[string]KeyBindingConfig `json:"input"`
	FilePicker     map[string]KeyBindingConfig `json:"file_picker"`
	Duplicate      map[string]KeyBindingConfig `json:"duplicate"`
	Extension      map[string]KeyBindingConfig `json:"extension"`
	Settings       map[string]KeyBindingConfig `json:"settings"`
	SettingsEditor map[string]KeyBindingConfig `json:"settings_editor"`
	BatchConfirm   map[string]KeyBindingConfig `json:"batch_confirm"`
	Update         map[string]KeyBindingConfig `json:"update"`
	BugReport      map[string]KeyBindingConfig `json:"bug_report"`
	CategoryMgr    map[string]KeyBindingConfig `json:"category_mgr"`
	SpeedLimits    map[string]KeyBindingConfig `json:"speed_limits"`
	QuitConfirm    map[string]KeyBindingConfig `json:"quit_confirm"`
}

// GetKeyMapConfigPath returns the path to the Keymaps JSON file.
func GetKeyMapConfigPath() string {
	return filepath.Join(GetSurgeDir(), "keymap.json")
}

// LoadKeyMap loads the keymap configuration from file.
func LoadKeyMap() (*KeyMap, error) {
	defaults := DefaultKeyMap()
	path := GetKeyMapConfigPath()
	utils.Debug("Loading keymap from %s", path)
	data, err := os.ReadFile(path)
	if err != nil {
		utils.Debug("Failed to read keymap file: %v", err)
		if os.IsNotExist(err) {
			utils.Debug("Warning: Created New %s file \u2014 using defaults", path)
			defaults.StartupWarnings = append(defaults.StartupWarnings, "Config: created new keymap file "+path)
			_ = SaveKeyMap(defaults)
			return defaults, nil
		}
		return nil, err
	}

	var cfg KeyMapConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		utils.Debug("Warning: corrupt keymap file %s: %v \u2014 using defaults", path, err)
		defaults.StartupWarnings = append(defaults.StartupWarnings,
			fmt.Sprintf("Config: keymap file is corrupt (%v) \u2014 all keybindings reset to defaults & rewrite the file", err))
		err = SaveKeyMap(defaults)
		return defaults, err
	}

	defaults.ApplyConfig(&cfg)
	defaults.Validate()

	// Self-healing: save the fully-merged and validated keymap back to disk
	// ONLY if it differs from what was on disk to avoid constant disk thrashing.
	mergedData, _ := json.MarshalIndent(defaults.ToConfig(), "", "  ")
	if string(data) != string(mergedData) {
		defaults.StartupWarnings = append(defaults.StartupWarnings, "Config: keymap file was updated to include missing keys or fix formatting")
		_ = SaveKeyMap(defaults)
	}

	return defaults, nil
}

// SaveKeyMap saves the keymap configuration to file.
func SaveKeyMap(k *KeyMap) error {
	return writeJSONAtomic(GetKeyMapConfigPath(), k.ToConfig())
}

// ApplyConfig applies configuration from KeyMapConfig to KeyMap.
func (k *KeyMap) ApplyConfig(cfg *KeyMapConfig) {
	if cfg == nil {
		return
	}

	applyToStruct := func(s any, m map[string]KeyBindingConfig) {
		ciMap := make(map[string]KeyBindingConfig)
		for k, vCfg := range m {
			ciMap[strings.ToLower(k)] = vCfg
		}

		v := reflect.ValueOf(s).Elem()
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Type() == reflect.TypeFor[key.Binding]() {
				fieldName := strings.ToLower(t.Field(i).Name)
				if bCfg, ok := ciMap[fieldName]; ok {
					if len(bCfg.Keys) > 0 {
						helpDesc := bCfg.Help
						if helpDesc == "" {
							helpDesc = field.Interface().(key.Binding).Help().Desc
						}
						helpKey := strings.Join(bCfg.Keys, "/")
						newBinding := key.NewBinding(
							key.WithKeys(bCfg.Keys...),
							key.WithHelp(helpKey, helpDesc),
						)
						field.Set(reflect.ValueOf(newBinding))
					}
				}
			}
		}
	}

	applyToStruct(&k.Dashboard, cfg.Dashboard)
	applyToStruct(&k.Input, cfg.Input)
	applyToStruct(&k.FilePicker, cfg.FilePicker)
	applyToStruct(&k.Duplicate, cfg.Duplicate)
	applyToStruct(&k.Extension, cfg.Extension)
	applyToStruct(&k.Settings, cfg.Settings)
	applyToStruct(&k.SettingsEditor, cfg.SettingsEditor)
	applyToStruct(&k.BatchConfirm, cfg.BatchConfirm)
	applyToStruct(&k.Update, cfg.Update)
	applyToStruct(&k.BugReport, cfg.BugReport)
	applyToStruct(&k.CategoryMgr, cfg.CategoryMgr)
	applyToStruct(&k.SpeedLimits, cfg.SpeedLimits)
	applyToStruct(&k.QuitConfirm, cfg.QuitConfirm)
}

// ToConfig converts KeyMap to KeyMapConfig for serialization.
func (k *KeyMap) ToConfig() *KeyMapConfig {
	structToMap := func(s any) map[string]KeyBindingConfig {
		v := reflect.ValueOf(s)
		t := v.Type()
		m := make(map[string]KeyBindingConfig)
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Type() == reflect.TypeFor[key.Binding]() {
				binding := field.Interface().(key.Binding)
				m[t.Field(i).Name] = KeyBindingConfig{
					Keys: binding.Keys(),
					Help: binding.Help().Desc,
				}
			}
		}
		return m
	}

	return &KeyMapConfig{
		Dashboard:      structToMap(k.Dashboard),
		Input:          structToMap(k.Input),
		FilePicker:     structToMap(k.FilePicker),
		Duplicate:      structToMap(k.Duplicate),
		Extension:      structToMap(k.Extension),
		Settings:       structToMap(k.Settings),
		SettingsEditor: structToMap(k.SettingsEditor),
		BatchConfirm:   structToMap(k.BatchConfirm),
		Update:         structToMap(k.Update),
		BugReport:      structToMap(k.BugReport),
		CategoryMgr:    structToMap(k.CategoryMgr),
		SpeedLimits:    structToMap(k.SpeedLimits),
		QuitConfirm:    structToMap(k.QuitConfirm),
	}
}

// Validate checks keymap for missing or invalid bindings and fills with defaults at both section and binding levels.
func (k *KeyMap) Validate() {
	defaults := DefaultKeyMap()
	if k == nil {
		return
	}

	v := reflect.ValueOf(k).Elem()
	dV := reflect.ValueOf(defaults).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		defaultField := dV.Field(i)
		fieldType := t.Field(i).Type

		if fieldType.Kind() == reflect.Struct {
			// If the entire section struct is zero, copy it completely from default
			if reflect.DeepEqual(field.Interface(), reflect.Zero(fieldType).Interface()) {
				field.Set(defaultField)
				continue
			}

			// Otherwise, do field-by-field check for each key.Binding within the section
			// to heal any individual missing or invalid bindings.
			if field.CanAddr() {
				fieldAddr := field.Addr().Interface()
				defaultFieldVal := defaultField

				subV := reflect.ValueOf(fieldAddr).Elem()
				subDV := defaultFieldVal

				for j := 0; j < subV.NumField(); j++ {
					subField := subV.Field(j)
					if subField.Type() == reflect.TypeFor[key.Binding]() {
						b := subField.Interface().(key.Binding)
						// If the binding is uninitialized or has no keys configured, restore the default binding
						if len(b.Keys()) == 0 {
							subField.Set(subDV.Field(j))
						}
					}
				}
			}
		}
	}
}

func DefaultKeyMap() *KeyMap {
	return &KeyMap{
		Dashboard: DashboardKeyMap{
			TabQueued: key.NewBinding(
				key.WithKeys("q"),
				key.WithHelp("q", "queued tab"),
			),
			TabActive: key.NewBinding(
				key.WithKeys("w"),
				key.WithHelp("w", "active tab"),
			),
			TabDone: key.NewBinding(
				key.WithKeys("e"),
				key.WithHelp("e", "done tab"),
			),
			NextTab: key.NewBinding(
				key.WithKeys("tab", "right"),
				key.WithHelp("tab/\u2192", "next tab"),
			),
			PrevTab: key.NewBinding(
				key.WithKeys("shift+tab", "left"),
				key.WithHelp("shift+tab/\u2190", "prev tab"),
			),
			Add: key.NewBinding(
				key.WithKeys("a"),
				key.WithHelp("a", "add download"),
			),
			AddFromClipboard: key.NewBinding(
				key.WithKeys("A"),
				key.WithHelp("A", "add from clipboard"),
			),
			BatchImport: key.NewBinding(
				key.WithKeys("b", "B"),
				key.WithHelp("b", "batch import"),
			),
			Search: key.NewBinding(
				key.WithKeys("f"),
				key.WithHelp("f", "search"),
			),
			Pause: key.NewBinding(
				key.WithKeys("p"),
				key.WithHelp("p", "pause/resume"),
			),
			Refresh: key.NewBinding(
				key.WithKeys("r"),
				key.WithHelp("r", "refresh url"),
			),
			Delete: key.NewBinding(
				key.WithKeys("x"),
				key.WithHelp("x", "delete"),
			),
			PurgeFile: key.NewBinding(
				key.WithKeys("X", "shift+x"),
				key.WithHelp("X", "delete+purge"),
			),
			Settings: key.NewBinding(
				key.WithKeys("s"),
				key.WithHelp("s", "settings"),
			),
			SpeedLimits: key.NewBinding(
				key.WithKeys("T"),
				key.WithHelp("T", "speed limits"),
			),
			Log: key.NewBinding(
				key.WithKeys("l"),
				key.WithHelp("l", "toggle log"),
			),
			ToggleHelp: key.NewBinding(
				key.WithKeys("h"),
				key.WithHelp("h", "toggle help"),
			),
			ReportBug: key.NewBinding(
				key.WithKeys("?"),
				key.WithHelp("?", "bug report"),
			),
			OpenFile: key.NewBinding(
				key.WithKeys("o"),
				key.WithHelp("o", "open file"),
			),
			OpenFolder: key.NewBinding(
				key.WithKeys("O"),
				key.WithHelp("O", "open folder"),
			),
			Quit: key.NewBinding(
				key.WithKeys("ctrl+c", "ctrl+q"),
				key.WithHelp("ctrl+q", "quit"),
			),
			ForceQuit: key.NewBinding(
				key.WithKeys("ctrl+c"),
				key.WithHelp("ctrl+c", "force quit"),
			),
			CategoryFilter: key.NewBinding(
				key.WithKeys("c"),
				key.WithHelp("c", "category"),
			),
			PinTab: key.NewBinding(
				key.WithKeys("t"),
				key.WithHelp("t", "pin tab"),
			),
			Up: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("\u2191/k", "up"),
			),
			Down: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("\u2193/j", "down"),
			),
			LogUp: key.NewBinding(
				key.WithKeys("up"),
				key.WithHelp("\u2191", "scroll up"),
			),
			LogDown: key.NewBinding(
				key.WithKeys("down"),
				key.WithHelp("\u2193", "scroll down"),
			),
			LogTop: key.NewBinding(
				key.WithKeys("g"),
				key.WithHelp("g", "top"),
			),
			LogBottom: key.NewBinding(
				key.WithKeys("G"),
				key.WithHelp("G", "bottom"),
			),
			LogClose: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "close log"),
			),
		},
		Input: InputKeyMap{
			Tab: key.NewBinding(
				key.WithKeys("tab"),
				key.WithHelp("tab", "browse/next"),
			),
			Enter: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "confirm/next"),
			),
			Esc: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
			Up: key.NewBinding(
				key.WithKeys("up"),
				key.WithHelp("\u2191", "previous"),
			),
			Down: key.NewBinding(
				key.WithKeys("down"),
				key.WithHelp("\u2193", "next"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		},
		FilePicker: FilePickerKeyMap{
			UseDir: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "use current"),
			),
			GotoHome: key.NewBinding(
				key.WithKeys("h", "H"),
				key.WithHelp("h/H", "home"),
			),
			Back: key.NewBinding(
				key.WithKeys("left"),
				key.WithHelp("\u2190", "back"),
			),
			Forward: key.NewBinding(
				key.WithKeys("right"),
				key.WithHelp("\u2192", "open"),
			),
			Open: key.NewBinding(
				key.WithKeys("."),
				key.WithHelp(".", "select highlighted"),
			),
			Jump: key.NewBinding(
				key.WithKeys("a-z", "0-9"),
				key.WithHelp("a-z/0-9", "jump to file"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		},
		Duplicate: DuplicateKeyMap{
			Continue: key.NewBinding(
				key.WithKeys("c", "C", "enter"),
				key.WithHelp("c/enter", "continue"),
			),
			Focus: key.NewBinding(
				key.WithKeys("f", "F", "down", "j"),
				key.WithHelp("f/j", "focus existing"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("x", "X", "esc", "q"),
				key.WithHelp("x/q", "cancel"),
			),
		},
		Extension: ExtensionKeyMap{
			Confirm: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "confirm"),
			),
			Browse: key.NewBinding(
				key.WithKeys("tab"),
				key.WithHelp("tab", "browse path"),
			),
			Next: key.NewBinding(
				key.WithKeys("down", "j", "tab"),
				key.WithHelp("\u2193/j", "next field"),
			),
			Prev: key.NewBinding(
				key.WithKeys("up", "k", "shift+tab"),
				key.WithHelp("\u2191/k", "prev field"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		},
		Settings: SettingsKeyMap{
			Tab1: key.NewBinding(
				key.WithKeys("1"),
				key.WithHelp("1", "general"),
			),
			Tab2: key.NewBinding(
				key.WithKeys("2"),
				key.WithHelp("2", "network"),
			),
			Tab3: key.NewBinding(
				key.WithKeys("3"),
				key.WithHelp("3", "performance"),
			),
			Tab4: key.NewBinding(
				key.WithKeys("4"),
				key.WithHelp("4", "categories"),
			),
			Tab5: key.NewBinding(
				key.WithKeys("5"),
				key.WithHelp("5", "extension"),
			),
			NextTab: key.NewBinding(
				key.WithKeys("right", "l"),
				key.WithHelp("\u2192/l", "next tab"),
			),
			PrevTab: key.NewBinding(
				key.WithKeys("left", "h"),
				key.WithHelp("\u2190/h", "prev tab"),
			),
			Browse: key.NewBinding(
				key.WithKeys("tab"),
				key.WithHelp("tab", "browse dir"),
			),
			Edit: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "edit"),
			),
			Up: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("\u2191/k", "up"),
			),
			Down: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("\u2193/j", "down"),
			),
			Reset: key.NewBinding(
				key.WithKeys("r", "R"),
				key.WithHelp("r", "reset"),
			),
			Close: key.NewBinding(
				key.WithKeys("esc", "q"),
				key.WithHelp("esc/q", "save & close"),
			),
			ReportBug: key.NewBinding(
				key.WithKeys("?"),
				key.WithHelp("?", "bug report"),
			),
		},
		SettingsEditor: SettingsEditorKeyMap{
			Confirm: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "confirm"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		},
		BatchConfirm: BatchConfirmKeyMap{
			Confirm: key.NewBinding(
				key.WithKeys("y", "Y", "enter"),
				key.WithHelp("y", "confirm"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("n", "N", "esc"),
				key.WithHelp("n", "cancel"),
			),
		},
		Update: UpdateKeyMap{
			Install: key.NewBinding(
				key.WithKeys("enter", "u", "U"),
				key.WithHelp("enter/u", "install update"),
			),
			OpenGitHub: key.NewBinding(
				key.WithKeys("o", "O"),
				key.WithHelp("o", "open on github"),
			),
			IgnoreNow: key.NewBinding(
				key.WithKeys("i", "I", "esc"),
				key.WithHelp("esc/i", "later"),
			),
		},
		BugReport: BugReportKeyMap{
			Core: key.NewBinding(
				key.WithKeys("1", "c", "C"),
				key.WithHelp("1", "core report"),
			),
			Extension: key.NewBinding(
				key.WithKeys("2", "e", "E"),
				key.WithHelp("2", "extension report"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc", "q"),
				key.WithHelp("esc/q", "cancel"),
			),
		},
		CategoryMgr: CategoryManagerKeyMap{
			Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("\u2191/k", "up")),
			Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("\u2193/j", "down")),
			Edit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
			Add:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
			Delete: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
			Toggle: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle")),
			Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
			Close:  key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "save & close")),
		},
		SpeedLimits: SpeedLimitsKeyMap{
			Up:    key.NewBinding(key.WithKeys("up"), key.WithHelp("\u2191", "up")),
			Down:  key.NewBinding(key.WithKeys("down"), key.WithHelp("\u2193", "down")),
			Edit:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
			Reset: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reset")),
			Close: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel/close")),
		},
		QuitConfirm: QuitConfirmKeyMap{
			Left: key.NewBinding(
				key.WithKeys("left", "h"),
			),
			Right: key.NewBinding(
				key.WithKeys("right", "l", "tab"),
			),
			Yes: key.NewBinding(
				key.WithKeys("y", "Y"),
			),
			No: key.NewBinding(
				key.WithKeys("n", "N"),
			),
			Select: key.NewBinding(
				key.WithKeys("enter", "space"),
				key.WithHelp("y/enter", "confirm"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc", "ctrl+c", "ctrl+q"),
				key.WithHelp("n/esc", "cancel"),
			),
		},
		StartupWarnings: nil,
	}
}

// ShortHelp returns keybindings to show in the mini help view
func (k DashboardKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ToggleHelp, k.ReportBug}
}

// FullHelp returns keybindings for the expanded help view
func (k DashboardKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.TabQueued, k.TabActive, k.TabDone, k.NextTab, k.PrevTab},
		{k.Add, k.AddFromClipboard, k.BatchImport, k.Search, k.CategoryFilter, k.Pause, k.Refresh, k.Delete, k.PurgeFile, k.Settings, k.SpeedLimits, k.PinTab},
		{k.Log, k.OpenFile, k.OpenFolder, k.ToggleHelp, k.ReportBug, k.Quit},
	}
}

func (k InputKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Enter, k.Esc}
}

func (k InputKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Tab, k.Enter, k.Esc}}
}

func (k FilePickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Forward, k.UseDir, k.GotoHome, k.Open, k.Jump, k.Cancel}
}

func (k FilePickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Back, k.Forward, k.UseDir, k.GotoHome, k.Open, k.Jump, k.Cancel}}
}

func (k DuplicateKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Continue, k.Focus, k.Cancel}
}

func (k DuplicateKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Continue, k.Focus, k.Cancel}}
}

func (k ExtensionKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Browse, k.Prev, k.Next, k.Confirm, k.Cancel}
}

func (k ExtensionKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Browse, k.Prev, k.Next, k.Confirm, k.Cancel}}
}

func (k SettingsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PrevTab, k.NextTab, k.Edit, k.Reset, k.Close}
}

func (k SettingsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab1, k.Tab2, k.Tab3, k.Tab4, k.Tab5},
		{k.PrevTab, k.NextTab, k.Up, k.Down, k.Edit, k.Reset, k.Browse, k.ReportBug, k.Close},
	}
}

func (k SettingsEditorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k SettingsEditorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}}
}

func (k BatchConfirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k BatchConfirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}}
}

func (k UpdateKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Install, k.OpenGitHub, k.IgnoreNow}
}

func (k UpdateKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Install, k.OpenGitHub, k.IgnoreNow}}
}

func (k BugReportKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Core, k.Extension, k.Cancel}
}

func (k BugReportKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Core, k.Extension, k.Cancel}}
}

func (k CategoryManagerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Edit, k.Add, k.Delete, k.Tab, k.Toggle, k.Close}
}

func (k CategoryManagerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Edit, k.Tab, k.Toggle},
		{k.Add, k.Delete, k.Close},
	}
}

func (k SpeedLimitsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Edit, k.Reset, k.Close}
}

func (k SpeedLimitsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Edit, k.Reset, k.Close},
	}
}

func (k QuitConfirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Cancel}
}

func (k QuitConfirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Cancel}}
}
