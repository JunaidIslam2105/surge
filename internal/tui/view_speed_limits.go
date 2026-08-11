package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/utils"
)

func (m RootModel) viewSpeedLimits() string {
	w, h := GetDynamicModalDimensions(m.width, m.height, 60, 10, 80, 20)

	metaList := m.getSpeedLimitsMetadata()
	values := m.getSpeedLimitsValues()

	items := make([]components.ListInputItem, 0, len(metaList))

	for i, meta := range metaList {
		val := values[meta.Key]
		var valStr string
		if vStr, ok := val.(string); ok {
			valStr = vStr
		} else {
			valStr = fmt.Sprintf("%v", val)
		}

		if bps, err := utils.ParseRateLimitValue(valStr); err == nil && bps == 0 {
			valStr = "0"
		}

		suffix := "MB/s"
		if strings.HasPrefix(meta.Key, "dl:") {
			defaultRate := int64(0)
			if m.Settings != nil && m.Settings.Network.DefaultDownloadRateLimit != nil {
				if rate, err := utils.ParseRateLimitValue(m.Settings.Network.DefaultDownloadRateLimit.Value); err == nil {
					defaultRate = rate
				}
			}
			displayRate := utils.FormatRateLimit(defaultRate)
			if defaultRate <= 0 {
				displayRate = "0 MB/s"
			}
			suffix = fmt.Sprintf("MB/s (-1 to inherit %s)", displayRate)
		}
		items = append(items, components.ListInputItem{
			Label:       meta.Label,
			Value:       valStr,
			IsEditing:   m.speedLimitsIsEditing && m.speedLimitsCursor == i,
			InputSuffix: suffix,
		})
	}

	var helpKeys help.KeyMap = m.keys.SpeedLimits
	if m.speedLimitsIsEditing {
		helpKeys = m.keys.SettingsEditor
	}

	modal := components.ListInputModal{
		Title:       "Speed Limits",
		Subtitle:    "Note: Enter 0 for infinity/unlimited.",
		Items:       items,
		Cursor:      m.speedLimitsCursor,
		Input:       m.SettingsInput,
		Help:        m.help,
		HelpKeys:    helpKeys,
		BorderColor: colors.Magenta(),
		Width:       w,
		Height:      h,
		Error:       m.speedLimitsError,
	}

	box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
	return box
}

func (m RootModel) getSpeedLimitsMetadata() []config.SettingMeta {
	networkMeta := config.GetSettingsMetadata()["Network"]
	keyToMeta := make(map[string]config.SettingMeta, len(networkMeta))
	for _, m := range networkMeta {
		keyToMeta[m.Key] = m
	}
	meta := []config.SettingMeta{
		keyToMeta["global_rate_limit"],
		keyToMeta["default_download_rate_limit"],
	}

	for _, d := range m.downloads {
		if !d.done {
			label := d.Filename
			if label == "" {
				label = d.ID
			}
			meta = append(meta, config.SettingMeta{
				Key:         "dl:" + d.ID,
				Label:       label,
				Description: fmt.Sprintf("Speed limit for this specific download: %s. Use \"inherit\" for default, or 0 to disable.", label),
				Type:        "string",
			})
		}
	}
	return meta
}

func (m RootModel) getSpeedLimitsValues() map[string]interface{} {
	values := make(map[string]interface{})

	if m.Settings != nil && m.Settings.Network.GlobalRateLimit != nil {
		values["global_rate_limit"] = m.Settings.Network.GlobalRateLimit.Value
	} else {
		values["global_rate_limit"] = "0"
	}

	if m.Settings != nil && m.Settings.Network.DefaultDownloadRateLimit != nil {
		values["default_download_rate_limit"] = m.Settings.Network.DefaultDownloadRateLimit.Value
	} else {
		values["default_download_rate_limit"] = "0"
	}

	for _, d := range m.downloads {
		if !d.done {
			values["dl:"+d.ID] = m.formatDownloadRateLimitValue(d)
		}
	}
	return values
}

func (m *RootModel) setSpeedLimitValue(key, value string) error {
	if m.Settings == nil {
		return fmt.Errorf("settings not loaded")
	}

	if key == "global_rate_limit" {
		if utils.IsRateLimitInherit(value) {
			return fmt.Errorf("global rate limit cannot be inherited")
		}
		rate, err := utils.ParseRateLimit(value)
		if err != nil {
			return err
		}
		if err := m.applyRemoteGlobalRateLimit(rate); err != nil {
			return err
		}
		if m.Settings.Network.GlobalRateLimit == nil {
			m.Settings.Network.GlobalRateLimit = config.DefaultSettings().Network.GlobalRateLimit
		}
		m.Settings.Network.GlobalRateLimit.Value = utils.FormatRateLimit(rate)
		return nil
	}

	if key == "default_download_rate_limit" {
		if utils.IsRateLimitInherit(value) {
			return fmt.Errorf("default rate limit cannot be inherited")
		}
		rate, err := utils.ParseRateLimit(value)
		if err != nil {
			return err
		}
		if err := m.applyRemoteDefaultRateLimit(rate); err != nil {
			return err
		}
		if m.Settings.Network.DefaultDownloadRateLimit == nil {
			m.Settings.Network.DefaultDownloadRateLimit = config.DefaultSettings().Network.DefaultDownloadRateLimit
		}
		m.Settings.Network.DefaultDownloadRateLimit.Value = utils.FormatRateLimit(rate)
		return nil
	}

	if strings.HasPrefix(key, "dl:") {
		dlID := strings.TrimPrefix(key, "dl:")
		if isRateLimitInheritValue(value) {
			if err := m.clearDownloadRateLimit(dlID); err != nil {
				return err
			}
			if d := m.FindDownloadByID(dlID); d != nil {
				d.RateLimit = 0
				d.RateLimitSet = false
			}
			return nil
		}
		rate, err := utils.ParseRateLimit(value)
		if err != nil {
			return err
		}
		if m.Service != nil {
			if err := m.Service.SetRateLimit(dlID, rate); err != nil {
				return err
			}
		}
		if d := m.FindDownloadByID(dlID); d != nil {
			d.RateLimit = rate
			d.RateLimitSet = true
		}
		return nil
	}

	return fmt.Errorf("unknown speed limit key: %s", key)
}

func (m *RootModel) resetSpeedLimitToDefault(key string, defaults *config.Settings) error {
	if key == "global_rate_limit" {
		var rate int64
		if defaults.Network.GlobalRateLimit != nil {
			rate, _ = utils.ParseRateLimitValue(defaults.Network.GlobalRateLimit.Value)
		}
		if err := m.applyRemoteGlobalRateLimit(rate); err != nil {
			return err
		}
		if m.Settings != nil {
			if m.Settings.Network.GlobalRateLimit == nil {
				m.Settings.Network.GlobalRateLimit = config.DefaultSettings().Network.GlobalRateLimit
			}
			if defaults.Network.GlobalRateLimit != nil {
				m.Settings.Network.GlobalRateLimit.Value = defaults.Network.GlobalRateLimit.Value
			}
		}
		return nil
	}
	if key == "default_download_rate_limit" {
		var rate int64
		if defaults.Network.DefaultDownloadRateLimit != nil {
			rate, _ = utils.ParseRateLimitValue(defaults.Network.DefaultDownloadRateLimit.Value)
		}
		if err := m.applyRemoteDefaultRateLimit(rate); err != nil {
			return err
		}
		if m.Settings != nil {
			if m.Settings.Network.DefaultDownloadRateLimit == nil {
				m.Settings.Network.DefaultDownloadRateLimit = config.DefaultSettings().Network.DefaultDownloadRateLimit
			}
			if defaults.Network.DefaultDownloadRateLimit != nil {
				m.Settings.Network.DefaultDownloadRateLimit.Value = defaults.Network.DefaultDownloadRateLimit.Value
			}
		}
		return nil
	}
	if strings.HasPrefix(key, "dl:") {
		dlID := strings.TrimPrefix(key, "dl:")
		if err := m.clearDownloadRateLimit(dlID); err != nil {
			return err
		}
		if d := m.FindDownloadByID(dlID); d != nil {
			d.RateLimit = 0
			d.RateLimitSet = false
		}
		return nil
	}
	return nil
}

// formatDownloadRateLimitValue returns the string representation of the download's rate limit.
func (m RootModel) formatDownloadRateLimitValue(d *DownloadModel) string {
	if d == nil {
		return "inherit"
	}
	if d.RateLimitSet {
		return utils.FormatRateLimit(d.RateLimit)
	}
	defaultRate := int64(0)
	if m.Settings != nil && m.Settings.Network.DefaultDownloadRateLimit != nil {
		if rate, err := utils.ParseRateLimitValue(m.Settings.Network.DefaultDownloadRateLimit.Value); err == nil {
			defaultRate = rate
		}
	}
	return fmt.Sprintf("inherit (%s)", utils.FormatRateLimit(defaultRate))
}

func isRateLimitInheritValue(value string) bool {
	return utils.IsRateLimitInherit(value)
}

func (m *RootModel) clearDownloadRateLimit(downloadID string) error {
	if m.Service == nil {
		return nil
	}
	return m.Service.ClearRateLimit(downloadID)
}

func (m *RootModel) applyRemoteGlobalRateLimit(rate int64) error {
	if m.Service == nil {
		return nil
	}
	setter, ok := m.Service.(interface{ SetGlobalRateLimit(int64) error })
	if !ok {
		return nil
	}
	return setter.SetGlobalRateLimit(rate)
}

func (m *RootModel) applyRemoteDefaultRateLimit(rate int64) error {
	if m.Service == nil {
		return nil
	}
	setter, ok := m.Service.(interface{ SetDefaultRateLimit(int64) error })
	if !ok {
		return nil
	}
	return setter.SetDefaultRateLimit(rate)
}
