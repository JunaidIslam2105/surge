package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/clipboard"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/utils"
)

type extensionTokenFlashFadeMsg struct{}

func (m RootModel) updateSettings(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.SettingsBaseline == nil {
		m.snapshotSettings()
	}
	m.normalizeSettingsSelection()

	categories := config.CategoryOrder()
	categoryCount := len(categories)
	if categoryCount == 0 {
		return m, nil
	}

	// Handle editing mode first
	if m.SettingsIsEditing {
		typ := m.getCurrentSettingType()
		if typ == config.TypeCustomCategory || typ == config.TypeCustomCategoryAdd {
			if key.Matches(msg, m.keys.SettingsEditor.Cancel) {
				m.SettingsIsEditing = false
				m.catMgrInputs[m.catMgrEditField].Blur()
				return m, nil
			}
			if key.Matches(msg, m.keys.SettingsEditor.Confirm) {
				// Validate and save
				cat := config.Category{
					Name:        strings.TrimSpace(m.catMgrInputs[0].Value()),
					Description: strings.TrimSpace(m.catMgrInputs[1].Value()),
					Pattern:     strings.TrimSpace(m.catMgrInputs[2].Value()),
					Path:        strings.TrimSpace(m.catMgrInputs[3].Value()),
				}
				if err := cat.Validate(); err != nil {
					m.settingsError = err.Error()
					return m, nil
				}

				if m.catMgrIsNew {
					m.Settings.Categories.Categories = append(m.Settings.Categories.Categories, cat)
					m.SettingsSelectedRow = m.getSettingsCount() - 2 // Select newly added category
				} else {
					settingKey := m.getCurrentSettingKey()
					idx, _ := strconv.Atoi(strings.TrimPrefix(settingKey, "category_"))
					if idx >= 0 && idx < len(m.Settings.Categories.Categories) {
						m.Settings.Categories.Categories[idx] = cat
					} else {
						return m, nil
					}
				}

				m.SettingsIsEditing = false
				m.settingsError = ""
				m.catMgrInputs[m.catMgrEditField].Blur()
				return m, nil
			}

			// Navigation between fields
			s := msg.String()

			if m.catMgrEditField == 3 && key.Matches(msg, m.keys.Settings.Browse) {
				originalPath := strings.TrimSpace(m.catMgrInputs[3].Value())
				browseDir := originalPath
				if browseDir == "" {
					browseDir = m.PWD
				}
				return m, m.openDirectoryPicker(FilePickerOriginCategory, originalPath, browseDir, false, true)
			}

			if s == "tab" || s == "down" {
				m.catMgrInputs[m.catMgrEditField].Blur()
				m.catMgrEditField = (m.catMgrEditField + 1) % 4
				m.catMgrInputs[m.catMgrEditField].Focus()
				return m, nil
			}
			if s == "shift+tab" || s == "up" {
				m.catMgrInputs[m.catMgrEditField].Blur()
				m.catMgrEditField = (m.catMgrEditField - 1 + 4) % 4
				m.catMgrInputs[m.catMgrEditField].Focus()
				return m, nil
			}

			// Pass to active input
			var cmd tea.Cmd
			m.catMgrInputs[m.catMgrEditField], cmd = m.catMgrInputs[m.catMgrEditField].Update(msg)
			if m.settingsError != "" {
				m.settingsError = ""
			}
			return m, cmd
		}

		if key.Matches(msg, m.keys.SettingsEditor.Cancel) {
			// Cancel editing
			m.SettingsIsEditing = false
			m.SettingsInput.Blur()
			return m, nil
		}
		if key.Matches(msg, m.keys.SettingsEditor.Confirm) {
			currentCategory := categories[m.SettingsActiveTab]
			settingKey := m.getCurrentSettingKey()
			val := m.SettingsInput.Value()

			if err := m.setSettingValue(currentCategory, settingKey, val); err != nil {
				m.settingsError = err.Error()
				utils.Debug("Settings Validation Error: %s = %s (%v)", settingKey, val, err)
				return m, nil
			}

			m.SettingsIsEditing = false
			m.settingsError = ""
			m.SettingsInput.Blur()
			return m, nil
		}

		// Pass to text input
		var cmd tea.Cmd
		m.SettingsInput, cmd = m.SettingsInput.Update(msg)
		// Clear error when typing
		if m.settingsError != "" {
			m.settingsError = ""
		}
		return m, cmd
	}

	// Not editing - handle navigation
	if key.Matches(msg, m.keys.Settings.Close) {
		requiresRestart := m.checkRestartRequirement()
		// Save settings and exit
		if err := m.persistSettings(); err != nil {
			m.settingsError = "Failed to save settings: " + err.Error()
			return m, nil
		}
		if requiresRestart {
			m.state = RestartConfirmState
			m.quitConfirmFocused = 0
			return m, nil
		}
		m.state = DashboardState
		m.SettingsBaseline = nil
		return m, nil
	}
	if key.Matches(msg, m.keys.Settings.ReportBug) {
		// Save settings and exit before going to bug report
		if err := m.persistSettings(); err != nil {
			m.settingsError = "Failed to save settings: " + err.Error()
			return m, nil
		}
		m.SettingsBaseline = nil

		m.quitConfirmFocused = 0
		m.bugReportIncludeSystemInfo = true
		m.bugReportIncludeLatestLog = true
		m.state = BugReportTargetState
		return m, nil
	}
	tabBindings := []key.Binding{
		m.keys.Settings.Tab1,
		m.keys.Settings.Tab2,
		m.keys.Settings.Tab3,
		m.keys.Settings.Tab4,
		m.keys.Settings.Tab5,
	}
	for i, binding := range tabBindings {
		if key.Matches(msg, binding) {
			if categoryCount > i {
				m.SettingsActiveTab = i
				m.settingsError = ""
			}
			m.SettingsSelectedRow = 0
			return m, nil
		}
	}

	// Tab Navigation (NextTab/PrevTab) - always switches active tab
	if key.Matches(msg, m.keys.Settings.NextTab) {
		m.SettingsActiveTab = (m.SettingsActiveTab + 1) % categoryCount
		m.SettingsSelectedRow = 0
		m.settingsError = ""
		return m, nil
	}
	if key.Matches(msg, m.keys.Settings.PrevTab) {
		m.SettingsActiveTab = (m.SettingsActiveTab - 1 + categoryCount) % categoryCount
		m.SettingsSelectedRow = 0
		m.settingsError = ""
		return m, nil
	}

	// Up/Down navigation & pane switching between tabs and lists
	if m.SettingsFocusedPane == 0 { // Tabs focused
		if key.Matches(msg, m.keys.Settings.Down) {
			m.SettingsFocusedPane = 1 // Focus settings list
			m.SettingsSelectedRow = 0
			m.settingsError = ""
			return m, nil
		}
	} else { // List focused
		if key.Matches(msg, m.keys.Settings.Up) {
			if m.SettingsSelectedRow > 0 {
				m.SettingsSelectedRow--
				m.settingsError = ""
			} else {
				m.SettingsSelectedRow = m.getSettingsCount() - 1
				m.settingsError = ""
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Settings.Down) {
			maxRow := m.getSettingsCount() - 1
			if m.SettingsSelectedRow < maxRow {
				m.SettingsSelectedRow++
				m.settingsError = ""
			} else {
				m.SettingsSelectedRow = 0
				m.settingsError = ""
			}
			return m, nil
		}
	}

	// Open file browser for default_download_dir or theme_path
	if key.Matches(msg, m.keys.Settings.Browse) {
		if m.SettingsFocusedPane == 0 {
			return m, nil // Can't browse when tabs are focused
		}
		settingKey := m.getCurrentSettingKey()
		switch settingKey {
		case "default_download_dir":
			originalPath := config.Resolve[string](m.Settings.General.DefaultDownloadDir)
			browseDir := originalPath
			if browseDir == "" {
				browseDir = m.PWD
			}
			return m, m.openDirectoryPicker(FilePickerOriginSettings, originalPath, browseDir, false, true)
		case "theme_path":
			originalPath := config.Resolve[string](m.Settings.General.ThemePath)
			browseDir := originalPath
			if browseDir != "" {
				if info, err := os.Stat(browseDir); err == nil && !info.IsDir() {
					browseDir = filepath.Dir(browseDir)
				}
			}
			if browseDir == "" {
				browseDir = config.GetThemesDir()
			}
			if browseDir == "" {
				browseDir = m.PWD
			}
			cmd := m.openDirectoryPicker(FilePickerOriginTheme, originalPath, browseDir, true, false)
			m.filepicker.AllowedTypes = []string{".toml"}
			return m, cmd
		}
		return m, nil
	}

	// Edit / Toggle
	if key.Matches(msg, m.keys.Settings.Edit) {
		if m.SettingsFocusedPane == 0 {
			m.SettingsFocusedPane = 1
			m.SettingsSelectedRow = 0
			m.settingsError = ""
			return m, nil
		}

		settingKey := m.getCurrentSettingKey()
		// Prevent editing ignored settings
		if settingKey == "max_global_connections" {
			return m, nil
		}

		// Special handling for Theme cycling
		if settingKey == "theme" {
			newTheme := (config.Resolve[int](m.Settings.General.Theme) + 1) % 3
			m.Settings.General.Theme.Value = newTheme
			m.ApplyTheme(newTheme, config.Resolve[string](m.Settings.General.ThemePath))
			return m, nil
		}

		// Toggle bool or enter edit mode for other types
		typ := m.getCurrentSettingType()

		// Special actions for custom types
		if typ == "auth_token" {
			token := GetAuthToken()
			if token != "" {
				_ = clipboard.Write(token)
				m.ExtensionTokenCopied = true
				return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
					return extensionTokenFlashFadeMsg{}
				})
			}
			return m, nil
		}

		if typ == "link" {
			currentCategory := categories[m.SettingsActiveTab]
			values := m.getSettingsValues(currentCategory)
			if url, ok := values[settingKey].(string); ok && url != "" {
				_ = utils.OpenBrowser(url)
			}
			return m, nil
		}

		currentCategory := categories[m.SettingsActiveTab]
		switch typ {
		case "bool":
			if err := m.setSettingValue(currentCategory, settingKey, ""); err != nil {
				m.settingsError = err.Error()
			}
		case config.TypeCustomCategory, config.TypeCustomCategoryAdd:
			m.SettingsIsEditing = true
			m.catMgrEditField = 0 // start at Name

			if typ == config.TypeCustomCategoryAdd {
				m.catMgrIsNew = true
				m.catMgrInputs[0].SetValue("")
				m.catMgrInputs[1].SetValue("")
				m.catMgrInputs[2].SetValue("")
				m.catMgrInputs[3].SetValue("")
			} else {
				m.catMgrIsNew = false
				if strings.HasPrefix(settingKey, "category_") {
					idx, _ := strconv.Atoi(strings.TrimPrefix(settingKey, "category_"))
					if idx >= 0 && idx < len(m.Settings.Categories.Categories) {
						cat := m.Settings.Categories.Categories[idx]
						m.catMgrInputs[0].SetValue(cat.Name)
						m.catMgrInputs[1].SetValue(cat.Description)
						m.catMgrInputs[2].SetValue(cat.Pattern)
						m.catMgrInputs[3].SetValue(cat.Path)
					}
				}
			}
			m.catMgrInputs[0].Focus()
		default:
			// Enter edit mode
			m.SettingsIsEditing = true
			// Pre-fill with current value (without units)
			values := m.getSettingsValues(currentCategory)
			unit := config.UnitNone
			if meta := m.getCurrentSettingMeta(); meta != nil {
				unit = meta.Unit
			}
			m.SettingsInput.SetValue(formatSettingValueForEdit(values[settingKey], typ, unit, settingKey, false))
			m.updateSettingsInputWidthForViewport()
			m.SettingsInput.Focus()
		}
		return m, nil
	}

	// Reset
	if key.Matches(msg, m.keys.Settings.Reset) {
		if m.SettingsFocusedPane == 0 {
			return m, nil // Can't reset when tabs are focused
		}
		settingKey := m.getCurrentSettingKey()
		if settingKey == "max_global_connections" {
			return m, nil
		}

		// Categories tab \u2192 'Manage Categories' selected \u2192 confirm full reset
		// If deleting a custom category
		typ := m.getCurrentSettingType()
		if typ == config.TypeCustomCategory && strings.HasPrefix(settingKey, "category_") {
			idx, _ := strconv.Atoi(strings.TrimPrefix(settingKey, "category_"))
			if idx >= 0 && idx < len(m.Settings.Categories.Categories) {
				m.Settings.Categories.Categories = append(
					m.Settings.Categories.Categories[:idx],
					m.Settings.Categories.Categories[idx+1:]...,
				)
				if m.SettingsSelectedRow >= m.getSettingsCount() {
					m.SettingsSelectedRow = m.getSettingsCount() - 1
				}
			}
			return m, nil
		}

		defaults := config.DefaultSettings()
		currentCategory := categories[m.SettingsActiveTab]
		if err := m.resetSettingToDefault(currentCategory, settingKey, defaults); err != nil {
			m.settingsError = err.Error()
			return m, nil
		}
		if settingKey == "theme" || settingKey == "theme_path" {
			m.ApplyTheme(config.Resolve[int](m.Settings.General.Theme), config.Resolve[string](m.Settings.General.ThemePath))
		}
		return m, nil
	}

	return m, nil
}
