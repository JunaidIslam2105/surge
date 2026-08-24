package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/SurgeDM/Surge/internal/probe"
	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/strategy/concurrent"
	"github.com/SurgeDM/Surge/internal/strategy/single"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// safeSendProgress sends msg on ch, recovering from panics caused by sending
// on a closed channel (which can happen during shutdown).
func safeSendProgress(ch chan<- types.DownloadEvent, msg types.DownloadEvent, doneCh <-chan struct{}) {
	defer func() { _ = recover() }()
	if doneCh != nil {
		select {
		case ch <- msg:
			return
		default:
		}
		select {
		case ch <- msg:
		case <-doneCh:
		}
	} else {
		ch <- msg
	}
}

// uniqueFilePath returns a unique file path by appending (1), (2), etc. if the file exists
func uniqueFilePath(path string) string {
	// Check if file exists (both final and incomplete)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err := os.Stat(path + types.IncompleteSuffix); os.IsNotExist(err) {
			return path // Neither exists, use original
		}
	}

	// File exists, generate unique name
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	name := strings.TrimSuffix(filepath.Base(path), ext)

	// Check if name already has a counter like "file(1)"
	base := name
	counter := 1

	// Clean name to ensure parsing works even with trailing spaces
	cleanName := strings.TrimSpace(name)
	if len(cleanName) > 3 && cleanName[len(cleanName)-1] == ')' {
		if openParen := strings.LastIndexByte(cleanName, '('); openParen != -1 {
			// Try to parse number between parens
			numStr := cleanName[openParen+1 : len(cleanName)-1]
			if num, err := strconv.Atoi(numStr); err == nil && num > 0 {
				base = cleanName[:openParen]
				// Parsing "file (1)" -> "file " preserves original whitespace.
				counter = num + 1
			}
		}
	}

	for i := 0; i < 100; i++ { // Try next 100 numbers
		candidate := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, counter+i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			if _, err := os.Stat(candidate + types.IncompleteSuffix); os.IsNotExist(err) {
				return candidate
			}
		}
	}

	// Fallback: if all 100 numbered candidates are taken, return an empty string so
	// callers can detect the failure rather than silently receiving a conflicting path.
	return ""
}

// RunDownload is the main entry point for downloads executed by the Engine pool
func RunDownload(ctx context.Context, cfg *types.DownloadRecord) error {
	start := time.Now()
	if cfg.Runtime == nil {
		cfg.Runtime = types.DefaultRuntimeConfig()
	}
	// Engine expects cfg.OutputPath and cfg.Filename to be fully resolved by the processing layer
	destPath := cfg.OutputPath
	finalFilename := cfg.Filename
	finalDestPath := filepath.Join(destPath, finalFilename)

	// Local mirrors slice to avoid modifying config (race condition)
	mirrors := append([]string(nil), cfg.Mirrors...)

	// Check if this is a resume (explicitly marked by TUI)
	var savedState *types.DownloadRecord

	if cfg.IsResume && cfg.DestPath != "" {
		savedState = cfg

		// Restore mirrors from state if found
		if savedState != nil && len(savedState.Mirrors) > 0 {
			// Create map of existing mirrors to avoid duplicates
			existing := make(map[string]bool)
			for _, m := range mirrors {
				existing[m] = true
			}

			// Add restored mirrors
			for _, m := range savedState.Mirrors {
				if !existing[m] {
					mirrors = append(mirrors, m)
					existing[m] = true
				}
			}
			utils.Debug("Restored %d mirrors from state", len(savedState.Mirrors))
		}
	}
	isResume := cfg.IsResume && savedState != nil && savedState.DestPath != ""

	if isResume {
		// Resume: use saved destination path directly (don't generate new unique name)
		finalDestPath = savedState.DestPath
		finalFilename = filepath.Base(finalDestPath)
		utils.Debug("Resuming download, using saved destPath: %s", finalDestPath)
	}
	utils.Debug("Destination path: %s", finalDestPath)

	var progState *progress.DownloadProgress
	if cfg.ProgressState != nil {
		progState = progress.CfgProgress(cfg)
		progState.SetFilename(finalFilename)
		progState.SetDestPath(finalDestPath)
	}

	currentRateLimit := func() (int64, bool) {
		if progState != nil {
			return progState.GetRateLimit()
		}
		return cfg.RateLimit, cfg.RateLimitSet
	}

	// Send download started message
	if cfg.ProgressCh != nil {
		rateLimit, rateLimitSet := currentRateLimit()
		safeSendProgress(cfg.ProgressCh, types.DownloadEvent{
			Type:         types.EventStarted,
			DownloadID:   cfg.ID,
			URL:          cfg.URL,
			Filename:     finalFilename,
			Total:        cfg.TotalSize, // Relies on TotalSize from Config
			DestPath:     finalDestPath,
			State:        cfg,
			RateLimit:    rateLimit,
			RateLimitSet: rateLimitSet,
			Workers:      cfg.Runtime.Workers,
			MinChunkSize: cfg.Runtime.MinChunkSize,
		}, ctx.Done())
	}

	// Update shared state if we have a valid size
	if progState != nil && cfg.TotalSize > 0 {
		progState.SetTotalSize(cfg.TotalSize)
	}

	effectiveTotalSize := cfg.TotalSize
	if progState != nil && effectiveTotalSize <= 0 {
		_, stateTotal, _, _, _, _ := progState.GetProgress()
		if stateTotal > 0 {
			effectiveTotalSize = stateTotal
		}
	}

	// Choose downloader based on probe results
	var downloadErr error
	useConcurrent := cfg.SupportsRange

	if useConcurrent {
		utils.Debug("Using concurrent downloader")

		// We probe all candidate mirrors (mirrors) to filter out invalid ones
		var activeMirrors []string
		if len(mirrors) > 0 {
			utils.Debug("Probing %d mirrors", len(mirrors))
			// Always check primary + mirrors to ensure we are using the best set
			allToCheck := append([]string{cfg.URL}, mirrors...)
			runCfg := &types.RuntimeConfig{
				ProxyURL:  cfg.Runtime.ProxyURL,
				CustomDNS: cfg.Runtime.CustomDNS,
			}
			valid, errs := probe.ProbeMirrorsWithProxy(ctx, allToCheck, runCfg)

			// Log errors
			for u, e := range errs {
				utils.Debug("Mirror probe failed for %s: %v", u, e)
			}

			// Filter valid mirrors (excluding primary as it is handled separately)
			for _, v := range valid {
				if v != cfg.URL {
					activeMirrors = append(activeMirrors, v)
				}
			}
			utils.Debug("Found %d active mirrors from %d candidates", len(activeMirrors), len(mirrors))
		}

		d := concurrent.NewConcurrentDownloader(cfg.ID, cfg.ProgressCh, progState, cfg.Runtime)
		d.Headers = cfg.Headers // Forward custom headers from browser extension
		d.Limiter = cfg.Limiter
		d.RateLimitBps = cfg.RateLimit
		d.RateLimitSet = cfg.RateLimitSet
		utils.Debug("Calling Download with mirrors: %v", mirrors)
		// Pass effectiveTotalSize to avoid unnecessary bootstrap if state already knows the size
		downloadErr = d.Download(ctx, cfg.URL, mirrors, activeMirrors, finalDestPath, effectiveTotalSize)
		if d.TotalSize > 0 {
			effectiveTotalSize = d.TotalSize
		}

		var downloaded int64
		if progState != nil {
			downloaded = progState.Bytes.Downloaded.Load()
		}

		// Determine if we should attempt a fallback to single-threaded mode.
		// We fallback if concurrent failed, but it wasn't a clean pause or external cancellation,
		// AND we haven't made any progress yet (to avoid discarding progress).
		if shouldFallbackToSingle(downloadErr, downloaded) {
			utils.Debug("Concurrent download failed: %v - falling back to single-threaded", downloadErr)
			useConcurrent = false // Trigger sequential block below

			// Reset progress state cleanly for single-stream restart from byte 0
			if progState != nil {
				progState.SessionReset()
			}

			// Truncate the working file to zero to prevent stale tail bytes
			// from the failed concurrent session.
			surgePath := finalDestPath + types.IncompleteSuffix
			_ = os.Truncate(surgePath, 0)
		}
	}

	if !useConcurrent {
		// Fallback to single-threaded downloader
		utils.Debug("Using single-threaded downloader")
		d := single.NewSingleDownloader(cfg.ID, cfg.ProgressCh, progState, cfg.Runtime)
		d.Headers = cfg.Headers // Forward custom headers from browser extension
		d.Limiter = cfg.Limiter
		// Pass effectiveTotalSize here as well
		downloadErr = d.Download(ctx, cfg.URL, finalDestPath, effectiveTotalSize, finalFilename)
		if d.TotalSize > 0 {
			effectiveTotalSize = d.TotalSize
		}
		if downloadErr != nil {
			utils.Debug("Single-threaded download failed: %v", downloadErr)
		} else {
			utils.Debug("Single-threaded download completed: %d bytes", effectiveTotalSize)
		}
	}

	// Only send completion if NO error AND not paused.
	if errors.Is(downloadErr, types.ErrPaused) {
		utils.Debug("Download paused cleanly")
		return nil // Return nil so worker can remove it from active map
	}

	isPaused := progState != nil && progState.IsPaused()
	if downloadErr == nil && !isPaused {
		var elapsed time.Duration
		if progState != nil {
			_, elapsed = progState.FinalizeSession(effectiveTotalSize)
		} else {
			elapsed = time.Since(start)
		}

		// Persist to history before sending event
		// Compute average download speed in bytes/sec
		var avgSpeed float64
		if elapsed.Seconds() > 0 {
			avgSpeed = float64(effectiveTotalSize) / elapsed.Seconds()
		}

		if cfg.ProgressCh != nil {
			rateLimit, rateLimitSet := currentRateLimit()
			safeSendProgress(cfg.ProgressCh, types.DownloadEvent{
				Type:         types.EventComplete,
				DownloadID:   cfg.ID,
				Filename:     finalFilename,
				Elapsed:      elapsed,
				Total:        effectiveTotalSize,
				AvgSpeed:     avgSpeed,
				RateLimit:    rateLimit,
				RateLimitSet: rateLimitSet,
			}, ctx.Done())
		}
	} else if downloadErr != nil && !isPaused {
		// Verify it's not a cancellation error
		if errors.Is(downloadErr, context.Canceled) || errors.Is(downloadErr, context.DeadlineExceeded) {
			utils.Debug("Download canceled cleanly")
			return downloadErr
		}
		// EventError is emitted by the scheduler's worker() after all retries are exhausted.
	}

	return downloadErr
}

// shouldFallbackToSingle reports whether a failed concurrent download
// should fall back to single-threaded mode. Pause, cancel, deadline, and
// disk-full errors are excluded — Truncate cannot create free space and
// single-threaded would hit the same disk-full condition.
func shouldFallbackToSingle(downloadErr error, downloaded int64) bool {
	if downloadErr == nil {
		return false
	}
	if errors.Is(downloadErr, types.ErrPaused) {
		return false
	}
	if errors.Is(downloadErr, context.Canceled) || errors.Is(downloadErr, context.DeadlineExceeded) {
		return false
	}
	if types.IsInsufficientDiskSpace(downloadErr) {
		return false
	}
	return downloaded == 0
}

// Download is the CLI entry point (non-TUI) - convenience wrapper
func Download(ctx context.Context, url string, outPath string, progressCh chan<- types.DownloadEvent, id string) error {
	cfg := types.DownloadRecord{
		URL:           url,
		OutputPath:    outPath,
		ID:            id,
		ProgressCh:    progressCh,
		ProgressState: nil,
	}
	// Default runtime config
	cfg.Runtime = types.DefaultRuntimeConfig()
	return RunDownload(ctx, &cfg)
}
