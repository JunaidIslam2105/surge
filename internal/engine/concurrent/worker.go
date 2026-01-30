package concurrent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

// worker downloads tasks from the queue
func (d *ConcurrentDownloader) worker(ctx context.Context, id int, rawurl string, queue *TaskQueue, totalSize int64, startTime time.Time, verbose bool, client *http.Client) error {
	// Get pooled buffer
	// Get pooled buffer (manage lifecycle manually for zero-copy)
	bufPtr := d.bufferPool.Get().(*[]byte)

	// Ensure we return the LAST held buffer on exit
	defer func() {
		if bufPtr != nil {
			// Reslice to capacity just in case
			if cap(*bufPtr) == d.Runtime.GetWorkerBufferSize() {
				*bufPtr = (*bufPtr)[:cap(*bufPtr)]
				d.bufferPool.Put(bufPtr)
			}
		}
	}()
	// buf := *bufPtr // Don't dereference here as we need the pointer to rotate it

	utils.Debug("Worker %d started", id)
	defer utils.Debug("Worker %d finished", id)

	for {
		// Get next task
		task, ok := queue.Pop()

		if !ok {
			return nil // Queue closed, no more work
		}

		// Update active workers
		if d.State != nil {
			d.State.ActiveWorkers.Add(1)
		}

		var lastErr error
		maxRetries := d.Runtime.GetMaxTaskRetries()
		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(1<<attempt) * types.RetryBaseDelay) //Exponential backoff incase of failure
			}

			// Register active task with per-task cancellable context
			taskCtx, taskCancel := context.WithCancel(ctx)
			now := time.Now()
			activeTask := &ActiveTask{
				Task:          task,
				CurrentOffset: task.Offset,
				StopAt:        task.Offset + task.Length,
				LastActivity:  now.UnixNano(),
				StartTime:     now,
				Cancel:        taskCancel,
				WindowStart:   now, // Initialize sliding window
			}
			d.activeMu.Lock()
			d.activeTasks[id] = activeTask
			d.activeMu.Unlock()

			taskStart := time.Now()
			// Pass bufPtr to downloadTask which may rotate it
			lastErr = d.downloadTask(taskCtx, rawurl, activeTask, &bufPtr, verbose, client)

			// CRITICAL: Capture external cancellation state BEFORE calling taskCancel()
			// If we call taskCancel() first, taskCtx.Err() will always be non-nil
			wasExternallyCancelled := taskCtx.Err() != nil

			taskCancel() // Clean up context resources
			utils.Debug("Worker %d: Task offset=%d length=%d took %v", id, task.Offset, task.Length, time.Since(taskStart))

			// Check for PARENT context cancellation (pause/shutdown)
			// This preserves active task info for pause handler to collect
			if ctx.Err() != nil {
				// DON'T delete from activeTasks - pause handler needs it
				if d.State != nil {
					d.State.ActiveWorkers.Add(-1)
				}
				return ctx.Err()
			}

			// Check if TASK context was cancelled by Health Monitor (not by us calling taskCancel)
			// but parent context is still fine
			if wasExternallyCancelled && lastErr != nil {
				// Health monitor cancelled this task - re-queue REMAINING work only
				if remaining := activeTask.RemainingTask(); remaining != nil {
					// Clamp to original task end (don't go past original boundary)
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
				// Delete from active tasks and move to next task (don't retry from scratch)
				d.activeMu.Lock()
				delete(d.activeTasks, id)
				d.activeMu.Unlock()
				// Clear lastErr so the fallthrough logic doesn't re-queue the original task
				lastErr = nil
				break // Exit retry loop, get next task
			}

			// Only delete from activeTasks on normal completion (not cancelled)
			d.activeMu.Lock()
			delete(d.activeTasks, id)
			d.activeMu.Unlock()

			if lastErr == nil {
				// Check if we stopped early due to stealing
				stopAt := atomic.LoadInt64(&activeTask.StopAt)
				current := atomic.LoadInt64(&activeTask.CurrentOffset)
				if current < task.Offset+task.Length && current >= stopAt {
					// We were stopped early this is expected success for the partial work
					// The stolen part is already in the queue
				}
				break
			}

			// Resume-on-retry: update task to reflect remaining work
			// This prevents double-counting bytes on retry
			current := atomic.LoadInt64(&activeTask.CurrentOffset)
			if current > task.Offset {
				task = types.Task{Offset: current, Length: task.Offset + task.Length - current}
			}
		}

		// Update active workers
		if d.State != nil {
			d.State.ActiveWorkers.Add(-1)
		}

		if lastErr != nil {
			// Log failed task but continue with next task
			// If we modified StopAt we should probably reset it or push the remaining part?
			// TODO: Could optimize by pushing only remaining part if we track that.
			queue.Push(task)
			utils.Debug("task at offset %d failed after %d retries: %v", task.Offset, maxRetries, lastErr)
		}
	}
}

// downloadTask downloads a single byte range and writes to file at offset
func (d *ConcurrentDownloader) downloadTask(ctx context.Context, rawurl string, activeTask *ActiveTask, bufPtrWrapper **[]byte, verbose bool, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	task := activeTask.Task

	// Initialize local buffer reference
	buf := **bufPtrWrapper

	req.Header.Set("User-Agent", d.Runtime.GetUserAgent())
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Offset, task.Offset+task.Length-1))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Handle rate limiting explicitly
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Read and write at offset
	offset := task.Offset
	for {
		// Check if we should stop
		stopAt := atomic.LoadInt64(&activeTask.StopAt)
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

			// Check stopAt again before writing
			// truncate readSoFar
			currentStopAt := atomic.LoadInt64(&activeTask.StopAt)
			if offset+int64(readSoFar) > currentStopAt {
				readSoFar = int(currentStopAt - offset)
				if readSoFar <= 0 {
					return nil // stolen completely
				}
			}

			// ZERO-COPY OPTIMIZATION
			// Instead of copying to a new buffer, we pass the current buffer to the writer.
			// The writer takes ownership and returns it to the pool.
			// We must acquire a new buffer for the next read loop.

			currentBufPtr := *bufPtrWrapper
			currentBuf := *currentBufPtr

			// Send to async writer (pass buffer pointer for return to pool)
			writeReq := WriteRequest{
				Data:   currentBuf[:readSoFar],
				Offset: offset,
				BufPtr: currentBufPtr,
			}

			select {
			case d.writeQueue <- writeReq:
				// Sent successfully - we lost ownership of currentBufPtr
				// Get a NEW buffer for next iteration
				*bufPtrWrapper = d.bufferPool.Get().(*[]byte)

				// Update our local reference for the read loop (although we loop back to top and don't use 'buf' until Read)
				// But wait, the Read call uses 'buf'. We need to update 'buf' variable in the loop?
				// The 'buf' variable in this function comes from... wait.
				// In original code: `buf` was passed as argument.
				// Now we need to update `buf` to point to the new buffer.
				buf = **bufPtrWrapper

			case <-ctx.Done():
				// Context cancelled, we still own the buffer. it will be returned by caller's defer
				return ctx.Err()
			}

			// Check for write errors from previous writes
			if writeErr := d.writeErr.Load(); writeErr != nil {
				return fmt.Errorf("write error: %w", *writeErr)
			}

			now := time.Now()
			oldOffset := offset
			offset += int64(readSoFar)
			atomic.StoreInt64(&activeTask.CurrentOffset, offset)
			atomic.AddInt64(&activeTask.WindowBytes, int64(readSoFar))
			atomic.StoreInt64(&activeTask.LastActivity, now.UnixNano())

			// Update EMA speed using sliding window (2 second window)
			windowElapsed := now.Sub(activeTask.WindowStart).Seconds()
			if windowElapsed >= 2.0 {
				windowBytes := atomic.SwapInt64(&activeTask.WindowBytes, 0)
				recentSpeed := float64(windowBytes) / windowElapsed

				activeTask.SpeedMu.Lock()
				alpha := d.Runtime.GetSpeedEmaAlpha()
				if activeTask.Speed == 0 {
					activeTask.Speed = recentSpeed
				} else {
					activeTask.Speed = (1-alpha)*activeTask.Speed + alpha*recentSpeed
				}
				activeTask.SpeedMu.Unlock()

				activeTask.WindowStart = now // Reset window
			}

			// Update progress via shared state, clamping to StopAt boundary
			// to avoid double-counting bytes when work is stolen
			if d.State != nil {
				currentStopAt := atomic.LoadInt64(&activeTask.StopAt)
				effectiveEnd := offset
				if effectiveEnd > currentStopAt {
					effectiveEnd = currentStopAt
				}
				contributed := effectiveEnd - oldOffset
				if contributed > 0 {
					d.State.Downloaded.Add(contributed)
				}
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

	var bestID int = -1
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

	current := atomic.LoadInt64(&active.CurrentOffset)
	newStopAt := current + splitSize

	// Update the active task stop point
	atomic.StoreInt64(&active.StopAt, newStopAt)

	finalCurrent := atomic.LoadInt64(&active.CurrentOffset)

	// The actual start of the stolen chunk must be after where the worker effectively stops.
	stolenStart := newStopAt
	if finalCurrent > newStopAt {
		stolenStart = finalCurrent
	}

	// Double check: ensure we didn't race and lose the chunk
	currentStopAt := atomic.LoadInt64(&active.StopAt)
	if stolenStart >= currentStopAt && currentStopAt != newStopAt {
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
		utils.ConvertBytesToHumanReadable(stolenTask.Length), bestID, stolenTask.Offset, stolenTask.Offset+stolenTask.Length)

	return true
}
