package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func (m *RootModel) processProgressMsg(msg types.DownloadEvent) tea.Cmd {
	d := m.FindDownloadByID(msg.DownloadID)
	if d == nil || d.done || d.paused {
		return nil
	}

	prevDownloaded := d.Downloaded
	d.Downloaded = msg.Downloaded
	d.Total = msg.Total
	d.Speed = msg.Speed
	d.Elapsed = msg.Elapsed
	d.Connections = msg.Connections
	d.rateLimited = msg.RateLimited

	// Keep "Resuming..." visible until we observe actual transfer.
	if d.resuming && (d.Speed > 0 || d.Downloaded > prevDownloaded) {
		d.resuming = false
	}

	// Update Chunk State if provided
	if msg.BitmapWidth > 0 && len(msg.ChunkBitmap) > 0 {
		if d.state != nil && msg.Total > 0 {
			d.state.SetTotalSize(msg.Total)
		}
		// We only get bitmap, no progress array (to save bandwidth)
		// State needs to be updated carefully
		if d.state != nil {
			d.state.RestoreBitmap(msg.ChunkBitmap, msg.ChunkSize)
		}
		if d.state != nil && len(msg.ChunkProgress) > 0 {
			d.state.SetChunkProgress(msg.ChunkProgress)
		}
	}

	var cmd tea.Cmd
	if d.Total > 0 {
		percentage := float64(d.Downloaded) / float64(d.Total)
		cmd = d.progress.SetPercent(percentage)
	}

	// Cache total speed for this frame so View() doesn't recompute it.
	m.cachedTotalSpeed = m.calcTotalSpeedBps()

	// Update speed graph history with EMA smoothing for smooth transitions
	if time.Since(m.lastSpeedHistoryUpdate) >= GraphUpdateInterval {
		totalSpeed := float64(m.cachedTotalSpeed)
		// EMA smooth against previous graph point for visual continuity
		var smoothed float64
		if m.Settings != nil && config.Resolve[bool](m.Settings.General.LiveSpeedGraph) {
			smoothed = totalSpeed
		} else if len(m.SpeedHistory) > 0 {
			prev := m.SpeedHistory[len(m.SpeedHistory)-1]
			const graphAlpha = 0.3 // Graph smoothing factor
			smoothed = graphAlpha*totalSpeed + (1-graphAlpha)*prev
		} else {
			smoothed = totalSpeed
		}
		if len(m.SpeedHistory) > 0 {
			m.SpeedHistory = append(m.SpeedHistory[1:], smoothed)
		}
		m.lastSpeedHistoryUpdate = time.Now()
		m.graphCacheDirty = true
	}

	// ponytail: list items hold *DownloadModel pointers — speed/progress are
	// read live on every View() without a rebuild. UpdateListItems() is only
	// needed for structural changes (start/pause/complete/error/tab switch).
	return cmd
}

// startDownload initiates a new download
func (m RootModel) startDownload(url string, mirrors []string, headers map[string]string, path string, isDefaultPath bool, filename, id string, workers int, minChunkSize int64) (RootModel, tea.Cmd) {
	if m.Service == nil {
		m.addLogEntry(LogStyleError.Render("\u2716 Service unavailable"))
		return m, nil
	}

	// Enforce absolute path
	path = utils.EnsureAbsPath(path)

	candidateFilename := strings.TrimSpace(filename)
	requestID := strings.TrimSpace(id)

	resolvedPath := path
	resolvedFilename := candidateFilename
	optimisticFilename := candidateFilename
	if p, f, err := orchestrator.ResolveDestination(url, candidateFilename, path, isDefaultPath, m.Settings, nil, nil); err == nil {
		resolvedPath = p
		resolvedFilename = f
		if candidateFilename != "" {
			// Only mirror the resolved filename into the optimistic row when the
			// user already chose it; probe-derived names can legitimately change.
			optimisticFilename = f
		}
	} else {
		utils.Debug("Optimistic destination resolve failed for %s: %v", url, err)
	}

	// Call Orchestrator Enqueue
	req := &orchestrator.DownloadRequest{
		URL:                url,
		Filename:           candidateFilename,
		Path:               path,
		Mirrors:            mirrors,
		Headers:            headers,
		IsExplicitCategory: !isDefaultPath,
		SkipApproval:       true,
		Workers:            workers,
		MinChunkSize:       minChunkSize,
	}

	optimisticID := requestID
	if optimisticID == "" {
		optimisticID = fmt.Sprintf("pending-%d", time.Now().UnixNano())
	}
	displayName := optimisticFilename
	if displayName == "" {
		displayName = orchestrator.InferFilenameFromURL(url)
	}
	if displayName == "" {
		displayName = "Queued"
	}

	newDownload := NewDownloadModel(optimisticID, url, displayName, 0)
	if resolvedFilename != "" {
		newDownload.Destination = filepath.Join(resolvedPath, resolvedFilename)
	} else {
		newDownload.Destination = resolvedPath
	}
	m.downloads = append(m.downloads, newDownload)
	m.SelectedDownloadID = optimisticID
	if m.pinnedTab == -1 {
		m.activeTab = TabQueued
	}
	m.UpdateListItems()

	// Legacy path for tests or startup wiring where processing is not injected yet.
	if m.Orchestrator == nil {
		var (
			newID string
			err   error
		)
		if requestID != "" {
			newID, err = m.Service.AddWithID(
				req.URL,
				req.Path,
				req.Filename,
				req.Mirrors,
				req.Headers,
				requestID,
				req.IsExplicitCategory,
				req.Workers,
				req.MinChunkSize,
			)
		} else {
			newID, err = m.Service.Add(
				url,
				resolvedPath,
				resolvedFilename,
				mirrors,
				headers,
				!isDefaultPath,
				workers,
				minChunkSize,
			)
		}
		if err != nil {
			m.removeDownloadByID(optimisticID)
			m.UpdateListItems()
			m.addLogEntry(LogStyleError.Render("\u2716 Failed to add download: " + err.Error()))
			return m, nil
		}

		if d := m.FindDownloadByID(optimisticID); d != nil {
			d.ID = newID
		}
		if m.SelectedDownloadID == optimisticID {
			m.SelectedDownloadID = newID
		}
		m.UpdateListItems()
		return m, nil
	}

	cmd := func() tea.Msg {
		ctx := m.downloadEnqueueContext()
		var newID, finalFilename string
		var err error
		if requestID != "" {
			newID, finalFilename, err = m.Orchestrator.EnqueueWithID(ctx, req, requestID)
		} else {
			newID, finalFilename, err = m.Orchestrator.Enqueue(ctx, req)
		}
		if err != nil {
			return enqueueErrorMsg{tempID: optimisticID, err: err}
		}

		// Use the server-resolved filename if available
		displayFilename := finalFilename
		if displayFilename == "" {
			displayFilename = optimisticFilename
		}

		return enqueueSuccessMsg{
			tempID:   optimisticID,
			id:       newID,
			url:      url,
			path:     resolvedPath,
			filename: displayFilename,
		}
	}

	utils.Debug("Queued enqueue command (via Orchestrator): %s -> %s", url, optimisticFilename)
	return m, cmd
}

func (m RootModel) defaultDownloadPath() string {
	if m.Settings != nil {
		if path := strings.TrimSpace(config.Resolve[string](m.Settings.General.DefaultDownloadDir)); path != "" {
			return path
		}
	}
	return "."
}

func (m RootModel) downloadEnqueueContext() context.Context {
	if m.enqueueCtx != nil {
		return m.enqueueCtx
	}
	return context.Background()
}

func (m RootModel) isDefaultDownloadPath(path string) bool {
	return utils.EnsureAbsPath(path) == utils.EnsureAbsPath(m.defaultDownloadPath())
}
