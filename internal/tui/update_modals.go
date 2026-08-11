package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/bugreport"
	"github.com/SurgeDM/Surge/internal/clipboard"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

var openBugReportBrowser = utils.OpenBrowser
var writeBugReportClipboard = clipboard.Write

func (m RootModel) updateSpeedLimits(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.speedLimitsIsEditing {
		if key.Matches(msg, m.keys.Input.Enter) {
			// Save
			value := m.SettingsInput.Value()
			metaList := m.getSpeedLimitsMetadata()
			if m.speedLimitsCursor >= 0 && m.speedLimitsCursor < len(metaList) {
				meta := metaList[m.speedLimitsCursor]

				valueStr := strings.TrimSpace(value)
				if !utils.IsRateLimitInherit(valueStr) {
					if _, err := strconv.ParseFloat(valueStr, 64); err != nil {
						m.speedLimitsError = "Please enter a number (in MB/s)"
						if strings.HasPrefix(meta.Key, "dl:") {
							m.speedLimitsError = "Please enter a number (in MB/s) or -1"
						}
						return m, nil
					}
				}

				oldValues := m.getSpeedLimitsValues()
				oldVal := oldValues[meta.Key]

				if err := m.setSpeedLimitValue(meta.Key, valueStr); err != nil {
					m.speedLimitsError = err.Error()
					return m, nil
				}
				m.speedLimitsError = ""

				newValues := m.getSpeedLimitsValues()
				newVal := newValues[meta.Key]

				oldStr := strings.ReplaceAll(fmt.Sprintf("%v", oldVal), "\u221E", "0 MB/s")
				newStr := strings.ReplaceAll(fmt.Sprintf("%v", newVal), "\u221E", "0 MB/s")

				m.addLogEntry(LogStyleComplete.Render(fmt.Sprintf("\u2714 %s updated: %s \u2192 %s", meta.Label, oldStr, newStr)))
			}
			m.speedLimitsIsEditing = false
			m.SettingsInput.Blur()
			return m, nil
		}
		if key.Matches(msg, m.keys.Input.Esc) {
			m.speedLimitsIsEditing = false
			m.speedLimitsError = ""
			m.SettingsInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.SettingsInput, cmd = m.SettingsInput.Update(msg)
		return m, cmd
	}

	if key.Matches(msg, m.keys.SpeedLimits.Close) {
		m.state = DashboardState
		m.speedLimitsError = ""
		return m, nil
	}

	metaList := m.getSpeedLimitsMetadata()
	if m.speedLimitsCursor >= len(metaList) {
		m.speedLimitsCursor = len(metaList) - 1
	}
	if m.speedLimitsCursor < 0 {
		m.speedLimitsCursor = 0
	}

	if key.Matches(msg, m.keys.SpeedLimits.Up) {
		m.speedLimitsError = ""
		m.speedLimitsCursor--
		if m.speedLimitsCursor < 0 {
			m.speedLimitsCursor = 0
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.SpeedLimits.Down) {
		m.speedLimitsError = ""
		m.speedLimitsCursor++
		if m.speedLimitsCursor >= len(metaList) {
			m.speedLimitsCursor = len(metaList) - 1
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.SpeedLimits.Edit) {
		m.speedLimitsError = ""
		if m.speedLimitsCursor >= 0 && m.speedLimitsCursor < len(metaList) {
			meta := metaList[m.speedLimitsCursor]
			values := m.getSpeedLimitsValues()
			val := values[meta.Key]

			var valStr string
			if vStr, ok := val.(string); ok {
				valStr = vStr
			} else {
				valStr = fmt.Sprintf("%v", val)
			}
			// Strip display-only prefixes before pre-filling the edit input
			if strings.HasPrefix(valStr, "inherit") {
				valStr = "-1"
			} else if bps, err := utils.ParseRateLimitValue(valStr); err == nil {
				if bps == 0 {
					valStr = "0"
				} else {
					valStr = fmt.Sprintf("%v", float64(bps)/1000000.0)
					valStr = strings.TrimRight(strings.TrimRight(valStr, "0"), ".")
					if valStr == "" {
						valStr = "0"
					}
				}
			}

			m.speedLimitsIsEditing = true
			m.SettingsInput.SetValue(valStr)
			m.SettingsInput.Focus()
			m.SettingsInput.CursorEnd()
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.SpeedLimits.Reset) {
		m.speedLimitsError = ""
		if m.speedLimitsCursor >= 0 && m.speedLimitsCursor < len(metaList) {
			meta := metaList[m.speedLimitsCursor]

			oldValues := m.getSpeedLimitsValues()
			oldVal := oldValues[meta.Key]

			if err := m.resetSpeedLimitToDefault(meta.Key, config.DefaultSettings()); err != nil {
				m.addLogEntry(LogStyleError.Render("\u2716 Reset failed: " + err.Error()))
			} else {
				newValues := m.getSpeedLimitsValues()
				newVal := newValues[meta.Key]

				oldStr := strings.ReplaceAll(fmt.Sprintf("%v", oldVal), "\u221E", "0 MB/s")
				newStr := strings.ReplaceAll(fmt.Sprintf("%v", newVal), "\u221E", "0 MB/s")

				m.addLogEntry(LogStyleComplete.Render(fmt.Sprintf("\u2714 %s reset: %s \u2192 %s", meta.Label, oldStr, newStr)))
			}
		}
		return m, nil
	}

	return m, nil
}

func (m RootModel) updateDuplicateWarning(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Duplicate.Continue) {
		// Continue anyway - startDownload handles unique filename generation
		m.state = DashboardState
		updated, cmd := m.startDownload(m.pendingURL, m.pendingMirrors, m.pendingHeaders, m.pendingPath, m.pendingIsDefaultPath, m.pendingFilename, "", m.pendingWorkers, m.pendingMinChunkSize)
		nextModel, nextCmd := updated.showNextPendingRequest()
		return nextModel, tea.Batch(cmd, nextCmd)
	}
	if key.Matches(msg, m.keys.Duplicate.Cancel) {
		// Cancel - don't add
		m.state = DashboardState
		return m.showNextPendingRequest()
	}
	if key.Matches(msg, m.keys.Duplicate.Focus) {
		// Focus existing download - find it and select in list
		for i, d := range m.getFilteredDownloads() {
			if d.URL == m.pendingURL {
				m.list.Select(i)
				break
			}
		}
		m.state = DashboardState
		return m.showNextPendingRequest()
	}
	return m, nil
}

func (m RootModel) updateQuitConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmQuit := func() (tea.Model, tea.Cmd) {
		if m.cancelEnqueue != nil {
			m.cancelEnqueue()
		}
		m.releasePowerInhibitor()
		m.shuttingDown = true
		return m, shutdownCmd(m.Service)
	}
	cancelQuit := func() (tea.Model, tea.Cmd) {
		m.state = DashboardState
		m.quitConfirmFocused = 0
		return m, nil
	}
	if key.Matches(msg, m.keys.QuitConfirm.Left) || key.Matches(msg, m.keys.QuitConfirm.Right) {
		m.quitConfirmFocused = 1 - m.quitConfirmFocused
		return m, nil
	}
	if key.Matches(msg, m.keys.QuitConfirm.Yes) {
		return confirmQuit()
	}
	if key.Matches(msg, m.keys.QuitConfirm.No) {
		return cancelQuit()
	}
	if key.Matches(msg, m.keys.QuitConfirm.Select) {
		if m.quitConfirmFocused == 0 {
			return confirmQuit()
		}
		return cancelQuit()
	}
	if key.Matches(msg, m.keys.QuitConfirm.Cancel) {
		return cancelQuit()
	}
	return m, nil
}

func (m RootModel) updateBatchConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.BatchConfirm.Confirm) {
		// Add all URLs as downloads, skipping duplicates
		path := strings.TrimSpace(m.inputs[2].Value())
		if path == "" {
			path = config.Resolve[string](m.Settings.General.DefaultDownloadDir)
		}
		if path == "" {
			path = "."
		}

		added := 0
		skipped := 0
		var batchCmds []tea.Cmd

		pathChanged := path != m.batchFilePath

		for _, request := range m.pendingBatchRequests {
			requestPath := path
			if !pathChanged && request.Path != "" {
				requestPath = request.Path
			}

			isDefaultPath := m.isDefaultDownloadPath(requestPath)
			if requestPath == "" {
				isDefaultPath = true
				requestPath = m.defaultDownloadPath()
			}
			if m.checkForDuplicate(request.URL) != nil {
				skipped++
				continue
			}
			var cmd tea.Cmd
			m, cmd = m.startDownload(request.URL, request.Mirrors, request.Headers, requestPath, isDefaultPath, request.Filename, request.DownloadID, request.Workers, request.MinChunkSize)
			if cmd != nil {
				batchCmds = append(batchCmds, cmd)
			}
			added++
		}
		for _, url := range m.pendingBatchURLs {
			// Skip duplicate URLs
			if m.checkForDuplicate(url) != nil {
				skipped++
				continue
			}
			var cmd tea.Cmd
			m, cmd = m.startDownload(url, nil, nil, path, true, "", "", 0, 0)
			if cmd != nil {
				batchCmds = append(batchCmds, cmd)
			}
			added++
		}

		if skipped > 0 {
			m.addLogEntry(LogStyleStarted.Render(fmt.Sprintf("\u2b07 Added %d downloads from batch (%d duplicates skipped)", added, skipped)))
		} else {
			m.addLogEntry(LogStyleStarted.Render(fmt.Sprintf("\u2b07 Added %d downloads from batch", added)))
		}
		m.pendingBatchURLs = nil
		m.pendingBatchRequests = nil
		m.batchFilePath = ""
		m.state = DashboardState
		nextModel, nextCmd := m.showNextPendingRequest()
		return nextModel, tea.Batch(append(batchCmds, nextCmd)...)
	}
	if key.Matches(msg, m.keys.BatchConfirm.Cancel) {
		m.pendingBatchURLs = nil
		m.pendingBatchRequests = nil
		m.batchFilePath = ""
		m.state = DashboardState
		return m.showNextPendingRequest()
	}
	var cmd tea.Cmd
	m.inputs[2], cmd = m.inputs[2].Update(msg)
	return m, cmd
}

func (m RootModel) updateURLUpdate(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Input.Esc) {
		m.state = DashboardState
		m.urlUpdateInput.SetValue("")
		m.urlUpdateInput.Blur()
		return m, nil
	}
	if key.Matches(msg, m.keys.Input.Enter) {
		newURL := strings.TrimSpace(m.urlUpdateInput.Value())
		if newURL != "" {
			if d := m.GetSelectedDownload(); d != nil {
				if err := m.Service.UpdateURL(d.ID, newURL); err != nil {
					m.addLogEntry(LogStyleError.Render(fmt.Sprintf("\u2716 Failed to update URL: %s", err.Error())))
				} else {
					m.addLogEntry(LogStyleComplete.Render(fmt.Sprintf("\u2714 URL Updated: %s", d.Filename)))
					d.URL = newURL
				}
			}
		}
		m.state = DashboardState
		m.urlUpdateInput.SetValue("")
		m.urlUpdateInput.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.urlUpdateInput, cmd = m.urlUpdateInput.Update(msg)
	return m, cmd
}

func (m RootModel) updateUpdateAvailable(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Update.Install) {
		m.addLogEntry(LogStyleStarted.Render("\u2b06 Installing Surge update..."))
		m.state = DashboardState
		m.UpdateInfo = nil
		return m, selfUpdateCmd(m.CurrentVersion)
	}
	if key.Matches(msg, m.keys.Update.OpenGitHub) {
		// Open the release page in browser
		if m.UpdateInfo != nil && m.UpdateInfo.ReleaseURL != "" {
			_ = openWithSystem(m.UpdateInfo.ReleaseURL)
		}
		m.state = DashboardState
		m.UpdateInfo = nil
		return m, nil
	}
	if key.Matches(msg, m.keys.Update.IgnoreNow) {
		// Just dismiss the modal
		m.state = DashboardState
		m.UpdateInfo = nil
		return m, nil
	}

	return m, nil
}

func (m RootModel) updateBugReportTarget(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.BugReport.Cancel) {
		m = m.resetBugReportFlow()
		return m, nil
	}

	m, decision, handled := m.handleYesNoSelection(msg)
	if handled {
		switch decision {
		case yesNoYes:
			m.bugReportIncludeSystemInfo = true
			m.bugReportIncludeLatestLog = true
			m.quitConfirmFocused = 0
			m.state = BugReportSystemDetailsState
			return m, nil
		case yesNoNo:
			reportURL := bugreport.ExtensionBugReportURL()
			m = m.tryOpenBugReportURL(reportURL)
			m = m.resetBugReportFlow()
			return m, nil
		case yesNoCancel:
			m = m.resetBugReportFlow()
			return m, nil
		}
	}

	if key.Matches(msg, m.keys.BugReport.Core) {
		m.bugReportIncludeSystemInfo = true
		m.bugReportIncludeLatestLog = true
		m.quitConfirmFocused = 0
		m.state = BugReportSystemDetailsState
		return m, nil
	}

	if key.Matches(msg, m.keys.BugReport.Extension) {
		reportURL := bugreport.ExtensionBugReportURL()
		m = m.tryOpenBugReportURL(reportURL)
		m = m.resetBugReportFlow()
		return m, nil
	}

	return m, nil
}

func (m RootModel) updateBugReportSystemDetails(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m, decision, handled := m.handleYesNoSelection(msg)
	if !handled {
		return m, nil
	}

	switch decision {
	case yesNoNone:
		return m, nil
	case yesNoCancel:
		m = m.resetBugReportFlow()
	case yesNoYes:
		m.bugReportIncludeSystemInfo = true
		m.quitConfirmFocused = 0
		m.state = BugReportLogPathState
	case yesNoNo:
		m.bugReportIncludeSystemInfo = false
		m.quitConfirmFocused = 0
		m.state = BugReportLogPathState
	}

	return m, nil
}

func (m RootModel) updateBugReportLogPath(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m, decision, handled := m.handleYesNoSelection(msg)
	if !handled {
		return m, nil
	}

	switch decision {
	case yesNoNone:
		return m, nil
	case yesNoCancel:
		m = m.resetBugReportFlow()
	case yesNoYes:
		m.bugReportIncludeLatestLog = true
		reportURL := m.buildCoreBugReportURL()
		m = m.tryOpenBugReportURL(reportURL)
		m = m.resetBugReportFlow()
	case yesNoNo:
		m.bugReportIncludeLatestLog = false
		reportURL := m.buildCoreBugReportURL()
		m = m.tryOpenBugReportURL(reportURL)
		m = m.resetBugReportFlow()
	}

	return m, nil
}

type yesNoDecision int

const (
	yesNoNone yesNoDecision = iota
	yesNoCancel
	yesNoYes
	yesNoNo
)

func (m RootModel) handleYesNoSelection(msg tea.KeyPressMsg) (RootModel, yesNoDecision, bool) {
	if key.Matches(msg, m.keys.QuitConfirm.Left) || key.Matches(msg, m.keys.QuitConfirm.Right) {
		m.quitConfirmFocused = 1 - m.quitConfirmFocused
		return m, yesNoNone, true
	}

	if key.Matches(msg, m.keys.QuitConfirm.Yes) {
		return m, yesNoYes, true
	}

	if key.Matches(msg, m.keys.QuitConfirm.No) {
		return m, yesNoNo, true
	}

	if key.Matches(msg, m.keys.QuitConfirm.Select) {
		if m.quitConfirmFocused == 0 {
			return m, yesNoYes, true
		}
		return m, yesNoNo, true
	}

	if key.Matches(msg, m.keys.QuitConfirm.Cancel) {
		return m, yesNoCancel, true
	}

	return m, yesNoNone, false
}

func (m RootModel) buildCoreBugReportURL() string {
	return bugreport.CoreBugReportURL(bugreport.CoreReportOptions{
		Version:              m.CurrentVersion,
		Commit:               m.CurrentCommit,
		IncludeSystemDetails: m.bugReportIncludeSystemInfo,
		IncludeLatestLogPath: m.bugReportIncludeLatestLog,
	})
}

func (m RootModel) tryOpenBugReportURL(reportURL string) RootModel {
	if reportURL == "" {
		m.addLogEntry(LogStyleError.Render("\u2716 Could not open browser. Try running surge bug-report from your terminal instead."))
		return m
	}

	if err := openBugReportBrowser(reportURL); err != nil {
		if err := writeBugReportClipboard(reportURL); err == nil {
			m.addLogEntry(LogStyleError.Render("\u2716 Could not open browser. URL copied to clipboard."))
			return m
		}

		m.addLogEntry(LogStyleError.Render("\u2716 Could not open browser. Try running surge bug-report from your terminal instead."))
		return m
	}

	m.addLogEntry(LogStyleStarted.Render("\U0001F41E Opening browser to file bug report..."))
	return m
}

func (m RootModel) resetBugReportFlow() RootModel {
	m.bugReportIncludeSystemInfo = true
	m.bugReportIncludeLatestLog = true
	m.quitConfirmFocused = 0
	m.state = DashboardState
	return m
}

func (m RootModel) updateRestartConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmRestart := func() (tea.Model, tea.Cmd) {
		if m.cancelEnqueue != nil {
			m.cancelEnqueue()
		}
		m.releasePowerInhibitor()
		m.shuttingDown = true
		m.RestartRequested = true
		return m, shutdownCmd(m.Service)
	}
	cancelRestart := func() (tea.Model, tea.Cmd) {
		m.state = DashboardState
		m.quitConfirmFocused = 0
		m.SettingsBaseline = nil
		return m, nil
	}

	m, decision, handled := m.handleYesNoSelection(msg)
	if !handled {
		return m, nil
	}

	switch decision {
	case yesNoYes:
		return confirmRestart()
	case yesNoNo, yesNoCancel:
		return cancelRestart()
	}

	return m, nil
}

func (m RootModel) updatePurgeConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmPurge := func() (tea.Model, tea.Cmd) {
		targetID := m.purgeTargetID
		m.purgeTargetID = ""
		m.quitConfirmFocused = 0
		m.state = DashboardState

		if m.Service == nil {
			m.addLogEntry(LogStyleError.Render("\u2716 Service unavailable"))
			return m, nil
		}

		filename := ""
		if d := m.FindDownloadByID(targetID); d != nil {
			filename = d.Filename
		}
		if filename == "" {
			if status, err := m.Service.GetStatus(targetID); err == nil && status != nil {
				filename = status.Filename
			}
			if filename == "" {
				if history, err := m.Service.History(); err == nil {
					for _, entry := range history {
						if entry.ID == targetID {
							filename = entry.Filename
							break
						}
					}
				}
			}
		}

		err := m.Service.Purge(targetID)

		m.removeDownloadByID(targetID)
		m.cachedTotalSpeed = m.calcTotalSpeedBps()
		m.UpdateListItems()

		if err != nil {
			if !errors.Is(err, types.ErrNotFound) {
				m.addLogEntry(LogStyleError.Render("\u2716 Purge failed: " + err.Error()))
				return m, nil
			}
		}

		if filename != "" {
			m.addLogEntry(LogStyleStarted.Render("\u2714 Purged: " + filename))
		} else {
			m.addLogEntry(LogStyleStarted.Render("\u2714 Purged download"))
		}

		m, autoCmd := m.refreshAutoShutdown()
		return m, autoCmd
	}

	cancelPurge := func() (tea.Model, tea.Cmd) {
		m.purgeTargetID = ""
		m.quitConfirmFocused = 0
		m.state = DashboardState
		return m, nil
	}

	m, decision, handled := m.handleYesNoSelection(msg)
	if !handled {
		return m, nil
	}

	switch decision {
	case yesNoYes:
		return confirmPurge()
	case yesNoNo, yesNoCancel:
		return cancelPurge()
	}

	return m, nil
}

func (m RootModel) updateRemoveConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmRemove := func() (tea.Model, tea.Cmd) {
		targetID := m.removeTargetID
		m.removeTargetID = ""
		m.quitConfirmFocused = 0
		m.state = DashboardState
		model, deleteCmd := m.deleteDownload(targetID)
		root, ok := model.(RootModel)
		if !ok {
			return model, deleteCmd
		}
		nextModel, nextCmd := root.showNextPendingRequest()
		return nextModel, tea.Batch(deleteCmd, nextCmd)
	}

	cancelRemove := func() (tea.Model, tea.Cmd) {
		m.removeTargetID = ""
		m.quitConfirmFocused = 0
		m.state = DashboardState
		return m.showNextPendingRequest()
	}

	m, decision, handled := m.handleYesNoSelection(msg)
	if !handled {
		return m, nil
	}

	switch decision {
	case yesNoYes:
		return confirmRemove()
	case yesNoNo, yesNoCancel:
		return cancelRemove()
	}

	return m, nil
}

func (m RootModel) deleteDownload(targetID string) (tea.Model, tea.Cmd) {
	if targetID == "" {
		return m, nil
	}

	if m.Service == nil {
		m.addLogEntry(LogStyleError.Render("\u2716 Service unavailable"))
		return m, nil
	}

	if err := m.Service.Delete(targetID); err != nil {
		// If the download is not found, it's already gone from the engine/DB.
		// We still remove it from our local list to avoid it being "stuck".
		if errors.Is(err, types.ErrNotFound) {
			m.removeDownloadByID(targetID)
		} else {
			m.addLogEntry(LogStyleError.Render("\u2716 Delete failed: " + err.Error()))
		}
	} else {
		m.removeDownloadByID(targetID)
	}

	m.cachedTotalSpeed = m.calcTotalSpeedBps()
	m.UpdateListItems()
	m, autoCmd := m.refreshAutoShutdown()
	return m, autoCmd
}
