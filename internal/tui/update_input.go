package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/utils"
)

func (m *RootModel) focusInput(i int) {
	m.inputs[m.focusedInput].Blur()
	m.focusedInput = i
	m.inputs[m.focusedInput].Focus()
}

func (m *RootModel) blurAllInputs() {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
}

func (m RootModel) updateInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Input.Esc) {
		m.state = DashboardState
		return m, nil
	}

	if key.Matches(msg, m.keys.Input.Tab) && m.focusedInput == 2 {
		originalPath := m.inputs[2].Value()
		browseDir := strings.TrimSpace(originalPath)
		if browseDir == "" {
			browseDir = config.Resolve[string](m.Settings.General.DefaultDownloadDir)
		}
		if browseDir == "" {
			browseDir = m.PWD
		}
		return m, m.openDirectoryPicker(FilePickerOriginAdd, originalPath, browseDir, false, true)
	}

	if key.Matches(msg, m.keys.Input.Up) && m.focusedInput > 0 {
		next := m.focusedInput - 1
		if m.hideMirrors && next == 1 {
			next = 0
		}
		m.focusInput(next)
		return m, nil
	}

	if key.Matches(msg, m.keys.Input.Down) && m.focusedInput < 3 {
		next := m.focusedInput + 1
		if m.hideMirrors && next == 1 {
			next = 2
		}
		m.focusInput(next)
		return m, nil
	}

	if key.Matches(msg, m.keys.Input.Enter) {
		if m.focusedInput < 3 {
			next := m.focusedInput + 1
			if m.hideMirrors && next == 1 {
				next = 2
			}
			m.focusInput(next)
			return m, nil
		}
		return m.submitInputForm()
	}

	var cmd tea.Cmd
	m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
	return m, cmd
}

func (m RootModel) submitInputForm() (tea.Model, tea.Cmd) {
	inputVal := m.inputs[0].Value()
	if inputVal == "" {
		m.blurAllInputs()
		m.focusedInput = 0
		m.inputs[0].Focus()
		return m, nil
	}

	url, mirrors := parseURLInput(inputVal)

	// Append mirrors from dedicated mirror input
	if mirrorsVal := m.inputs[1].Value(); mirrorsVal != "" {
		for _, part := range strings.Split(mirrorsVal, ",") {
			if cleaned := strings.TrimSpace(part); cleaned != "" {
				mirrors = append(mirrors, cleaned)
			}
		}
	}

	if url == "" {
		m.focusedInput = 0
		m.inputs[0].Focus()
		return m, nil
	}

	pathInput := strings.TrimSpace(m.inputs[2].Value())
	path := pathInput
	isDefaultPath := m.isDefaultDownloadPath(path)
	if path == "" {
		isDefaultPath = true
		path = m.defaultDownloadPath()
	}
	filename := m.inputs[3].Value()

	// Check if pending headers were populated from cURL parser
	headers := m.pendingHeaders
	
	if d := m.checkForDuplicate(url); d != nil {
		m.pendingURL = url
		m.pendingMirrors = mirrors
		m.pendingHeaders = headers
		m.pendingPath = path
		m.pendingIsDefaultPath = isDefaultPath
		m.pendingFilename = filename
		m.pendingWorkers = 0
		m.pendingMinChunkSize = 0
		m.duplicateInfo = d.Filename
		m.state = DuplicateWarningState
		return m, nil
	}

	m.state = DashboardState
	m.inputs[0].SetValue("")
	m.inputs[1].SetValue("")
	m.inputs[2].SetValue(path) // Keep path for next download
	m.inputs[3].SetValue("")
	m.pendingHeaders = nil // Reset

	return m.startDownload(url, mirrors, headers, path, isDefaultPath, filename, "", 0, 0)
}

// parseURLInput splits a comma-separated URL string into a primary URL and mirrors.
func parseURLInput(input string) (url string, mirrors []string) {
	for _, part := range strings.Split(input, ",") {
		cleaned := strings.TrimSpace(part)
		if cleaned == "" {
			continue
		}
		if url == "" {
			url = cleaned
		} else {
			mirrors = append(mirrors, cleaned)
		}
	}
	return
}

func (m RootModel) updateExtensionConfirmation(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Extension.Browse) && m.focusedInput == 2 {
		originalPath := m.inputs[2].Value()
		browseDir := strings.TrimSpace(originalPath)
		if browseDir == "" {
			browseDir = m.PWD
		}
		return m, m.openDirectoryPicker(FilePickerOriginExtension, originalPath, browseDir, false, true)
	}

	if key.Matches(msg, m.keys.Extension.Next) || key.Matches(msg, m.keys.Extension.Prev) {
		m.blurAllInputs()
		if m.focusedInput == 2 {
			m.focusedInput = 3
		} else {
			m.focusedInput = 2
		}
		m.inputs[m.focusedInput].Focus()
		return m, nil
	}

	if key.Matches(msg, m.keys.Extension.Confirm) {
		m.pendingPath = strings.TrimSpace(m.inputs[2].Value())
		m.pendingFilename = strings.TrimSpace(m.inputs[3].Value())
		m.pendingIsDefaultPath = m.isDefaultDownloadPath(m.pendingPath)
		if m.pendingPath == "" {
			m.pendingIsDefaultPath = true
			m.pendingPath = m.defaultDownloadPath()
		}

		if d := m.checkForDuplicate(m.pendingURL); d != nil {
			utils.Debug("Duplicate download detected after confirmation: %s", m.pendingURL)
			m.duplicateInfo = d.Filename
			m.state = DuplicateWarningState
			return m, nil
		}

		m.state = DashboardState
		updated, cmd := m.startDownload(m.pendingURL, m.pendingMirrors, m.pendingHeaders, m.pendingPath, m.pendingIsDefaultPath, m.pendingFilename, "", m.pendingWorkers, m.pendingMinChunkSize)
		nextModel, nextCmd := updated.showNextPendingRequest()
		return nextModel, tea.Batch(cmd, nextCmd)
	}

	if key.Matches(msg, m.keys.Extension.Cancel) {
		m.filepickerOrigin = FilePickerOriginNone
		m.blurAllInputs()
		m.state = DashboardState
		return m.showNextPendingRequest()
	}

	var cmd tea.Cmd
	m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
	return m, cmd
}
