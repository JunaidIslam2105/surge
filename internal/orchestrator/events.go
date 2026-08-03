package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

var (
	renameCompletedFile = retryRename
	copyCompletedFile   = utils.CopyFile
	notify              = utils.Notify
)

// advanceRemainingTasks keeps saved chunk boundaries aligned when pause
// recovery only knows aggregate downloaded bytes, not per-task progress.
func advanceRemainingTasks(tasks []types.Task, consumed int64) []types.Task {
	if consumed <= 0 || len(tasks) == 0 {
		return tasks
	}

	out := make([]types.Task, 0, len(tasks))
	left := consumed
	for _, task := range tasks {
		if left <= 0 {
			out = append(out, task)
			continue
		}
		if task.Length <= left {
			left -= task.Length
			continue
		}
		task.Offset += left
		task.Length -= left
		left = 0
		out = append(out, task)
	}
	return out
}

func finalizeCompletedFile(finalPath string) error {
	if finalPath == "" {
		return fmt.Errorf("missing destination path for completed download")
	}

	surgePath := finalPath + types.IncompleteSuffix
	if err := renameCompletedFile(surgePath, finalPath); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			if err := copyCompletedFile(surgePath, finalPath); err != nil {
				_ = os.Remove(finalPath)
				return fmt.Errorf("copy completed file: %w", err)
			}
			if err := retryRemove(surgePath); err != nil {
				return fmt.Errorf("remove copied working file: %w", err)
			}
			return nil
		}
		if _, statErr := os.Stat(finalPath); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

// StartEventWorker listens to engine events and handles database persistence
// and file cleanup, ensuring the core engine remains stateless.
func (mgr *LifecycleManager) StartEventWorker(ch <-chan types.DownloadEvent) {
	for msg := range ch {
		m := msg
		switch m.Type {
		case types.EventStarted:
			// Persist the started record immediately so crash recovery and later lifecycle
			// events have a stable destination record even before the first pause snapshot.
			entry := types.DownloadRecord{
				ID:           m.DownloadID,
				URL:          m.URL,
				URLHash:      store.URLHash(m.URL),
				DestPath:     m.DestPath,
				Filename:     m.Filename,
				Status:       "downloading",
				TotalSize:    m.Total,
				Downloaded:   0,
				RateLimit:    m.RateLimit,
				RateLimitSet: m.RateLimitSet,
				Workers:      m.Workers,
				MinChunkSize: m.MinChunkSize,
			}
			if existing, _ := store.GetDownload(m.DownloadID); existing != nil {
				entry.Mirrors = append([]string(nil), existing.Mirrors...)
				if existing.Downloaded > 0 {
					entry.Downloaded = existing.Downloaded
				}
				if existing.TimeTaken > 0 {
					entry.TimeTaken = existing.TimeTaken
				}
			}
			if err := store.AddToMasterList(entry); err != nil {
				utils.Debug("Lifecycle: Failed to save initial download state: %v", err)
			}

		case types.EventPaused:
			if m.State == nil {
				existing, _ := store.GetDownload(m.DownloadID)
				if existing == nil {
					utils.Debug("Lifecycle: Skipping paused fallback for %s: no persisted entry yet", m.DownloadID)
					break
				}

				entry := *existing
				entry.Status = "paused"
				if m.Downloaded > 0 {
					entry.Downloaded = m.Downloaded
				}
				if err := store.AddToMasterList(entry); err != nil {
					utils.Debug("Lifecycle: Failed to persist paused fallback entry: %v", err)
				}

				if existing.URL != "" && existing.DestPath != "" {
					saved, err := store.LoadState(existing.URL, existing.DestPath)
					if err == nil && saved != nil {
						prevDownloaded := saved.Downloaded
						prevElapsed := saved.Elapsed

						if m.Downloaded > saved.Downloaded {
							delta := m.Downloaded - saved.Downloaded
							saved.Tasks = advanceRemainingTasks(saved.Tasks, delta)
							saved.Downloaded = m.Downloaded
						}
						if existing.TimeTaken > 0 {
							candidateElapsed := existing.TimeTaken * int64(time.Millisecond)
							if candidateElapsed > saved.Elapsed {
								saved.Elapsed = candidateElapsed
							}
						}
						if saved.Downloaded > prevDownloaded && saved.Elapsed <= prevElapsed {
							saved.Elapsed = prevElapsed + int64(time.Millisecond)
						}

						if err := store.SaveStateWithOptions(existing.URL, existing.DestPath, saved, store.SaveStateOptions{SkipFileHash: true}); err != nil {
							utils.Debug("Lifecycle: Failed to persist paused fallback state: %v", err)
						}
					}
				}
				break
			}

			// Pause snapshots can race slightly behind the master entry, so fall back to
			// the DB values to keep the resume key stable when the in-memory state is sparse.
			stateSnapshot := m.State
			snapshot := *stateSnapshot
			destPath := stateSnapshot.DestPath
			url := stateSnapshot.URL

			existing, _ := store.GetDownload(m.DownloadID)
			if existing != nil {
				if destPath == "" {
					destPath = existing.DestPath
				}
				if url == "" {
					url = existing.URL
				}
				candidateElapsed := existing.TimeTaken * int64(time.Millisecond)
				if candidateElapsed > snapshot.Elapsed {
					snapshot.Elapsed = candidateElapsed
				}
				if snapshot.Downloaded > existing.Downloaded && snapshot.Elapsed <= candidateElapsed {
					snapshot.Elapsed = candidateElapsed + int64(time.Millisecond)
				}
			}

			entry := types.DownloadRecord{
				ID:           m.DownloadID,
				Status:       "paused",
				Downloaded:   snapshot.Downloaded,
				DestPath:     destPath,
				Filename:     m.Filename,
				TotalSize:    snapshot.TotalSize,
				TimeTaken:    snapshot.Elapsed / int64(time.Millisecond),
				RateLimit:    m.RateLimit,
				RateLimitSet: m.RateLimitSet,
				Workers:      m.Workers,
				MinChunkSize: m.MinChunkSize,
			}
			if existing != nil {
				entry.URL = existing.URL
				entry.URLHash = existing.URLHash
			}
			if err := store.AddToMasterList(entry); err != nil {
				utils.Debug("Lifecycle: Failed to persist paused state: %v", err)
			}

			// Persist enough chunk metadata for resume, but only once we have the same
			// destPath/url pair used everywhere else as the state DB key.
			if destPath != "" && url != "" {
				// Keep pause persistence fast so lifecycle events don't back up and get dropped.
				if err := store.SaveStateWithOptions(url, destPath, &snapshot, store.SaveStateOptions{
					SkipFileHash: true,
				}); err != nil {
					utils.Debug("Lifecycle: Failed to save pause state: %v", err)
				}
			} else {
				utils.Debug("Lifecycle: Skipping SaveState for %s: destPath=%q url=%q", m.DownloadID, destPath, url)
			}

		case types.EventComplete:
			var avgSpeed float64
			if m.Elapsed.Seconds() > 0 {
				avgSpeed = float64(m.Total) / m.Elapsed.Seconds()
			}

			destPath := ""
			// DownloadCompleteMsg does not carry destPath, so we recover the stable final
			// location from the DB entry written earlier on this same serialized event stream.
			existing, _ := store.GetDownload(m.DownloadID)
			var url, urlHash string
			filename := m.Filename
			if existing != nil {
				destPath = existing.DestPath
				url = existing.URL
				urlHash = existing.URLHash
				if filename == "" {
					filename = existing.Filename
				}
			}

			// Completion only becomes durable once the working file is promoted, so a
			// finalization failure must stay retryable instead of being recorded as done.
			if err := finalizeCompletedFile(destPath); err != nil {
				utils.Debug("Lifecycle: Failed to finalize completed file at %s: %v", destPath, err)
				errEntry := types.DownloadRecord{
					ID:           m.DownloadID,
					URL:          url,
					URLHash:      urlHash,
					DestPath:     destPath,
					Filename:     filename,
					Error:        err.Error(),
					Status:       "error",
					TotalSize:    m.Total,
					Downloaded:   m.Total,
					TimeTaken:    m.Elapsed.Milliseconds(),
					AvgSpeed:     avgSpeed,
					RateLimit:    m.RateLimit,
					RateLimitSet: m.RateLimitSet,
				}
				if existing != nil {
					errEntry.Workers = existing.Workers
					errEntry.MinChunkSize = existing.MinChunkSize
				}
				if err := store.AddToMasterList(errEntry); err != nil {
					utils.Debug("Lifecycle: Failed to persist finalization error state: %v", err)
				}
				if filename == "" {
					filename = m.DownloadID
				}
				msg := "Download failed"
				if err != nil {
					msg = err.Error()
				}
				if settings := mgr.GetSettings(); settings != nil && config.Resolve[bool](settings.General.DownloadCompleteNotification) {
					notify(fmt.Sprintf("Download failed: %s", filename), msg)
				}
				break
			}

			entry := types.DownloadRecord{
				ID:           m.DownloadID,
				URL:          url,
				URLHash:      urlHash,
				DestPath:     destPath,
				Filename:     filename,
				Status:       "completed",
				TotalSize:    m.Total,
				Downloaded:   m.Total,
				CompletedAt:  time.Now().Unix(),
				TimeTaken:    m.Elapsed.Milliseconds(),
				AvgSpeed:     avgSpeed,
				RateLimit:    m.RateLimit,
				RateLimitSet: m.RateLimitSet,
			}
			if existing != nil {
				entry.Workers = existing.Workers
				entry.MinChunkSize = existing.MinChunkSize
			}
			if err := store.AddToMasterList(entry); err != nil {
				utils.Debug("Lifecycle: Failed to persist completed download: %v", err)
			}
			if err := store.DeleteTasks(m.DownloadID); err != nil {
				utils.Debug("Lifecycle: Failed to delete completed tasks: %v", err)
			}
			if settings := mgr.GetSettings(); settings != nil && config.Resolve[bool](settings.General.DownloadCompleteNotification) {
				if filename == "" {
					filename = m.Filename
				}
				if filename == "" {
					filename = m.DownloadID
				}

				title := fmt.Sprintf("Download Complete: %s", filename)

				if m.Elapsed.Seconds() <= 0 {
					notify(title, "Download complete!")
				} else {
					notify(title, fmt.Sprintf("Download complete in %s (%.2f MiB/s)", m.Elapsed.Truncate(time.Second), avgSpeed/float64(utils.MiB)))
				}
			}

		case types.EventError:
			existing, _ := store.GetDownload(m.DownloadID)
			if m.State != nil {
				// Pause-grade snapshot: persist detail state for breakpoint resume.
				stateSnapshot := m.State
				snapshot := *stateSnapshot
				destPath := stateSnapshot.DestPath
				url := stateSnapshot.URL
				if existing != nil {
					if destPath == "" {
						destPath = existing.DestPath
					}
					if url == "" {
						url = existing.URL
					}
					candidateElapsed := existing.TimeTaken * int64(time.Millisecond)
					if candidateElapsed > snapshot.Elapsed {
						snapshot.Elapsed = candidateElapsed
					}
					if snapshot.Downloaded > existing.Downloaded && snapshot.Elapsed <= candidateElapsed {
						snapshot.Elapsed = candidateElapsed + int64(time.Millisecond)
					}
					// Backfill sparse snapshot fields from the master record so both
					// the entry and SaveStateWithOptions propagate complete metadata.
					if snapshot.Filename == "" {
						snapshot.Filename = existing.Filename
					}
					if snapshot.TotalSize == 0 {
						snapshot.TotalSize = existing.TotalSize
					}
					if snapshot.Downloaded == 0 && existing.Downloaded > 0 {
						snapshot.Downloaded = existing.Downloaded
					}
					if snapshot.Workers == 0 {
						snapshot.Workers = existing.Workers
					}
					if snapshot.MinChunkSize == 0 {
						snapshot.MinChunkSize = existing.MinChunkSize
					}
					if !snapshot.RateLimitSet && existing.RateLimitSet {
						snapshot.RateLimit = existing.RateLimit
						snapshot.RateLimitSet = existing.RateLimitSet
					}
				}
				if destPath == "" {
					destPath = m.DestPath
				}
				entry := types.DownloadRecord{
					ID:           m.DownloadID,
					URL:          url,
					DestPath:     destPath,
					Filename:     m.Filename,
					Status:       "error",
					TotalSize:    snapshot.TotalSize,
					Downloaded:   snapshot.Downloaded,
					Workers:      snapshot.Workers,
					MinChunkSize: snapshot.MinChunkSize,
					RateLimit:    snapshot.RateLimit,
					RateLimitSet: snapshot.RateLimitSet,
					TimeTaken:    snapshot.Elapsed / int64(time.Millisecond),
				}
				if m.Err != nil {
					entry.Error = m.Err.Error()
				}
				if existing != nil {
					entry.URLHash = existing.URLHash
					entry.Mirrors = append([]string(nil), existing.Mirrors...)
					if entry.Filename == "" {
						entry.Filename = existing.Filename
					}
				} else {
					entry.URLHash = store.URLHash(url)
				}
				if err := store.AddToMasterList(entry); err != nil {
					utils.Debug("Lifecycle: Failed to persist error state: %v", err)
				}
				if destPath != "" && url != "" {
					if err := store.SaveStateWithOptions(url, destPath, &snapshot, store.SaveStateOptions{
						SkipFileHash: true,
					}); err != nil {
						utils.Debug("Lifecycle: Failed to persist error detail state: %v", err)
					}
				} else {
					utils.Debug("Lifecycle: Skipping SaveState on error for %s: destPath=%q url=%q", m.DownloadID, destPath, url)
				}
			} else {
				if existing == nil {
					existing = &types.DownloadRecord{ID: m.DownloadID}
				}
				existing.Status = "error"
				if m.Err != nil {
					existing.Error = m.Err.Error()
				}
				if m.Filename != "" {
					existing.Filename = m.Filename
				}
				if m.DestPath != "" {
					existing.DestPath = m.DestPath
				}
				if m.URL != "" {
					existing.URL = m.URL
					existing.URLHash = store.URLHash(m.URL)
				}
				if err := store.AddToMasterList(*existing); err != nil {
					utils.Debug("Lifecycle: Failed to persist error state: %v", err)
				}
			}
			if settings := mgr.GetSettings(); settings != nil && config.Resolve[bool](settings.General.DownloadCompleteNotification) {
				filename := m.Filename
				if filename == "" && existing != nil {
					filename = existing.Filename
				}
				if filename == "" {
					filename = m.DownloadID
				}

				msg := "Download failed"
				if m.Err != nil {
					msg = m.Err.Error()
				}

				notify(fmt.Sprintf("Download failed: %s", filename), msg)
			}

		case types.EventRemoved:
			// Remove resume metadata before touching files so a deleted download does not
			// come back during startup recovery. DeleteState atomically removes both the
			// detail gob and the master list entry, so no separate RemoveFromMasterList call
			// is needed.
			if err := store.DeleteState(m.DownloadID); err != nil {
				utils.Debug("Lifecycle: Failed to delete state: %v", err)
			}

			// Only incomplete working files should be removed here; completed files have
			// already been promoted to their final name by the completion path.
			if m.DestPath != "" && !m.Completed {
				if err := RemoveIncompleteFile(m.DestPath); err != nil {
					utils.Debug("Lifecycle: Failed to remove incomplete file: %v", err)
				}
			}

		case types.EventQueued:
			// enqueueResolved already persisted this record synchronously (status="queued")
			// before pool.Add, so the entry normally exists by the time we arrive here.
			// Only write if absent to avoid regressing an already-advanced status
			// (e.g. "downloading") when EventStarted races ahead of this event.
			if existing, _ := store.GetDownload(m.DownloadID); existing == nil {
				if err := store.AddToMasterList(types.DownloadRecord{
					ID:           m.DownloadID,
					URL:          m.URL,
					URLHash:      store.URLHash(m.URL),
					DestPath:     m.DestPath,
					Filename:     m.Filename,
					Mirrors:      append([]string(nil), m.Mirrors...),
					Status:       "queued",
					RateLimit:    m.RateLimit,
					RateLimitSet: m.RateLimitSet,
					Workers:      m.Workers,
					MinChunkSize: m.MinChunkSize,
				}); err != nil {
					utils.Debug("Lifecycle: Failed to persist queued download: %v", err)
				}
			}

		case types.EventResumed, types.EventRequest, types.EventBatchRequest, types.EventSystem:
			// These events require no persistence in the lifecycle worker.

		case types.EventBatchProgress, types.EventProgress:
			// Progress ticks are intentionally transient; persisting them would add
			// file I/O churn without improving resume or history recovery.
		}
	}
}
