package config

// Setting represents a single application configuration option.
// Note: Custom MarshalJSON/UnmarshalJSON methods on *Setting (in settings.go)
// serialize only the Value field. The json tags on other fields are not used
// during normal serialization but document the intended schema.
type SettingType string

const (
	TypeString            SettingType = "string"
	TypeInt               SettingType = "int"
	TypeInt64             SettingType = "int64"
	TypeBool              SettingType = "bool"
	TypeFloat64           SettingType = "float64"
	TypeDuration          SettingType = "duration"
	TypeAuthToken         SettingType = "auth_token"
	TypeLink              SettingType = "link"
	TypeCustomCategory    SettingType = "custom_category"
	TypeCustomCategoryAdd SettingType = "custom_category_add"
)

// SettingUnit describes the unit used by the settings UI to display and edit a value.
// It is independent of SettingType, which describes the value's stored Go type.
type SettingUnit string

const (
	UnitNone               SettingUnit = ""
	UnitSeconds            SettingUnit = "seconds"
	UnitMinutes            SettingUnit = "minutes"
	UnitKilobytes          SettingUnit = "kilobytes"
	UnitMegabytes          SettingUnit = "megabytes"
	UnitMegabytesPerSecond SettingUnit = "megabytes_per_second"
	UnitConnections        SettingUnit = "connections"
	UnitRetries            SettingUnit = "retries"
	UnitRatio              SettingUnit = "ratio"
)

type Setting struct {
	Key          string      `json:"key"`
	Label        string      `json:"label"`
	Description  string      `json:"description"`
	NeedsRestart bool        `json:"needs_restart"`
	Type         SettingType `json:"type"`
	Unit         SettingUnit `json:"unit"`

	Value        any `json:"value"`
	DefaultValue any `json:"default_value"`

	// ValidateFunc is a custom validator for this setting.
	ValidateFunc func(val any) error `json:"-" toml:"-"`
}

// Validate checks the given value against any custom validation rule.
func (s *Setting) Validate(val any) error {
	if s.ValidateFunc != nil {
		return s.ValidateFunc(val)
	}
	return nil
}

// SettingsCategory represents a group of related Setting options.
type SettingsCategory struct {
	Name     string     `json:"name"`
	Settings []*Setting `json:"settings"`
}
