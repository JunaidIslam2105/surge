package tui

import (
	engineprogress "github.com/SurgeDM/Surge/internal/progress"

	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func stateProgress(state interface{}) *engineprogress.DownloadProgress {
	if state == nil {
		return nil
	}
	if dr, ok := state.(*types.DownloadRecord); ok {
		return engineprogress.CfgProgress(dr)
	}
	return nil
}

func (m RootModel) updateEvents(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ev, ok := msg.(types.DownloadEvent); ok {
		model, cmd := m.handleDownloadEvent(ev)
		root, ok := model.(RootModel)
		if !ok {
			return model, cmd
		}
		root, autoCmd := root.refreshAutoShutdown()
		return root, tea.Batch(cmd, autoCmd)
	}

	switch msg := msg.(type) {
	case autoShutdownCheckMsg:
		return m.refreshAutoShutdown()

	case autoShutdownResultMsg:
		return m.handleAutoShutdownResult(msg.err)

	case autoShutdownRetryMsg:
		return m.handleAutoShutdownRetry()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		needsSpinner := false
		for _, d := range m.downloads {
			if d.pausing || d.resuming || components.DetermineStatus(d.done, d.paused, d.err != nil, d.started, d.resuming) == components.StatusQueued {
				needsSpinner = true
				break
			}
		}
		if needsSpinner {
			m.UpdateListItems()
			return m, cmd
		}
		return m, nil

	case resumeResultMsg:
		if msg.err != nil {
			m.addLogEntry(LogStyleError.Render(fmt.Sprintf("\u2716 Auto-resume failed for %s: %v", msg.id, msg.err)))
			return m, nil
		}
		if d := m.FindDownloadByID(msg.id); d != nil {
			d.paused = false
			d.pausing = false
			d.resuming = true
		}
		return m, m.spinner.Tick

	case enqueueSuccessMsg:
		if msg.tempID != "" && msg.tempID != msg.id {
			temp := m.FindDownloadByID(msg.tempID)
			real := m.FindDownloadByID(msg.id)
			if temp != nil && real != nil && temp != real {
				if real.URL == "" {
					real.URL = temp.URL
				}
				if real.Filename == "" {
					real.Filename = msg.filename
					if real.Filename == "" {
						real.Filename = temp.Filename
					}
					real.FilenameLower = strings.ToLower(real.Filename)
				}
				if real.Destination == "" {
					real.Destination = temp.Destination
				}

				if m.SelectedDownloadID == msg.tempID || (m.GetSelectedDownload() != nil && m.GetSelectedDownload().ID == msg.tempID) {
					m.SelectedDownloadID = msg.id
				}
				_ = m.removeDownloadByID(msg.tempID)
			} else if temp != nil {
				if m.SelectedDownloadID == msg.tempID || (m.GetSelectedDownload() != nil && m.GetSelectedDownload().ID == msg.tempID) {
					m.SelectedDownloadID = msg.id
				}
				temp.ID = msg.id
			}
		}
		m.UpdateListItems()
		m, autoCmd := m.refreshAutoShutdown()
		return m, autoCmd

	case enqueueErrorMsg:
		if msg.tempID != "" {
			if d := m.FindDownloadByID(msg.tempID); d != nil {
				d.err = msg.err
				d.done = true
				d.paused = false
				d.pausing = false
				d.resuming = false
				d.Speed = 0
				d.Connections = 0
				if d.FilenameLower == "" {
					d.FilenameLower = strings.ToLower(d.Filename)
				}
			} else {
				failed := NewDownloadModel(msg.tempID, "", "", 0)
				failed.err = msg.err
				failed.done = true
				m.downloads = append(m.downloads, failed)
			}
			m.UpdateListItems()
		}
		m.addLogEntry(LogStyleError.Render("\u2716 Failed to enqueue download: " + msg.err.Error()))
		m, autoCmd := m.refreshAutoShutdown()
		return m, autoCmd

	case startupConfigWarningMsg:
		for _, w := range msg {
			if w != "" {
				m.addLogEntry(LogStyleError.Render("\u26a0 " + w))
			}
		}
		return m, nil
	}

	return m, nil
}

func (m RootModel) handleDownloadEvent(msg types.DownloadEvent) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case types.EventRequest:
		return m.handleDownloadRequestMsg(msg, true)

	case types.EventBatchRequest:
		return m.handleBatchDownloadRequestMsg(msg, true)

	case types.EventStarted:
		found := false
		if d := m.FindDownloadByID(msg.DownloadID); d != nil {
			d.Filename = msg.Filename
			d.FilenameLower = strings.ToLower(msg.Filename)
			d.Total = msg.Total
			d.Destination = msg.DestPath
			d.RateLimit = msg.RateLimit
			d.RateLimitSet = msg.RateLimitSet
			d.StartTime = time.Now()
			d.paused = false
			d.pausing = false
			d.hasEtaSpeed = false
			d.etaSpeed = 0
			d.lastETA = 0
			var progressCmd tea.Cmd
			if d.Total > 0 {
				progressCmd = d.progress.SetPercent(0)
			}
			if d.state == nil && msg.State != nil {
				d.state = stateProgress(msg.State)
			}
			if d.state != nil {
				d.state.SetTotalSize(msg.Total)
			}
			d.started = true
			m.SelectedDownloadID = msg.DownloadID
			m.UpdateListItems()
			m.addLogEntry(LogStyleStarted.Render("\u2b07 Started: " + msg.Filename))
			return m, tea.Batch(progressCmd, m.spinner.Tick)
		}

		if !found {
			newDownload := NewDownloadModel(msg.DownloadID, msg.URL, msg.Filename, msg.Total)
			newDownload.Destination = msg.DestPath
			newDownload.RateLimit = msg.RateLimit
			newDownload.RateLimitSet = msg.RateLimitSet
			if msg.State != nil {
				newDownload.state = stateProgress(msg.State)
			}
			newDownload.started = true
			m.downloads = append(m.downloads, newDownload)
			m.SelectedDownloadID = msg.DownloadID
			m.UpdateListItems()
			m.addLogEntry(LogStyleStarted.Render("\u2b07 Started: " + msg.Filename))
			return m, m.spinner.Tick
		}

	case types.EventProgress:
		cmd := m.processProgressMsg(msg)
		return m, cmd

	case types.EventBatchProgress:
		var cmds []tea.Cmd
		for _, bm := range msg.BatchEvents {
			cmds = append(cmds, m.processProgressMsg(bm))
		}
		m.cachedTotalSpeed = m.calcTotalSpeedBps()
		return m, tea.Batch(cmds...)

	case types.EventComplete:
		var cmds []tea.Cmd
		if d := m.FindDownloadByID(msg.DownloadID); d != nil {
			if !d.done {
				d.Total = msg.Total
				d.Downloaded = d.Total
				d.Elapsed = msg.Elapsed
				d.Speed = msg.AvgSpeed
				d.done = true
				cmds = append(cmds, d.progress.SetPercent(1.0))

				speed := d.Speed
				if msg.Elapsed.Seconds() >= 1 {
					speed = float64(d.Total) / float64(int(msg.Elapsed.Seconds()))
				} else if msg.Elapsed.Seconds() > 0 {
					speed = float64(d.Total) / msg.Elapsed.Seconds()
				}
				m.addLogEntry(LogStyleComplete.Render(fmt.Sprintf("\u2714 Done: %s (%.2f MiB/s)", d.Filename, speed/float64(utils.MiB))))
			}
		}
		m.cachedTotalSpeed = m.calcTotalSpeedBps()
		m.UpdateListItems()
		return m, tea.Batch(cmds...)

	case types.EventError:
		found := false
		if d := m.FindDownloadByID(msg.DownloadID); d != nil {
			d.err = msg.Err
			d.done = true
			m.addLogEntry(LogStyleError.Render("\u2716 Error: " + d.Filename))
			found = true
		}
		if !found {
			newDownload := NewDownloadModel(msg.DownloadID, "", msg.Filename, 0)
			newDownload.err = msg.Err
			newDownload.done = true
			m.downloads = append(m.downloads, newDownload)
			m.addLogEntry(LogStyleError.Render("\u2716 Error: " + msg.Filename))
		}
		m.cachedTotalSpeed = m.calcTotalSpeedBps()
		m.UpdateListItems()
		return m, nil

	case types.EventPaused:
		if d := m.FindDownloadByID(msg.DownloadID); d != nil {
			d.paused = true
			d.pausing = false
			d.resuming = false
			d.Downloaded = msg.Downloaded
			d.RateLimit = msg.RateLimit
			d.RateLimitSet = msg.RateLimitSet
			d.Speed = 0
			m.addLogEntry(LogStylePaused.Render("\u23f8 Paused: " + d.Filename))
		}
		m.cachedTotalSpeed = m.calcTotalSpeedBps()
		m.UpdateListItems()
		return m, nil

	case types.EventResumed:
		if d := m.FindDownloadByID(msg.DownloadID); d != nil {
			d.paused = false
			d.pausing = false
			d.resuming = true
			d.hasEtaSpeed = false
			d.etaSpeed = 0
			d.lastETA = 0
			m.addLogEntry(LogStyleStarted.Render("\u25b6 Resumed: " + d.Filename))
		}
		m.UpdateListItems()
		return m, m.spinner.Tick

	case types.EventQueued:
		found := false
		if d := m.FindDownloadByID(msg.DownloadID); d != nil {
			d.RateLimit = msg.RateLimit
			d.RateLimitSet = msg.RateLimitSet
			found = true
		}
		if !found {
			newDownload := NewDownloadModel(msg.DownloadID, msg.URL, msg.Filename, 0)
			newDownload.Destination = msg.DestPath
			newDownload.RateLimit = msg.RateLimit
			newDownload.RateLimitSet = msg.RateLimitSet
			m.downloads = append(m.downloads, newDownload)
			m.SelectedDownloadID = msg.DownloadID
			m.UpdateListItems()
			return m, m.spinner.Tick
		}
		return m, nil

	case types.EventRemoved:
		if m.removeDownloadByID(msg.DownloadID) {
			if msg.Filename != "" {
				m.addLogEntry(LogStyleError.Render("\u2716 Removed: " + msg.Filename))
			}
			m.cachedTotalSpeed = m.calcTotalSpeedBps()
			m.UpdateListItems()
		}
		return m, nil

	case types.EventSystem:
		if msg.Message != "" {
			m.addLogEntry(LogStyleStarted.Render("\u2139 " + msg.Message))
		}
		return m, nil
	}

	return m, nil
}
