package orchestrator

import (
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// removed EngineHooks

// Pause pauses an active download.
func (mgr *LifecycleManager) Pause(id string) error {
	if mgr.pool == nil {
		return types.ErrEngineNotInit
	}

	if mgr.pool.Pause(id) {
		return nil
	}

	// Downloads paused in a prior session are not tracked by the in-memory pool;
	// synthesize a paused event so the UI can clear any transient "pausing" spinner.
	entry, err := store.GetDownload(id)
	if err == nil && entry != nil {
		if mgr.eventBus != nil {
			_ = mgr.eventBus.Publish(types.DownloadEvent{
				Type:         types.EventPaused,
				DownloadID:   id,
				Filename:     entry.Filename,
				Downloaded:   entry.Downloaded,
				RateLimit:    entry.RateLimit,
				RateLimitSet: entry.RateLimitSet,
			})
		}
		return nil // Already stopped
	}

	return types.ErrNotFound
}

// hydrateConfigFromDisk loads the latest persisted pause snapshot from disk
// and merges it into cfg so the download resumes at the correct byte offset
// and task list even when the pool's in-memory state is stale.
func hydrateConfigFromDisk(cfg *types.DownloadRecord) {
	if cfg.URL == "" || cfg.DestPath == "" {
		return
	}
	saved, err := store.LoadState(cfg.URL, cfg.DestPath)
	if err != nil || saved == nil {
		return
	}
	if saved.TotalSize > 0 {
		cfg.TotalSize = saved.TotalSize
	}
	if len(saved.Tasks) > 0 {
		cfg.SupportsRange = true
		cfg.Tasks = saved.Tasks
		cfg.ChunkBitmap = saved.ChunkBitmap
		cfg.ActualChunkSize = saved.ActualChunkSize
	}
}

// Resume resumes a paused download.
//
// Hot path: download is still in pool memory (same session) - extract config directly.
// Cold path: download was paused in a prior session, only stored in DB.
func (mgr *LifecycleManager) Resume(id string) error {
	if mgr.pool == nil {
		return types.ErrEngineNotInit
	}

	// Guard: still transitioning to paused
	if st := mgr.pool.GetStatus(id); st != nil {
		switch st.Status {
		case "pausing":
			return types.ErrPausing
		case "downloading", "queued":
			return types.ErrAlreadyActive
		}
	}

	// Hot path: pool still holds the paused download in memory.
	if cfg := mgr.pool.ExtractPausedConfig(id); cfg != nil {
		hydrateConfigFromDisk(cfg)
		cfg.IsResume = true

		if mgr.eventBus != nil {
			cfg.ProgressCh = mgr.eventBus.InputCh
		}

		mgr.pool.Add(*cfg)

		if mgr.eventBus != nil {
			_ = mgr.eventBus.Publish(types.DownloadEvent{
				Type:       types.EventResumed,
				DownloadID: id,
				Filename:   cfg.Filename,
			})
		}
		return nil
	}

	// Cold path: download from a prior session (only in DB).
	entry, err := store.GetDownload(id)
	if err != nil || entry == nil {
		return types.ErrNotFound
	}

	if entry.Status == "completed" {
		return types.ErrCompleted
	}

	settings := mgr.GetSettings()

	outputPath := config.Resolve[string](settings.General.DefaultDownloadDir)
	if outputPath == "" {
		outputPath = "."
	}

	savedState, stateErr := store.LoadState(entry.URL, entry.DestPath)
	if stateErr != nil {
		savedState = nil
	}

	cfg := buildResumeConfig(id, outputPath, entry, savedState, settings)

	if mgr.eventBus != nil {
		cfg.ProgressCh = mgr.eventBus.InputCh
	}
	mgr.pool.Add(cfg)

	if mgr.eventBus != nil {
		_ = mgr.eventBus.Publish(types.DownloadEvent{
			Type:       types.EventResumed,
			DownloadID: id,
			Filename:   entry.Filename,
		})
	}
	return nil
}

// ResumeBatch resumes multiple paused downloads efficiently.
func (mgr *LifecycleManager) ResumeBatch(ids []string) []error {
	errs := make([]error, len(ids))

	if mgr.pool == nil {
		for i := range errs {
			errs[i] = types.ErrEngineNotInit
		}
		return errs
	}

	settings := mgr.GetSettings()
	outputPath := config.Resolve[string](settings.General.DefaultDownloadDir)
	if outputPath == "" {
		outputPath = "."
	}

	// Partition: downloads still in pool memory (hot) vs cold (DB-only).
	var coldIDs []string
	coldIdx := make(map[string]int)

	for i, id := range ids {
		if st := mgr.pool.GetStatus(id); st != nil {
			switch st.Status {
			case "pausing":
				errs[i] = types.ErrPausing
				continue
			case "downloading", "queued":
				errs[i] = types.ErrAlreadyActive
				continue
			}
		}

		// Try hot path first
		if cfg := mgr.pool.ExtractPausedConfig(id); cfg != nil {
			hydrateConfigFromDisk(cfg)
			cfg.IsResume = true

			if mgr.eventBus != nil {
				cfg.ProgressCh = mgr.eventBus.InputCh
			}

			mgr.pool.Add(*cfg)

			if mgr.eventBus != nil {
				_ = mgr.eventBus.Publish(types.DownloadEvent{
					Type:       types.EventResumed,
					DownloadID: id,
					Filename:   cfg.Filename,
				})
			}
			errs[i] = nil
			continue
		}

		// Tag for cold-path batch load
		coldIDs = append(coldIDs, id)
		coldIdx[id] = i
	}

	if len(coldIDs) == 0 {
		return errs
	}

	states, _ := store.LoadStates(coldIDs)

	masterList, mErr := store.LoadMasterList()
	var masterMap map[string]*types.DownloadRecord
	if mErr == nil && masterList != nil {
		masterMap = make(map[string]*types.DownloadRecord, len(masterList.Downloads))
		for i := range masterList.Downloads {
			e := &masterList.Downloads[i]
			masterMap[e.ID] = e
		}
	}

	for _, id := range coldIDs {
		idx := coldIdx[id]
		savedState := states[id]

		var entry *types.DownloadRecord
		if masterMap != nil {
			entry = masterMap[id]
		}

		if savedState == nil && entry == nil {
			errs[idx] = types.ErrNotFound
			continue
		}

		if entry != nil && entry.Status == "completed" {
			errs[idx] = types.ErrCompleted
			continue
		}

		cfg := buildResumeConfig(id, outputPath, entry, savedState, settings)

		if mgr.eventBus != nil {
			cfg.ProgressCh = mgr.eventBus.InputCh
		}

		mgr.pool.Add(cfg)

		if mgr.eventBus != nil {
			_ = mgr.eventBus.Publish(types.DownloadEvent{
				Type:       types.EventResumed,
				DownloadID: id,
				Filename:   cfg.Filename,
			})
		}
		errs[idx] = nil
	}

	return errs
}

// Cancel stops a download (both pool in-memory and DB) and emits a removal event.
// The event worker handles file cleanup and DB removal via DownloadRemovedMsg.
func (mgr *LifecycleManager) Cancel(id string) error {
	var filename, destPath string
	var completed bool
	var found bool

	// Mechanical cancel via pool
	if mgr.pool != nil {
		result := mgr.pool.Cancel(id)
		if result.Found {
			found = true
			filename = result.Filename
			destPath = result.DestPath
			completed = result.Completed
		}
	}

	// Supplement with DB info (covers DB-only / completed entries)
	if entry, err := store.GetDownload(id); err == nil && entry != nil {
		found = true
		if filename == "" {
			filename = entry.Filename
		}
		if destPath == "" {
			destPath = entry.DestPath
		}
		if entry.Status == "completed" {
			completed = true
		}
	}

	if !found {
		// It's safe to treat a missing download as success during cancellation
		// because it may have been deleted in a prior session or removed
		// during a race condition (e.g. TUI refresh vs engine deletion).
		utils.Debug("Cancel: download %s not found in pool or DB, treating as success", id)
		return nil
	}

	// Emit removal event - event worker handles DB deletion and file cleanup.
	if mgr.eventBus != nil {
		_ = mgr.eventBus.Publish(types.DownloadEvent{
			Type:       types.EventRemoved,
			DownloadID: id,
			Filename:   filename,
			DestPath:   destPath,
			Completed:  completed,
		})
	}
	return nil
}

// UpdateURL updates the URL of a download in both the pool (in-memory) and the DB.
func (mgr *LifecycleManager) UpdateURL(id string, newURL string) error {
	// Update in-memory state via pool (validates download state too)
	if mgr.pool != nil {
		if err := mgr.pool.UpdateURL(id, newURL); err != nil {
			return err
		}
		// Pool update succeeded; persist to DB.
		return store.UpdateURL(id, newURL)
	}
	// No pool connected - DB-only update is correct (no in-memory state to sync).
	return store.UpdateURL(id, newURL)
}

// buildResumeConfig constructs a DownloadRecord for a cold-path resume from saved state.
// When entry is non-nil it provides identity fields (URL, filename, destPath); savedState
// takes precedence for progress, elapsed time, and mirror topology. If savedState is nil,
// SupportsRange is false and the download restarts from the entry's Downloaded offset.
func buildResumeConfig(id, outputPath string, entry *types.DownloadRecord, savedState *types.DownloadRecord, settings *config.Settings) types.DownloadRecord {
	var destPath, url, filename string
	var totalSize, downloaded int64
	var rateLimit int64
	var rateLimitSet bool

	if entry != nil {
		destPath = entry.DestPath
		url = entry.URL
		filename = entry.Filename
		totalSize = entry.TotalSize
		downloaded = entry.Downloaded
		rateLimit = entry.RateLimit
		rateLimitSet = entry.RateLimitSet
	} else if savedState != nil {
		destPath = savedState.DestPath
		url = savedState.URL
		filename = savedState.Filename
		totalSize = savedState.TotalSize
		downloaded = savedState.Downloaded
		rateLimit = savedState.RateLimit
		rateLimitSet = savedState.RateLimitSet
	}

	var tlsCAFile string
	var tlsInsecure bool
	if savedState != nil && (savedState.TLSCAFile != "" || savedState.TLSInsecure) {
		tlsCAFile = savedState.TLSCAFile
		tlsInsecure = savedState.TLSInsecure
	} else if entry != nil {
		tlsCAFile = entry.TLSCAFile
		tlsInsecure = entry.TLSInsecure
	}

	runtime := settings.ToRuntimeConfig().WithTLS(tlsCAFile, tlsInsecure)
	if !rateLimitSet {
		rateLimit = runtime.DefaultDownloadRateLimitBps
	}

	if savedState != nil && savedState.Workers > 0 {
		runtime.Workers = savedState.Workers
	} else if entry != nil && entry.Workers > 0 {
		runtime.Workers = entry.Workers
	}
	if savedState != nil && savedState.MinChunkSize > 0 {
		runtime.MinChunkSize = savedState.MinChunkSize
	} else if entry != nil && entry.MinChunkSize > 0 {
		runtime.MinChunkSize = entry.MinChunkSize
	}

	var mirrorURLs []string
	var dmState *progress.DownloadProgress

	if savedState != nil {
		dmState = progress.New(id, savedState.TotalSize)
		dmState.Bytes.Downloaded.Store(savedState.Downloaded)
		dmState.Bytes.VerifiedProgress.Store(savedState.Downloaded)
		if savedState.Elapsed > 0 {
			dmState.SetSavedElapsed(time.Duration(savedState.Elapsed))
		}
		if len(savedState.Mirrors) > 0 {
			mirrors := make([]types.MirrorStatus, 0, len(savedState.Mirrors))
			for _, u := range savedState.Mirrors {
				mirrors = append(mirrors, types.MirrorStatus{URL: u, Active: true})
				mirrorURLs = append(mirrorURLs, u)
			}
			dmState.SetMirrors(mirrors)
		}
		dmState.SetDestPath(destPath)
		dmState.SyncSessionStart()
	} else {
		dmState = progress.New(id, totalSize)
		dmState.Bytes.Downloaded.Store(downloaded)
		dmState.Bytes.VerifiedProgress.Store(downloaded)
		dmState.SetDestPath(destPath)
		dmState.SyncSessionStart()
		mirrorURLs = []string{url}
	}
	dmState.SetRateLimit(rateLimit, rateLimitSet)

	var tasks []types.Task
	var chunkBitmap []byte
	var actualChunkSize int64
	if savedState != nil {
		tasks = savedState.Tasks
		chunkBitmap = savedState.ChunkBitmap
		actualChunkSize = savedState.ActualChunkSize
	}

	return types.DownloadRecord{
		URL:             url,
		OutputPath:      outputPath,
		DestPath:        destPath,
		ID:              id,
		Filename:        filename,
		TotalSize:       totalSize,
		SupportsRange:   savedState != nil && len(savedState.Tasks) > 0,
		IsResume:        true,
		ProgressState:   dmState,
		Runtime:         runtime,
		Mirrors:         mirrorURLs,
		RateLimit:       rateLimit,
		RateLimitSet:    rateLimitSet,
		Tasks:           tasks,
		ChunkBitmap:     chunkBitmap,
		ActualChunkSize: actualChunkSize,
		TLSCAFile:       tlsCAFile,
		TLSInsecure:     tlsInsecure,
	}
}
