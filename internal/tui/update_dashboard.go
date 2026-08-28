package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/clipboard"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func (m RootModel) updateDashboard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Handle search input FIRST when active (intercepts ALL keys)
	if m.searchActive {
		switch msg.String() {
		case "esc", "Q":
			// Cancel search and clear query
			m.searchActive = false
			m.searchInput.Blur()
			m.searchQuery = ""
			m.searchInput.SetValue("")
			m.UpdateListItems()
			return m, nil
		case "enter":
			// Commit search (keep filter applied)
			m.searchActive = false
			m.searchInput.Blur()
			return m, nil
		default:
			// All other keys go to search input
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.searchQuery = m.searchInput.Value()
			m.UpdateListItems()
			return m, cmd
		}
	}

	// Focus search.
	if msg.String() == "/" || key.Matches(msg, m.keys.Dashboard.Search) {
		m.searchActive = true
		m.searchInput.Focus()
		return m, nil
	}

	// Tab switching
	pinnedGuard := func() bool {
		if m.pinnedTab != -1 {
			m.addLogEntry(LogStyleError.Render("\u25c6 Tab is pinned \u2014 press " + m.keys.Dashboard.PinTab.Help().Key + " to unpin"))
			return true
		}
		return false
	}

	switchTab := func(tab int) (tea.Model, tea.Cmd) {
		m.activeTab = tab
		m.ManualTabSwitch = true
		m.UpdateListItems()
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.TabQueued) {
		if pinnedGuard() {
			return m, nil
		}
		return switchTab(TabQueued)
	}
	if key.Matches(msg, m.keys.Dashboard.TabActive) {
		if pinnedGuard() {
			return m, nil
		}
		return switchTab(TabActive)
	}
	if key.Matches(msg, m.keys.Dashboard.TabDone) {
		if pinnedGuard() {
			return m, nil
		}
		return switchTab(TabDone)
	}
	// Quit
	if key.Matches(msg, m.keys.Dashboard.Quit, m.keys.Dashboard.ForceQuit) {
		m.state = QuitConfirmState
		m.quitConfirmFocused = 0
		return m, nil
	}

	// Add download
	if key.Matches(msg, m.keys.Dashboard.Add) {
		m.state = InputState
		m.hideMirrors = false
		m.focusedInput = 0
		m.inputs[0].Focus()
		// Use default download dir from settings
		defaultDir := config.Resolve[string](m.Settings.General.DefaultDownloadDir)
		if defaultDir == "" {
			defaultDir = "."
		}
		m.inputs[2].SetValue(defaultDir)
		m.inputs[2].Blur()
		m.inputs[3].SetValue("")
		m.inputs[3].Blur()
		m.inputs[1].SetValue("") // Clear mirrors
		m.inputs[1].Blur()
		m.pendingHeaders = nil // clear stale headers

		url := ""
		if config.Resolve[bool](m.Settings.General.ClipboardMonitor) {
			url = clipboard.ReadURL()
		}
		m.inputs[0].SetValue(url)
		return m, nil
	}

	// Add from clipboard
	if key.Matches(msg, m.keys.Dashboard.AddFromClipboard) {
		text, err := clipboard.Read()
		if err == nil && text != "" {
			return m.handleClipboardPaste(text)
		}
		return m, nil
	}

	// Next / Prev Tab
	if key.Matches(msg, m.keys.Dashboard.NextTab) {
		if pinnedGuard() {
			return m, nil
		}
		m.activeTab = (m.activeTab + 1) % 3
		m.ManualTabSwitch = true
		m.UpdateListItems()
		return m, nil
	}
	if key.Matches(msg, m.keys.Dashboard.PrevTab) {
		if pinnedGuard() {
			return m, nil
		}
		m.activeTab = (m.activeTab + 2) % 3 // +2 mod 3 = prev
		m.ManualTabSwitch = true
		m.UpdateListItems()
		return m, nil
	}

	// Delete download
	if key.Matches(msg, m.keys.Dashboard.Delete) {
		if d := m.GetSelectedDownload(); d != nil {
			if !d.done || d.err != nil {
				m.removeTargetID = d.ID
				m.quitConfirmFocused = 1 // default focus on "Cancel"
				m.state = RemoveConfirmState
				return m, nil
			}
			return m.deleteDownload(d.ID)
		}
	}

	// Delete download + file from disk (purge)
	if key.Matches(msg, m.keys.Dashboard.PurgeFile) {
		if d := m.GetSelectedDownload(); d != nil {
			if !d.done || d.err != nil {
				m.addLogEntry(LogStyleError.Render("\u2716 Purge is only for successfully completed downloads"))
				return m, nil
			}
			if m.Service == nil {
				m.addLogEntry(LogStyleError.Render("\u2716 Service unavailable"))
				return m, nil
			}
			m.purgeTargetID = d.ID
			m.quitConfirmFocused = 1 // default focus on "Cancel"
			m.state = PurgeConfirmState
			return m, nil
		}
	}

	// Pause/Resume toggle
	if key.Matches(msg, m.keys.Dashboard.Pause) {
		if d := m.GetSelectedDownload(); d != nil {
			if m.Service == nil {
				m.addLogEntry(LogStyleError.Render("\u2716 Service unavailable"))
				return m, nil
			}
			if !d.done {
				if d.paused {
					// Resume
					d.paused = false
					d.resuming = true
					if err := m.Service.Resume(d.ID); err != nil {
						m.addLogEntry(LogStyleError.Render("\u2716 Resume failed: " + err.Error()))
						d.paused = true // Revert
						d.resuming = false
					}
				} else {
					// Pause
					if err := m.Service.Pause(d.ID); err != nil {
						m.addLogEntry(LogStyleError.Render("\u2716 Pause failed: " + err.Error()))
					} else {
						d.resuming = false
						d.pausing = true
					}
				}
			}
		}
		m.UpdateListItems()
		return m, nil
	}

	// Open file
	if key.Matches(msg, m.keys.Dashboard.OpenFile) {
		if d := m.GetSelectedDownload(); d != nil {
			canOpen := d.done || (config.Resolve[bool](m.Settings.Network.SequentialDownload) && !d.paused && d.Downloaded > 0)
			if canOpen && d.Destination != "" {
				filePath := d.Destination
				if !d.done {
					filePath = d.Destination + types.IncompleteSuffix
				}
				_ = openWithSystem(filePath)
			}
		}
		return m, nil
	}

	// Open folder
	if key.Matches(msg, m.keys.Dashboard.OpenFolder) {
		if d := m.GetSelectedDownload(); d != nil {
			if d.Destination != "" {
				filePath := d.Destination
				if !d.done {
					filePath = d.Destination + types.IncompleteSuffix
				}
				_ = utils.OpenContainingFolder(filePath)
			}
		}
		return m, nil
	}

	// Refresh URL
	if key.Matches(msg, m.keys.Dashboard.Refresh) {
		if d := m.GetSelectedDownload(); d != nil {
			if m.Service == nil {
				m.addLogEntry(LogStyleError.Render("\u2716 Service unavailable"))
				return m, nil
			}
			// Only allow refresh if download is paused or errored
			if d.paused || d.err != nil {
				m.state = URLUpdateState
				m.urlUpdateInput.SetValue(d.URL)
				m.urlUpdateInput.Focus()
			} else {
				m.addLogEntry(LogStyleError.Render("\u2716 Pause download before refreshing URL"))
			}
		}
		return m, nil
	}

	// Other keys...
	if key.Matches(msg, m.keys.Dashboard.Log) {
		m.logFocused = !m.logFocused
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.ToggleHelp) {
		m.state = HelpModalState
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.ReportBug) {
		m.quitConfirmFocused = 0
		m.bugReportIncludeSystemInfo = true
		m.bugReportIncludeLatestLog = true
		m.state = BugReportTargetState
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.Settings) {
		m.snapshotSettings()
		m.state = SettingsState
		m.SettingsActiveTab = 0
		m.SettingsSelectedRow = 0
		m.SettingsIsEditing = false
		m.SettingsFocusedPane = 1
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.SpeedLimits) {
		m.snapshotSettings()
		m.state = SpeedLimitsState
		m.speedLimitsCursor = 0
		m.speedLimitsIsEditing = false
		m.SettingsInput.SetValue("")
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.CategoryFilter) {
		if !config.Resolve[bool](m.Settings.Categories.CategoryEnabled) || len(m.Settings.Categories.Categories) == 0 {
			if m.categoryFilter != "" {
				m.categoryFilter = ""
				m.addLogEntry(LogStyleStarted.Render("\U0001F4C2 Filter: All"))
				m.UpdateListItems()
				return m, nil
			}
			m.addLogEntry(LogStyleError.Render("\u2716 Enable categories in Settings first"))
			return m, nil
		}
		names := config.CategoryNames(m.Settings.Categories.Categories)
		cycle := append([]string{""}, names...)
		cycle = append(cycle, "Uncategorized")
		current := 0
		for i, n := range cycle {
			if n == m.categoryFilter {
				current = i
				break
			}
		}
		m.categoryFilter = cycle[(current+1)%len(cycle)]
		label := m.categoryFilter
		if label == "" {
			label = "All"
		}
		m.addLogEntry(LogStyleStarted.Render("\U0001F4C2 Filter: " + label))
		m.UpdateListItems()
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.PinTab) {
		if m.pinnedTab == m.activeTab {
			m.pinnedTab = -1
			m.addLogEntry(LogStyleStarted.Render("\u25c6 Tab Unpinned"))
		} else {
			m.pinnedTab = m.activeTab
			var tabName string
			switch m.activeTab {
			case TabActive:
				tabName = "Active"
			case TabDone:
				tabName = "Done"
			default:
				tabName = "Queued"
			}
			m.addLogEntry(LogStyleStarted.Render("\u25c6 Tab Pinned: " + tabName))
		}
		return m, nil
	}

	if key.Matches(msg, m.keys.Dashboard.BatchImport) {
		m.state = BatchFilePickerState
		m.filepicker = newFilepicker(m.PWD)
		m.filepicker.FileAllowed = true
		m.filepicker.DirAllowed = false
		return m, m.filepicker.Init()
	}

	if m.logFocused {
		if key.Matches(msg, m.keys.Dashboard.LogClose) {
			m.logFocused = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Dashboard.LogDown) {
			m.logViewport.ScrollDown(1)
			return m, nil
		}
		if key.Matches(msg, m.keys.Dashboard.LogUp) {
			m.logViewport.ScrollUp(1)
			return m, nil
		}
		if key.Matches(msg, m.keys.Dashboard.LogTop) {
			m.logViewport.GotoTop()
			return m, nil
		}
		if key.Matches(msg, m.keys.Dashboard.LogBottom) {
			m.logViewport.GotoBottom()
			return m, nil
		}
		return m, nil
	}

	// Block bare ESC from propagating (only quit via ctrl+q/ctrl+c)
	if msg.String() == "esc" {
		return m, nil
	}

	// Pass messages to the list for navigation/filtering
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m RootModel) handleClipboardPaste(text string) (tea.Model, tea.Cmd) {
	url, headers := clipboard.ParseCurl(text)
	if url == "" {
		if strings.HasPrefix(strings.TrimSpace(text), "http") {
			url = strings.TrimSpace(text)
		} else {
			url = clipboard.ReadURL()
		}
		if url == "" {
			return m, nil
		}
		m.pendingHeaders = nil // clear stale headers
		m.state = InputState
		m.hideMirrors = false
		m.focusedInput = 0
		m.inputs[0].Focus()

		defaultDir := config.Resolve[string](m.Settings.General.DefaultDownloadDir)
		if defaultDir == "" {
			defaultDir = "."
		}
		m.inputs[2].SetValue(defaultDir)
		m.inputs[2].Blur()
		m.inputs[3].SetValue("")
		m.inputs[3].Blur()
		m.inputs[1].SetValue("") // Clear mirrors
		m.inputs[1].Blur()
		m.inputs[0].SetValue(url)
		return m, nil
	} else {
		defaultDir := config.Resolve[string](m.Settings.General.DefaultDownloadDir)
		if defaultDir == "" {
			defaultDir = "."
		}

		m.pendingHeaders = headers
		m.state = InputState
		m.hideMirrors = true
		m.focusedInput = 0
		m.inputs[0].Focus()

		m.inputs[2].SetValue(defaultDir)
		m.inputs[2].Blur()
		m.inputs[3].SetValue("")
		m.inputs[3].Blur()
		m.inputs[1].SetValue("") // Clear mirrors
		m.inputs[1].Blur()
		m.inputs[0].SetValue(url)

		return m, nil
	}
}
