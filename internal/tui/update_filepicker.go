package tui

import (
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/utils"
)

func (m *RootModel) handleBatchFileSelection(path string) (tea.Model, tea.Cmd) {
	urls, err := utils.ReadURLsFromFile(path)
	if err != nil {
		m.addLogEntry(LogStyleError.Render("\u2716 Failed to read batch file: " + err.Error()))
		m.resetFilepickerToDirMode()
		m.state = DashboardState
		return m, nil
	}
	m.pendingBatchURLs = urls
	m.inputs[2].SetValue(m.defaultDownloadPath())
	m.batchFilePath = path
	m.resetFilepickerToDirMode()
	m.state = BatchConfirmState
	return m, nil
}

func (m RootModel) updateFilePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.FilePicker.Cancel) {
		switch m.filepickerOrigin {
		case FilePickerOriginTheme:
			m.Settings.General.ThemePath.Value = m.filepickerOriginalPath
			m.filepickerOrigin = FilePickerOriginNone
			m.state = SettingsState
			m.resetFilepickerToDirMode()
			return m, nil
		case FilePickerOriginSettings:
			m.Settings.General.DefaultDownloadDir.Value = m.filepickerOriginalPath
			m.filepickerOrigin = FilePickerOriginNone
			m.state = SettingsState
			m.resetFilepickerToDirMode()
			return m, nil
		case FilePickerOriginExtension:
			m.inputs[2].SetValue(m.filepickerOriginalPath)
			m.focusInput(2)
			m.filepickerOrigin = FilePickerOriginNone
			m.state = ExtensionConfirmationState
			return m, nil

		default:
			m.inputs[2].SetValue(m.filepickerOriginalPath)
			m.focusInput(2)
			m.filepickerOrigin = FilePickerOriginNone
			m.state = InputState
			return m, nil
		}
	}

	// H key to jump to default download directory
	if key.Matches(msg, m.keys.FilePicker.GotoHome) {
		cmd := m.handleFilePickerGotoHome()
		if m.filepickerOrigin == FilePickerOriginTheme {
			m.applyFilePickerMode(true, false)
			m.filepicker.AllowedTypes = []string{".toml"}
		}
		return m, cmd
	}

	// '.' to select current directory - only in directory-picking modes.
	// Skip for FilePickerOriginTheme which is file-only.
	if m.filepickerOrigin != FilePickerOriginTheme && key.Matches(msg, m.keys.FilePicker.UseDir) {
		return m.handleFilePickerSelection(m.filepicker.CurrentDirectory)
	}

	// Jump to file/folder by typing its first letter
	if len(msg.Text) == 1 {
		char := strings.ToLower(msg.Text)
		if (char >= "a" && char <= "z") || (char >= "0" && char <= "9") {
			var handled bool
			m, handled = m.handleFilepickerJump(char)
			if handled {
				return m, nil
			}
		}
	}

	// Pass key to filepicker
	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)

	// Check if a directory was selected
	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		return m.handleFilePickerSelection(path)
	}

	return m, cmd
}

func (m RootModel) updateBatchFilePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.FilePicker.Cancel) {
		// Reset filepicker to directory mode and return
		m.resetFilepickerToDirMode()
		m.state = DashboardState
		return m, nil
	}

	// H key to jump to default download directory
	if key.Matches(msg, m.keys.FilePicker.GotoHome) {
		cmd := m.handleFilePickerGotoHome()
		m.applyFilePickerMode(true, false)
		return m, cmd
	}

	// Jump to file/folder by typing its first letter
	if len(msg.Text) == 1 {
		char := strings.ToLower(msg.Text)
		if (char >= "a" && char <= "z") || (char >= "0" && char <= "9") {
			var handled bool
			m, handled = m.handleFilepickerJump(char)
			if handled {
				return m, nil
			}
		}
	}

	// Pass key to filepicker
	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)

	// Check if a file was selected
	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		return m.handleBatchFileSelection(path)
	}

	return m, cmd
}

func (m RootModel) handleFilepickerJump(char string) (RootModel, bool) {
	if m.filepicker.HighlightedPath() == "" {
		return m, false
	}

	originalModel := m.filepicker
	m.filepicker, _ = m.filepicker.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	var lastPath string
	found := false
	for {
		path := m.filepicker.HighlightedPath()
		if path == "" || path == lastPath {
			break
		}
		lastPath = path
		base := filepath.Base(path)
		if strings.HasPrefix(strings.ToLower(base), char) {
			found = true
			break
		}
		m.filepicker, _ = m.filepicker.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	if !found {
		m.filepicker = originalModel
		m.filepicker, _ = m.filepicker.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})

		lastPath = ""
		for {
			path := m.filepicker.HighlightedPath()
			if path == "" || path == lastPath {
				break
			}
			lastPath = path
			base := filepath.Base(path)
			if strings.HasPrefix(strings.ToLower(base), char) {
				found = true
				break
			}
			if path == originalModel.HighlightedPath() {
				break
			}
			m.filepicker, _ = m.filepicker.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}

		if !found {
			m.filepicker = originalModel
			return m, false
		}
	}
	return m, true
}
