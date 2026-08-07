package tui

import (
	"os"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/utils"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

func (m RootModel) updatePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.state == DashboardState && m.searchActive {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.searchQuery = m.searchInput.Value()
		m.UpdateListItems()
		return m, cmd
	}

	switch m.state {
	case InputState, ExtensionConfirmationState:
		var cmd tea.Cmd
		m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
		return m, cmd
	case URLUpdateState:
		var cmd tea.Cmd
		m.urlUpdateInput, cmd = m.urlUpdateInput.Update(msg)
		return m, cmd
	case SpeedLimitsState:
		if m.speedLimitsIsEditing {
			var cmd tea.Cmd
			m.SettingsInput, cmd = m.SettingsInput.Update(msg)
			return m, cmd
		}
		return m, nil
	case SettingsState:
		if m.SettingsIsEditing {
			typ := m.getCurrentSettingType()
			if typ == config.TypeCustomCategory || typ == config.TypeCustomCategoryAdd {
				var cmd tea.Cmd
				m.catMgrInputs[m.catMgrEditField], cmd = m.catMgrInputs[m.catMgrEditField].Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.SettingsInput, cmd = m.SettingsInput.Update(msg)
			return m, cmd
		}
		return m, nil

	default:
		return m, nil
	}
}

// Update handles messages and updates the model
func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Dynamically reload keymap.json on the fly if it is updated on disk
	if time.Since(m.lastConfigCheckTime) > 1*time.Second {
		m.lastConfigCheckTime = time.Now()
		if info, err := os.Stat(config.GetKeyMapConfigPath()); err == nil {
			if info.ModTime().After(m.lastKeyMapModTime) {
				preLoadModTime := info.ModTime()
				if newKeys, err := config.LoadKeyMap(); err == nil && newKeys != nil {
					m.keys = newKeys
					if postInfo, postErr := os.Stat(config.GetKeyMapConfigPath()); postErr == nil {
						m.lastKeyMapModTime = postInfo.ModTime()
					} else {
						m.lastKeyMapModTime = preLoadModTime
					}
					utils.Debug("TUI: dynamically reloaded keymap.json from disk")
				}
			}
		}
	}

	if m.Settings == nil {
		m.Settings = config.DefaultSettings()
	}

	if m.keys == nil {
		m.keys = config.DefaultKeyMap()
	}

	if m.shuttingDown {
		switch msg := msg.(type) {
		case shutdownCompleteMsg:
			if msg.err != nil {
				utils.Debug("TUI shutdown error: %v", msg.err)
			}
			return m, tea.Quit
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			return m, nil
		default:
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.lastResizeTime = time.Now()

		if m.state == SettingsState {
			m.normalizeSettingsSelection()
			if m.SettingsIsEditing {
				m.updateSettingsInputWidthForViewport()
			}
		}

		// Sync layout calculations with DashboardLayout to set dimensions correctly
		layout := CalculateDashboardLayout(msg.Width, msg.Height)

		// Update viewport width and re-wrap content to new bounds
		logHeight := layout.HeaderHeight - BoxStyle.GetVerticalFrameSize()
		if logHeight < 1 {
			logHeight = 1
		}
		m.logViewport.SetWidth(layout.LogWidth - BoxStyle.GetHorizontalFrameSize())
		m.logViewport.SetHeight(logHeight)
		m.refreshLogViewportContent()

		// Setup download list dimensions
		listInnerPadding := lipgloss.NewStyle().Padding(1, 2)
		m.list.SetSize(
			layout.ListWidth-listInnerPadding.GetHorizontalFrameSize()-BoxStyle.GetHorizontalFrameSize(),
			layout.ListHeight-layout.TabBarHeight-BoxStyle.GetVerticalFrameSize()-listInnerPadding.GetVerticalFrameSize(),
		)

		// Update list based on active tab
		m.UpdateListItems()

		// Update filepicker height (Account for 2 borders, 1 title, 1 path line, 2 padding, 2 help)
		const pickerChromeHeight = 8
		_, fpHeight := GetDynamicModalDimensions(m.width, m.height, 60, 10, 90, 20)
		m.filepicker.SetHeight(fpHeight - pickerChromeHeight)

		return m, nil

	case extensionTokenFlashFadeMsg:
		m.ExtensionTokenCopied = false
		return m, nil

	case UpdateCheckResultMsg:
		if msg.Info != nil && msg.Info.UpdateAvailable {
			m.UpdateInfo = msg.Info
			m.state = UpdateAvailableState
		}
		return m, nil

	case shutdownCompleteMsg:
		if msg.err != nil {
			utils.Debug("TUI shutdown error: %v", msg.err)
		}
		return m, tea.Quit

	case tea.PasteMsg:
		return m.updatePaste(msg)

	// Handle filepicker messages for all message types when in FilePickerState
	default:
		var cmds []tea.Cmd
		for _, d := range m.downloads {
			newProgress, cmd := d.progress.Update(msg)
			d.progress = newProgress
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.state == FilePickerState {
			var cmd tea.Cmd
			m.filepicker, cmd = m.filepicker.Update(msg)
			if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
				model, selCmd := m.handleFilePickerSelection(path)
				return model, tea.Batch(append(cmds, selCmd)...)
			}
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		if m.state == BatchFilePickerState {
			var cmd tea.Cmd
			m.filepicker, cmd = m.filepicker.Update(msg)
			if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
				model, selCmd := m.handleBatchFileSelection(path)
				return model, tea.Batch(append(cmds, selCmd)...)
			}
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
		model, cmd := m.updateEvents(msg)
		cmds = append(cmds, cmd)
		return model, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		switch m.state {
		case DashboardState:
			return m.updateDashboard(msg)

		case DetailState:
			if msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
				m.state = DashboardState
				return m, nil
			}

		case InputState:
			return m.updateInput(msg)

		case FilePickerState:
			return m.updateFilePicker(msg)

		case DuplicateWarningState:
			return m.updateDuplicateWarning(msg)

		case ExtensionConfirmationState:
			return m.updateExtensionConfirmation(msg)

		case BatchFilePickerState:
			return m.updateBatchFilePicker(msg)

		case QuitConfirmState:
			return m.updateQuitConfirm(msg)

		case RestartConfirmState:
			return m.updateRestartConfirm(msg)

		case BatchConfirmState:
			return m.updateBatchConfirm(msg)

		case SpeedLimitsState:
			return m.updateSpeedLimits(msg)

		case SettingsState:
			return m.updateSettings(msg)

		case UpdateAvailableState:
			return m.updateUpdateAvailable(msg)

		case URLUpdateState:
			return m.updateURLUpdate(msg)

		case HelpModalState:
			if msg.String() == "esc" {
				m.state = DashboardState
				return m, nil
			}
			return m, nil

		case BugReportTargetState:
			return m.updateBugReportTarget(msg)

		case BugReportSystemDetailsState:
			return m.updateBugReportSystemDetails(msg)

		case BugReportLogPathState:
			return m.updateBugReportLogPath(msg)

		case PurgeConfirmState:
			return m.updatePurgeConfirm(msg)

		case RemoveConfirmState:
			return m.updateRemoveConfirm(msg)

		default:
			return m, nil
		}
	}

	return m, nil
}
