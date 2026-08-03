package concurrent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// worker downloads tasks from the queue
func (d *ConcurrentDownloader) worker(ctx context.Context, id int, mirrors []string, file *os.File, queue *TaskQueue, totalSize int64, client *http.Client) error {
	bufPtr := d.bufPool.Get().(*[]byte)
	defer d.bufPool.Put(bufPtr)
	buf := *bufPtr

	utils.Debug("Worker %d started", id)
	defer utils.Debug("Worker %d finished", id)

	currentMirrorIdx := id % len(mirrors)

	mirrorHosts := make([]string, len(mirrors))
	for i, m := range mirrors {
		mirrorHosts[i] = transport.MirrorHost(m)
	}

	for {
		task, ok := queue.Pop()
		if !ok {
			return nil
		}

		if d.State != nil {
			d.State.ActiveWorkers.Add(1)
		}

		now := time.Now()
		activeTask := &ActiveTask{
			Task:        task,
			StartTime:   now,
			WindowStart: now,
		}
		if task.SharedMaxOffset != nil {
			activeTask.SharedMaxOffset = task.SharedMaxOffset
			activeTask.Hedged.Store(1)
		}
		activeTask.CurrentOffset.Store(task.Offset)
		activeTask.StopAt.Store(task.Offset + task.Length)
		activeTask.LastActivity.Store(now.UnixNano())

		d.activeMu.Lock()
		d.activeTasks[id] = activeTask
		d.activeMu.Unlock()

		var lastErr error
		maxRetries := d.Runtime.GetMaxTaskRetries()
		genericAttempt := 0
		rlRetries := 0

		for {
			idx, wait := d.hostLimiter.PickMirror(mirrorHosts, currentMirrorIdx, time.Now())
			currentMirrorIdx = idx
			if wait > 0 {
				activeTask.WaitingOnLimiter.Store(true)
				if !interruptibleSleep(ctx, wait) {
					activeTask.WaitingOnLimiter.Store(false)
					if d.State != nil {
						d.State.ActiveWorkers.Add(-1)
					}
					return ctx.Err()
				}
				activeTask.WaitingOnLimiter.Store(false)
			}
			currentURL := mirrors[currentMirrorIdx]

			taskCtx, taskCancel := context.WithCancel(ctx)
			now := time.Now()
			
			d.activeMu.Lock()
			activeTask.Cancel = taskCancel
			activeTask.StartTime = now
			d.activeMu.Unlock()
			
			activeTask.WindowStart = now
			activeTask.WindowBytes.Store(0)
			activeTask.LastActivity.Store(now.UnixNano())

			if d.State != nil {
				utils.Debug("Worker %d: Setting range %d-%d to Downloading", id, task.Offset, task.Offset+task.Length)
				d.State.UpdateChunkStatus(task.Offset, task.Length, types.ChunkDownloading)
			} else {
				utils.Debug("Worker %d: d.State is nil, cannot update chunk status", id)
			}

			taskStart := time.Now()
			lastErr = d.downloadTask(taskCtx, currentURL, file, activeTask, buf, client, totalSize)

			wasExternallyCancelled := taskCtx.Err() != nil

			taskCancel()
			utils.Debug("Worker %d: Task offset=%d length=%d took %v", id, task.Offset, task.Length, time.Since(taskStart))

			if ctx.Err() != nil {
				if d.State != nil {
					d.State.ActiveWorkers.Add(-1)
				}
				return ctx.Err()
			}

			if wasExternallyCancelled && lastErr != nil {
				currentMirrorIdx = (currentMirrorIdx + 1) % len(mirrors)
				utils.Debug("Worker %d: Health check cancelled task, rotating from mirror %s to %s", id, mirrors[(currentMirrorIdx+len(mirrors)-1)%len(mirrors)], mirrors[currentMirrorIdx])

				if remaining := activeTask.RemainingTask(); remaining != nil {
					originalEnd := task.Offset + task.Length
					if remaining.Offset+remaining.Length > originalEnd {
						remaining.Length = originalEnd - remaining.Offset
					}
					if remaining.Length > 0 {
						queue.Push(*remaining)
						utils.Debug("Worker %d: health-cancelled task requeued (remaining: %d bytes from offset %d)",
							id, remaining.Length, remaining.Offset)
					}
				}
				lastErr = nil
				break
			}

			if lastErr == nil {
				d.hostLimiter.RecordSuccess(mirrorHosts[currentMirrorIdx])
				stopAt := activeTask.StopAt.Load()
				current := activeTask.CurrentOffset.Load()
				if current < task.Offset+task.Length && current >= stopAt {
					utils.Debug("Worker stopped early due to stealing")
				}
				break
			}

			var rlErr *rateLimitError
			if errors.As(lastErr, &rlErr) {
				d.hostLimiter.Penalize(mirrorHosts[currentMirrorIdx], rlErr.retryAfter, rlErr.explicit, time.Now())
				d.ReportMirrorError(currentURL)
				rlRetries++
				if rlRetries > types.RateLimitMaxRetries {
					break
				}
				currentMirrorIdx = (currentMirrorIdx + 1) % len(mirrors)
				resumeOnRetryOffset(&task, activeTask)
				continue
			}

			genericAttempt++
			if genericAttempt >= maxRetries {
				break
			}
			d.ReportMirrorError(mirrors[currentMirrorIdx])
			currentMirrorIdx = (currentMirrorIdx + 1) % len(mirrors)
			if len(mirrors) == 1 {
				activeTask.WaitingOnLimiter.Store(true)
				interruptibleSleep(ctx, time.Duration(1<<genericAttempt)*types.RetryBaseDelay)
				activeTask.WaitingOnLimiter.Store(false)
			}
			resumeOnRetryOffset(&task, activeTask)
		}

		if d.State != nil {
			d.State.ActiveWorkers.Add(-1)
		}

		if lastErr != nil {
			utils.Debug("Worker %d: task at offset %d failed after %d retries: %v", id, task.Offset, maxRetries, lastErr)

			if remain := activeTask.RemainingTask(); remain != nil {
				queue.Push(*remain)
			}

			d.activeMu.Lock()
			delete(d.activeTasks, id)
			d.activeMu.Unlock()

			return lastErr
		}

		d.activeMu.Lock()
		delete(d.activeTasks, id)
		d.activeMu.Unlock()
	}
}

// downloadTask downloads a single byte range and writes to file at offset
func (d *ConcurrentDownloader) downloadTask(ctx context.Context, rawurl string, file *os.File, activeTask *ActiveTask, buf []byte, client *http.Client, totalSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	task := activeTask.Task

	// Apply custom headers first (from browser extension: cookies, auth, referer, etc.)
	for key, val := range d.Headers {
		// Skip Range header - we set it ourselves for parallel downloads
		if key != "Range" {
			req.Header.Set(key, val)
		}
	}

	// Set User-Agent from config only if not provided in custom headers
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", d.Runtime.GetUserAgent())
	}
	// Range header is always set for partial downloads (overrides any browser Range header)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Offset, task.Offset+task.Length-1))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	// Handle rate limiting explicitly
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("Retry-After") != "") {
		ra, ok := transport.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return &rateLimitError{retryAfter: ra, explicit: ok}
	}

	// Validate status code
	if resp.StatusCode == http.StatusOK {
		// Valid only if we requested the full file
		// If we wanted a partial range but got the whole file (200), that's an error because we can't handle the full stream at a non-zero offset
		if task.Offset != 0 || task.Length != totalSize {
			return fmt.Errorf("server indicated success (200) but ignored range request (expected 206)")
		}
	} else if resp.StatusCode != http.StatusPartialContent {
		if types.IsPermanentHTTPStatus(resp.StatusCode) {
			return fmt.Errorf("unexpected status: %d: %w", resp.StatusCode, types.ErrPermanentHTTP)
		}
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Batching State
	var pendingBytes int64
	var pendingStart int64 = -1
	lastUpdate := time.Now()
	batchSizeThreshold := int64(types.WorkerBatchSize)
	batchTimeThreshold := types.WorkerBatchInterval

	// Helper to flush pending updates to global state
	flushUpdates := func() {
		if pendingBytes > 0 && d.State != nil {
			// Update Chunk Map (Global Lock)
			d.State.UpdateChunkStatus(pendingStart, pendingBytes, types.ChunkCompleted)

			// Update Downloaded Counter (Atomic)
			d.State.Bytes.Downloaded.Add(pendingBytes)

			pendingBytes = 0
			pendingStart = -1
			lastUpdate = time.Now()
		}
	}
	// Ensure we flush whatever we have on exit
	defer flushUpdates()

	// Read and write at offset
	offset := task.Offset
	for {
		// Check if we should stop
		stopAt := activeTask.StopAt.Load()
		if offset >= stopAt {
			// Stealing happened, stop here
			return nil
		}

		// Calculate how much to read to fill buffer or hit stopAt/EOF
		// We want to fill buf as much as possible to minimize WriteAt calls

		// Limit by remaining length to stopAt
		remaining := stopAt - offset
		if remaining <= 0 {
			return nil
		}

		readSize := int64(len(buf))
		if readSize > remaining {
			readSize = remaining
		}

		readSoFar := 0
		var readErr error

		for readSoFar < int(readSize) {
			n, err := resp.Body.Read(buf[readSoFar:readSize])
			if n > 0 {
				readSoFar += n
				// CONTINUOUS HEALTH KEEPALIVE:
				// Update LastActivity directly off the TCP socket instead of waiting for the buffer
				// to completely fill and hit disk. This prevents the Health Monitor from killing
				// workers on slightly slower networks during the 500KB buffer acquisition.
				activeTask.LastActivity.Store(time.Now().UnixNano())
			}
			if err != nil {
				readErr = err
				break
			}
			if n == 0 {
				readErr = io.ErrUnexpectedEOF
				break
			}
		}

		if readSoFar > 0 {
			// check stopAt again before writing
			// truncate readSoFar
			currentStopAt := activeTask.StopAt.Load()
			if offset+int64(readSoFar) > currentStopAt {
				readSoFar = int(currentStopAt - offset)
				if readSoFar <= 0 {
					return nil // stolen completely
				}
			}

			if d.Limiter != nil {
				// Reset stall clock before the wait so the health monitor measures
				// time from when throttling begins, not from the last network read.
				activeTask.LastActivity.Store(time.Now().UnixNano())
				activeTask.WaitingOnLimiter.Store(true)
				err := d.Limiter.WaitN(ctx, int64(readSoFar))
				activeTask.WaitingOnLimiter.Store(false)
				if err != nil {
					return err
				}

				// Refresh again after the wait to keep the stall clock current.
				activeTask.LastActivity.Store(time.Now().UnixNano())
			}

			_, writeErr := file.WriteAt(buf[:readSoFar], offset)
			if writeErr != nil {
				return fmt.Errorf("write error: %w", writeErr)
			}

			now := time.Now()
			rangeStart := offset // Start of this write
			offset += int64(readSoFar)

			// Compute newly written bytes deduplicated across racing workers
			var newlyWritten int64
			// Read pointer under RLock to avoid racing with hedger initialization
			activeTask.SharedMaxOffsetMu.RLock()
			ptr := activeTask.SharedMaxOffset
			activeTask.SharedMaxOffsetMu.RUnlock()
			if ptr != nil {
				for {
					maxOff := ptr.Load()
					if offset <= maxOff {
						// This exact byte range was already reported by the racing worker!
						newlyWritten = 0
						break
					}
					if rangeStart >= maxOff {
						// Entirely new progress
						if ptr.CompareAndSwap(maxOff, offset) {
							newlyWritten = int64(readSoFar)
							break
						}
					} else {
						// Partially new progress
						if ptr.CompareAndSwap(maxOff, offset) {
							newlyWritten = offset - maxOff
							break
						}
					}
				}
			} else {
				newlyWritten = int64(readSoFar)
			}

			activeTask.CurrentOffset.Store(offset)
			activeTask.WindowBytes.Add(newlyWritten)
			activeTask.LastActivity.Store(now.UnixNano())

			// Calculate effective contribution
			if newlyWritten > 0 {
				if pendingStart == -1 {
					pendingStart = offset - newlyWritten
				}
				pendingBytes += newlyWritten
			}

			// Check thresholds
			if pendingBytes >= batchSizeThreshold || now.Sub(lastUpdate) >= batchTimeThreshold {
				flushUpdates()
			}

			// Update EMA speed using sliding window (2 second window)
			// This relies on WindowBytes which is updated atomically above, so independent of batching
			windowElapsed := now.Sub(activeTask.WindowStart).Seconds()
			if windowElapsed >= 2.0 {
				windowBytes := activeTask.WindowBytes.Swap(0)
				recentSpeed := float64(windowBytes) / windowElapsed

				activeTask.SpeedMu.Lock()
				alpha := d.Runtime.GetSpeedEmaAlpha()
				if alpha <= 0 || activeTask.Speed == 0 {
					// Alpha 0 disables smoothing and uses the latest measured speed directly.
					activeTask.Speed = recentSpeed
				} else {
					activeTask.Speed = (1-alpha)*activeTask.Speed + alpha*recentSpeed
				}
				activeTask.SpeedMu.Unlock()

				activeTask.WindowStart = now // Reset window
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read error: %w", readErr)
		}
	}

	return nil
}

// StealWork tries to split an active task from a busy worker
// It greedily targets the worker with the MOST remaining work.
func (d *ConcurrentDownloader) StealWork(queue *TaskQueue) bool {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	bestID := -1
	var maxRemaining int64 = 0
	var bestActive *ActiveTask

	// Find the worker with the MOST remaining work
	for id, active := range d.activeTasks {
		remaining := active.RemainingBytes()
		if remaining > types.MinChunk && remaining > maxRemaining {
			maxRemaining = remaining
			bestID = id
			bestActive = active
		}
	}

	if bestID == -1 {
		return false
	}

	// Found the best candidate, now try to steal
	remaining := maxRemaining
	active := bestActive

	// Split in half, aligned to AlignSize
	splitSize := alignedSplitSize(remaining)
	if splitSize == 0 {
		return false
	}

	current := active.CurrentOffset.Load()
	newStopAt := current + splitSize

	// Update the active task stop point
	active.StopAt.Store(newStopAt)

	finalCurrent := active.CurrentOffset.Load()

	// The actual start of the stolen chunk must be after where the worker effectively stops.
	stolenStart := newStopAt
	if finalCurrent > newStopAt {
		stolenStart = finalCurrent
	}

	// Double check: ensure we didn't race and lose the chunk
	currentStopAt := active.StopAt.Load()
	if stolenStart >= currentStopAt && currentStopAt != newStopAt {
		utils.Debug("StealWork race detected: stolenStart >= currentStopAt")
	}

	originalEnd := current + remaining

	if stolenStart >= originalEnd {
		return false
	}

	stolenTask := types.Task{
		Offset: stolenStart,
		Length: originalEnd - stolenStart,
	}

	queue.Push(stolenTask)
	utils.Debug("Balancer: stole %s from worker %d (new range: %d-%d)",
		utils.FormatBytes(stolenTask.Length), bestID, stolenTask.Offset, stolenTask.Offset+stolenTask.Length)

	return true
}

// HedgeWork creates a duplicate task when stealing isn't possible (chunks too small).
// An idle worker picks up the duplicate and races the original on a fresh HTTP connection.
// Both workers write identical data to the same file offsets (WriteAt is idempotent),
// so the file is always correct. Whichever finishes first wins; the other exits
// naturally when the queue closes or its next read returns data already counted.
func (d *ConcurrentDownloader) HedgeWork(queue *TaskQueue) bool {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	if len(d.activeTasks) == 0 {
		return false
	}

	// Find the active task with the most remaining work that hasn't been hedged yet
	var bestActive *ActiveTask
	var maxRemaining int64

	for _, active := range d.activeTasks {
		// Skip tasks already being raced
		if active.Hedged.Load() != 0 {
			continue
		}
		remaining := active.RemainingBytes()
		if remaining > 0 && remaining > maxRemaining {
			maxRemaining = remaining
			bestActive = active
		}
	}

	if bestActive == nil || maxRemaining == 0 {
		return false
	}

	// Mark as hedged so we don't create multiple duplicates
	if !bestActive.Hedged.CompareAndSwap(0, 1) {
		return false // Another goroutine hedged it first
	}

	// Create a duplicate task for the remaining byte range
	current := bestActive.CurrentOffset.Load()
	stopAt := bestActive.StopAt.Load()
	if current >= stopAt {
		return false
	}

	// Initialize the shared deduplication state for both tasks
	bestActive.SharedMaxOffsetMu.Lock()
	if bestActive.SharedMaxOffset == nil {
		maxOff := &atomic.Int64{}
		maxOff.Store(current)
		bestActive.SharedMaxOffset = maxOff
	}
	// Create a duplicate task for the remaining byte range
	hedgedTask := types.Task{
		Offset:          current,
		Length:          stopAt - current,
		SharedMaxOffset: bestActive.SharedMaxOffset,
	}
	bestActive.SharedMaxOffsetMu.Unlock()

	queue.Push(hedgedTask)
	utils.Debug("Balancer: hedged %s (range: %d-%d) - idle worker will race on fresh connection",
		utils.FormatBytes(hedgedTask.Length), hedgedTask.Offset, hedgedTask.Offset+hedgedTask.Length)

	return true
}

func resumeOnRetryOffset(task *types.Task, activeTask *ActiveTask) {
	current := activeTask.CurrentOffset.Load()
	if current > task.Offset {
		oldStart := task.Offset
		task.Offset = current
		task.Length = oldStart + task.Length - current
		activeTask.Task = *task
	}
}
