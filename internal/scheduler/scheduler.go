package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// activeDownload tracks a download that's currently running
type activeDownload struct {
	config types.DownloadRecord
	cancel context.CancelFunc
	// running is true while the worker goroutine is executing RunDownload for this config.
	running atomic.Bool
	// done is closed by the worker when it stops (running becomes false).
	// Callers waiting for the worker to exit should select on this channel.
	done chan struct{}
}

// queuedTask wraps a download config with SQS-style visibility and retry state
type queuedTask struct {
	cfg      types.DownloadRecord
	retries  int
	inFlight bool
}

// Scheduler manages the download workers and tasks.
//
// Lock Ordering:
// If a code path must acquire both the pool's main mutex (p.mu) and a limiter's internal
// mutex (limiter.mu, via limiter.SetRate() or limiter.WaitN()), it MUST acquire p.mu first.
// Examples: Add, SetDownloadRateLimit, SetDefaultDownloadRateLimit.
// Never acquire p.mu while holding a limiter's internal mutex to prevent deadlocks.
type Scheduler struct {
	taskCond       *sync.Cond
	queueOrder     []string
	progressCh     chan<- types.DownloadEvent
	progressDone   chan struct{}              // closed when progressCh must no longer be sent to
	downloads      map[string]*activeDownload // Track active downloads for pause/resume
	queued         map[string]*queuedTask     // Track queued downloads
	mu             sync.RWMutex
	wg             sync.WaitGroup // We use this to wait for all active downloads to pause before exiting the program
	maxDownloads   int
	isShuttingDown bool

	globalLimiter               *transport.RateLimiter
	downloadLimiters            map[string]*transport.RateLimiter
	defaultDownloadRateLimitBps int64
	shutdownOnce                sync.Once
}

var (
	// gracefulShutdownPauseSoftTimeout controls when we emit a warning that
	// pausing is taking longer than expected. It is intentionally soft; shutdown
	// continues waiting for durable pause persistence.
	gracefulShutdownPauseSoftTimeout = 10 * time.Second
	// gracefulShutdownPausePollInterval controls how often shutdown rechecks pause state.
	gracefulShutdownPausePollInterval = 100 * time.Millisecond
	// gracefulShutdownPauseHardTimeout prevents indefinite shutdown hangs if a worker is stuck.
	gracefulShutdownPauseHardTimeout = 30 * time.Second
	// The timeout/poll interval vars below are kept for graceful-shutdown use only.
	// cancelStopWaitTimeout bounds how long Cancel waits for an active worker to exit.
	cancelStopWaitTimeout = 3 * time.Second
)

func New(progressCh chan<- types.DownloadEvent, maxDownloads int) *Scheduler {
	if maxDownloads < 1 {
		maxDownloads = 3 // Default to 3 if invalid
	}
	pool := &Scheduler{
		progressCh:       progressCh,
		progressDone:     make(chan struct{}),
		downloads:        make(map[string]*activeDownload),
		queued:           make(map[string]*queuedTask),
		maxDownloads:     maxDownloads,
		globalLimiter:    transport.NewRateLimiter(0, 0),
		downloadLimiters: make(map[string]*transport.RateLimiter),
	}
	pool.taskCond = sync.NewCond(&pool.mu)
	for i := 0; i < maxDownloads; i++ {
		go pool.worker()
	}
	return pool
}

// removeQueueOrderLocked removes an ID from queueOrder preserving the order of other elements.
func (p *Scheduler) removeQueueOrderLocked(id string) {
	for i, qid := range p.queueOrder {
		if qid == id {
			p.queueOrder = append(p.queueOrder[:i], p.queueOrder[i+1:]...)
			return
		}
	}
}

// syncConfigFromState syncs Filename, DestPath, and Mirrors from the associated state.
func syncConfigFromState(cfg *types.DownloadRecord) {
	if cfg.ProgressState == nil {
		return
	}
	if fn := progress.CfgProgress(cfg).GetFilename(); fn != "" {
		cfg.Filename = fn
	}
	if dp := progress.CfgProgress(cfg).GetDestPath(); dp != "" {
		cfg.DestPath = dp
	}
	if ms := progress.CfgProgress(cfg).GetMirrors(); len(ms) > 0 {
		var urls []string
		for _, m := range ms {
			urls = append(urls, m.URL)
		}
		cfg.Mirrors = urls
	}
	if _, totalSize, _, _, _, _ := progress.CfgProgress(cfg).GetProgress(); totalSize > 0 {
		cfg.TotalSize = totalSize
	}
}

// resolveDestPath resolves the destination path consistently from config, state, and output bounds.
func resolveDestPath(cfg *types.DownloadRecord) string {
	destPath := cfg.DestPath
	if destPath == "" && cfg.ProgressState != nil {
		destPath = progress.CfgProgress(cfg).GetDestPath()
	}
	if destPath == "" && cfg.OutputPath != "" && cfg.Filename != "" {
		destPath = filepath.Join(cfg.OutputPath, cfg.Filename)
	}
	if destPath == "" {
		destPath = cfg.OutputPath // default fallback
	}
	return destPath
}

// Add adds a new download task to the pool. The caller (LifecycleManager) is
// responsible for emitting any lifecycle events (e.g. DownloadQueuedMsg).
func (p *Scheduler) Add(cfg types.DownloadRecord) {
	if cfg.ProgressCh == nil {
		cfg.ProgressCh = p.progressCh
	}
	p.mu.Lock()
	p.ensureLimiterForConfigLocked(&cfg)
	qt := &queuedTask{cfg: cfg}
	p.queued[cfg.ID] = qt
	p.queueOrder = append(p.queueOrder, cfg.ID)
	p.wg.Add(1)
	p.taskCond.Signal()
	p.mu.Unlock()
}

func (p *Scheduler) ensureLimiterForConfigLocked(cfg *types.DownloadRecord) {
	if cfg == nil || cfg.ID == "" {
		return
	}

	if p.globalLimiter == nil {
		p.globalLimiter = transport.NewRateLimiter(0, 0)
	}
	if p.downloadLimiters == nil {
		p.downloadLimiters = make(map[string]*transport.RateLimiter)
	}

	// If state already carries an explicit rate, prefer it over cfg default.
	if cfg.ProgressState != nil {
		if stateRate, stateSet := progress.CfgProgress(cfg).GetRateLimit(); stateSet {
			cfg.RateLimit = stateRate
			cfg.RateLimitSet = true
		}
	}

	rate := cfg.RateLimit
	if !cfg.RateLimitSet {
		rate = p.defaultDownloadRateLimitBps
		cfg.RateLimit = rate
	}
	if cfg.ProgressState != nil {
		progress.CfgProgress(cfg).SetRateLimit(rate, cfg.RateLimitSet)
	}

	limiter := p.downloadLimiters[cfg.ID]
	if limiter == nil {
		limiter = transport.NewRateLimiter(rate, rateLimiterBurst(rate))
		p.downloadLimiters[cfg.ID] = limiter
	} else {
		limiter.SetRate(rate, rateLimiterBurst(rate))
	}

	if cfg.Limiter == nil {
		cfg.Limiter = transport.NewMultiLimiter(p.globalLimiter, limiter)
	}
}

func rateLimiterBurst(rate int64) int64 {
	if rate <= 0 {
		return 0
	}
	return rate
}

// HasDownload reports whether a download with the given URL is currently active or queued in the pool.
func (p *Scheduler) HasDownload(url string) bool {
	p.mu.RLock()
	for _, ad := range p.downloads {
		if ad.config.URL == url {
			p.mu.RUnlock()
			return true
		}
	}
	for _, qd := range p.queued {
		if qd.cfg.URL == url {
			p.mu.RUnlock()
			return true
		}
	}
	p.mu.RUnlock()

	return false
}

// ActiveCount returns the number of currently active (downloading/pausing) downloads
func (p *Scheduler) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, ad := range p.downloads {
		// Count if not completed and not fully paused
		if ad.config.ProgressState != nil && !progress.CfgProgress(&ad.config).Done.Load() && !progress.CfgProgress(&ad.config).IsPaused() {
			count++
		}
	}
	// Also count queued
	count += len(p.queued)
	return count
}

// GetAll returns all active download configs (for listing)
func (p *Scheduler) GetAll() []types.DownloadRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()

	configs := make([]types.DownloadRecord, 0, len(p.downloads)+len(p.queued))
	for _, ad := range p.downloads {
		cfg := ad.config
		cfg.Limiter = nil
		syncConfigFromState(&cfg)
		configs = append(configs, cfg)
	}
	for _, id := range p.queueOrder {
		if qd, ok := p.queued[id]; ok {
			cfg := qd.cfg
			cfg.Limiter = nil
			configs = append(configs, cfg)
		}
	}
	return configs
}

// Pause pauses a specific download by ID. Returns true if found and pause initiated
// (or already paused), false otherwise. Pure mechanical operation - no events emitted.
func (p *Scheduler) Pause(downloadID string) bool {
	p.mu.RLock()
	ad, exists := p.downloads[downloadID]
	p.mu.RUnlock()

	if !exists || ad == nil {
		return false
	}

	// Set paused flag and cancel context
	if ad.config.ProgressState != nil {
		// Idempotency: If already paused, do nothing.
		if progress.CfgProgress(&ad.config).IsPaused() {
			return true
		}
		// If transition is already in progress, still ensure worker context is canceled.
		if progress.CfgProgress(&ad.config).IsPausing() {
			if ad.cancel != nil {
				ad.cancel()
			}
			return true
		}
		progress.CfgProgress(&ad.config).SetPausing(true) // Mark as transitioning to pause
		progress.CfgProgress(&ad.config).Pause()
	}
	// Always cancel worker context as a safety net (single downloader does not set state cancel itself).
	if ad.cancel != nil {
		ad.cancel()
	}

	// Send pause message is now exclusively handled by worker return paths
	// to ensure fully synchronized byte counts.
	return true
}

// SetGlobalRateLimit updates the global rate limiter (bytes/sec). Use 0 to disable.
func (p *Scheduler) SetGlobalRateLimit(rate int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.globalLimiter == nil {
		p.globalLimiter = transport.NewRateLimiter(0, 0)
	}
	// All per-download MultiLimiters hold a pointer to this globalLimiter,
	// so updating the rate here propagates to all active downloads instantly.
	p.globalLimiter.SetRate(rate, rateLimiterBurst(rate))
}

// SetDefaultDownloadRateLimit updates the default per-download rate limit (bytes/sec).
func (p *Scheduler) SetDefaultDownloadRateLimit(rate int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.defaultDownloadRateLimitBps = rate
	if p.downloadLimiters == nil {
		p.downloadLimiters = make(map[string]*transport.RateLimiter)
	}

	for _, id := range p.queueOrder {
		if qd, ok := p.queued[id]; ok {
			cfg := qd.cfg
			if cfg.RateLimitSet {
				continue
			}
			cfg.RateLimit = rate
			qd.cfg = cfg
			if cfg.ProgressState != nil {
				progress.CfgProgress(&cfg).SetRateLimit(rate, false)
			}
			limiter := p.downloadLimiters[id]
			if limiter == nil {
				p.downloadLimiters[id] = transport.NewRateLimiter(rate, rateLimiterBurst(rate))
			} else {
				limiter.SetRate(rate, rateLimiterBurst(rate))
			}
		}
	}
	for id, ad := range p.downloads {
		if ad.config.RateLimitSet {
			continue
		}
		ad.config.RateLimit = rate
		if ad.config.ProgressState != nil {
			progress.CfgProgress(&ad.config).SetRateLimit(rate, false)
		}
		limiter := p.downloadLimiters[id]
		if limiter == nil {
			// Note: ensureLimiterForConfigLocked guarantees all active/queued downloads have a limiter.
			// This nil branch is defensive and should be unreachable in practice.
			p.downloadLimiters[id] = transport.NewRateLimiter(rate, rateLimiterBurst(rate))
		} else {
			limiter.SetRate(rate, rateLimiterBurst(rate))
		}
	}
}

// SetDownloadRateLimit updates a specific download's rate limit (bytes/sec).
func (p *Scheduler) SetDownloadRateLimit(downloadID string, rate int64) bool {
	if downloadID == "" {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	found := false
	if ad, ok := p.downloads[downloadID]; ok {
		ad.config.RateLimit = rate
		ad.config.RateLimitSet = true
		if ad.config.ProgressState != nil {
			progress.CfgProgress(&ad.config).SetRateLimit(rate, true)
		}
		found = true
	}
	if qt, ok := p.queued[downloadID]; ok {
		qt.cfg.RateLimit = rate
		qt.cfg.RateLimitSet = true
		if qt.cfg.ProgressState != nil {
			progress.CfgProgress(&qt.cfg).SetRateLimit(rate, true)
		}
		found = true
	}

	if !found {
		return false
	}

	if p.downloadLimiters == nil {
		p.downloadLimiters = make(map[string]*transport.RateLimiter)
	}
	limiter := p.downloadLimiters[downloadID]
	if limiter == nil {
		p.downloadLimiters[downloadID] = transport.NewRateLimiter(rate, rateLimiterBurst(rate))
	} else {
		limiter.SetRate(rate, rateLimiterBurst(rate))
	}
	return true
}

// ClearDownloadRateLimit removes a specific download's override so it inherits the current default.
func (p *Scheduler) ClearDownloadRateLimit(downloadID string) bool {
	if downloadID == "" {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	defaultRate := p.defaultDownloadRateLimitBps

	found := false
	if ad, ok := p.downloads[downloadID]; ok {
		ad.config.RateLimit = defaultRate
		ad.config.RateLimitSet = false
		if ad.config.ProgressState != nil {
			progress.CfgProgress(&ad.config).SetRateLimit(defaultRate, false)
		}
		found = true
	}
	if qt, ok := p.queued[downloadID]; ok {
		qt.cfg.RateLimit = defaultRate
		qt.cfg.RateLimitSet = false
		if qt.cfg.ProgressState != nil {
			progress.CfgProgress(&qt.cfg).SetRateLimit(defaultRate, false)
		}
		found = true
	}

	if !found {
		return false
	}

	if p.downloadLimiters == nil {
		p.downloadLimiters = make(map[string]*transport.RateLimiter)
	}
	limiter := p.downloadLimiters[downloadID]
	if limiter == nil {
		p.downloadLimiters[downloadID] = transport.NewRateLimiter(defaultRate, rateLimiterBurst(defaultRate))
	} else {
		limiter.SetRate(defaultRate, rateLimiterBurst(defaultRate))
	}
	return true
}

// PauseAll pauses all active downloads (for graceful shutdown)
func (p *Scheduler) PauseAll() {
	p.mu.RLock()
	ids := make([]string, 0, len(p.downloads)) // This stores the uuids of the downloads to be paused
	for id, ad := range p.downloads {
		// Only pause downloads that are actually active (not already paused or done or pausing)
		if ad != nil && ad.config.ProgressState != nil && !progress.CfgProgress(&ad.config).IsPaused() && !progress.CfgProgress(&ad.config).Done.Load() && !progress.CfgProgress(&ad.config).IsPausing() {
			ids = append(ids, id)
		}
	}
	p.mu.RUnlock()

	for _, id := range ids {
		p.Pause(id)
	}
}

// Cancel cancels and removes a download by ID. Returns metadata about what was
// removed so the caller (LifecycleManager) can emit events and handle cleanup.
// No events are emitted by the pool itself.
func (p *Scheduler) Cancel(downloadID string) types.CancelResult {
	p.mu.Lock()
	ad, activeExists := p.downloads[downloadID]
	qCfg, queuedExists := p.queued[downloadID]
	if activeExists {
		delete(p.downloads, downloadID)
	}

	var droppedQueued bool
	if queuedExists {
		delete(p.queued, downloadID)
		if !qCfg.inFlight {
			droppedQueued = true
		}
	}
	if activeExists || queuedExists {
		delete(p.downloadLimiters, downloadID)
	}
	p.mu.Unlock()

	if droppedQueued {
		p.wg.Done()
	}

	if !activeExists && !queuedExists {
		return types.CancelResult{}
	}

	result := types.CancelResult{Found: true, WasQueued: queuedExists && !activeExists}

	if activeExists && ad != nil {
		result.Filename = ad.config.Filename
		result.DestPath = resolveDestPath(&ad.config)
		result.Completed = ad.config.ProgressState != nil && progress.CfgProgress(&ad.config).Done.Load()

		// Cancel the context to stop workers
		if ad.cancel != nil {
			ad.cancel()
		}

		// Best effort: wait for worker to exit so delete cleanup doesn't race with
		// downloader startup that can recreate the .surge file after removal.
		if ad.running.Load() {
			deadline := time.NewTimer(cancelStopWaitTimeout)
			select {
			case <-ad.done:
			case <-deadline.C:
			}
			deadline.Stop()
		}

		// Mark as done to stop polling
		if ad.config.ProgressState != nil {
			progress.CfgProgress(&ad.config).Done.Store(true)
		}
	} else if queuedExists {
		result.Filename = qCfg.cfg.Filename
		result.DestPath = resolveDestPath(&qCfg.cfg)
	}

	return result
}

// ExtractPausedConfig atomically removes a paused download from the pool and returns
// its config (with state cleared for re-enqueue) so the LifecycleManager can resume it.
// Returns nil if the download is not found, not paused, or still transitioning (pausing).
func (p *Scheduler) ExtractPausedConfig(downloadID string) *types.DownloadRecord {
	p.mu.Lock()
	ad, exists := p.downloads[downloadID]
	if !exists || ad == nil {
		p.mu.Unlock()
		return nil
	}

	// Cannot extract if still pausing or not actually paused
	if ad.config.ProgressState == nil || !progress.CfgProgress(&ad.config).IsPaused() || progress.CfgProgress(&ad.config).IsPausing() {
		p.mu.Unlock()
		return nil
	}

	// Sync latest filename/path/mirrors from live state before handing off
	syncConfigFromState(&ad.config)

	cfg := ad.config
	delete(p.downloads, downloadID)
	delete(p.downloadLimiters, downloadID)
	p.mu.Unlock()

	cfg.Limiter = nil
	if cfg.ProgressState != nil {
		progress.CfgProgress(&cfg).Resume()
	}
	return &cfg
}

// UpdateURL updates the in-memory URL of a download by ID.
// The caller (LifecycleManager) is responsible for persisting the change to the DB.
// It fails if the download is actively downloading (not paused or errored).
func (p *Scheduler) UpdateURL(downloadID string, newURL string) error {
	p.mu.Lock()
	ad, exists := p.downloads[downloadID]
	_, qExists := p.queued[downloadID]

	if qExists {
		p.mu.Unlock()
		return types.ErrQueuedUpdate
	}

	if exists && ad != nil {
		if ad.config.ProgressState != nil && !progress.CfgProgress(&ad.config).IsPaused() {
			if ad.running.Load() {
				p.mu.Unlock()
				return types.ErrActiveUpdate
			}
		}
		ad.config.URL = newURL
		if ad.config.ProgressState != nil {
			progress.CfgProgress(&ad.config).SetURL(newURL)
		}
	}
	p.mu.Unlock()

	return nil
}

func (p *Scheduler) waitForTask() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	for {
		if p.isShuttingDown {
			return ""
		}
		for _, id := range p.queueOrder {
			if qt, ok := p.queued[id]; ok && !qt.inFlight {
				qt.inFlight = true
				return id
			}
		}
		p.taskCond.Wait()
	}
}

func (p *Scheduler) worker() {
	for {
		id := p.waitForTask()
		if id == "" {
			return
		}

		// Create cancellable context
		ctx, cancel := context.WithCancel(context.Background())

		p.mu.Lock()
		qt, stillQueued := p.queued[id]
		if !stillQueued {
			p.mu.Unlock()
			cancel()
			p.wg.Done()
			continue
		}

		// Ensure Runtime is initialized before exposing to GetAll
		if qt.cfg.Runtime == nil {
			qt.cfg.Runtime = types.DefaultRuntimeConfig()
		}

		// Register active download
		ad := &activeDownload{
			config: qt.cfg,
			cancel: cancel,
			done:   make(chan struct{}),
		}
		if ad.config.ProgressState != nil {
			progress.CfgProgress(&ad.config).SetCancelFunc(cancel)
		}
		ad.running.Store(true)

		ad.config = qt.cfg
		delete(p.queued, id)
		p.removeQueueOrderLocked(id)
		p.downloads[id] = ad

		// Make a local copy for RunDownload to mutate safely
		localCfg := ad.config
		p.mu.Unlock()

		err := RunDownload(ctx, &localCfg)
		ad.running.Store(false)
		close(ad.done) // unblock any Cancel caller waiting for this worker to exit

		// Sync back mutated fields cleanly under lock
		p.mu.Lock()
		if _, exists := p.downloads[localCfg.ID]; exists {
			ad.config.TotalSize = localCfg.TotalSize
			ad.config.SupportsRange = localCfg.SupportsRange
			ad.config.Runtime = localCfg.Runtime
		}
		p.mu.Unlock()

		// Logic:
		// 1. If Pause() was called: State.IsPaused() is true. We keep the task in p.downloads (so it can be resumed).
		// 2. If finished/error: We remove from p.downloads.

		isPaused := localCfg.ProgressState != nil && progress.CfgProgress(&localCfg).IsPaused()

		// Clear "Pausing" transition state now that worker has exited
		if localCfg.ProgressState != nil {
			progress.CfgProgress(&localCfg).SetPausing(false)
		}

		if isPaused {
			utils.Debug("Scheduler: Download %s paused cleanly", localCfg.ID)
			// The concurrent downloader sends DownloadPausedMsg itself via handlePause()
			// (which causes RunDownload to return nil). When a single-threaded download is
			// paused, RunDownload returns a non-nil error, and the pool must fill the gap.
			if err != nil && localCfg.ProgressCh != nil {
				var downloaded int64
				var rateLimit int64
				var rateLimitSet bool
				var workers int
				var minChunkSize int64
				if localCfg.ProgressState != nil {
					downloaded = progress.CfgProgress(&localCfg).Bytes.Downloaded.Load()
				}
				if localCfg.Runtime != nil {
					workers = localCfg.Runtime.Workers
					minChunkSize = localCfg.Runtime.MinChunkSize
				}
				rateLimit, rateLimitSet = localCfg.RateLimit, localCfg.RateLimitSet
				safeSendProgress(localCfg.ProgressCh, types.DownloadEvent{
					Type:         types.EventPaused,
					DownloadID:   localCfg.ID,
					Filename:     localCfg.Filename,
					Downloaded:   downloaded,
					RateLimit:    rateLimit,
					RateLimitSet: rateLimitSet,
					Workers:      workers,
					MinChunkSize: minChunkSize,
				}, p.progressDone)
			}
		} else if err != nil {
			p.mu.Lock()
			delete(p.downloads, localCfg.ID)

			if shouldRetryFailedDownload(p.isShuttingDown, err, qt.retries) {
				qt.retries++
				qt.inFlight = true // Keep in-flight while sending progress outside lock
				qt.cfg = localCfg
				p.queued[localCfg.ID] = qt

				p.removeQueueOrderLocked(localCfg.ID)
				p.queueOrder = append(p.queueOrder, localCfg.ID)

				p.mu.Unlock()

				if localCfg.ProgressCh != nil {
					safeSendProgress(localCfg.ProgressCh, types.DownloadEvent{
						Type:         types.EventQueued,
						DownloadID:   localCfg.ID,
						Filename:     localCfg.Filename,
						URL:          localCfg.URL,
						DestPath:     localCfg.DestPath,
						Mirrors:      localCfg.Mirrors,
						RateLimit:    localCfg.RateLimit,
						RateLimitSet: localCfg.RateLimitSet,
						Workers:      localCfg.Workers,
						MinChunkSize: localCfg.MinChunkSize,
					}, p.progressDone)
				}

				p.mu.Lock()
				if _, ok := p.queued[localCfg.ID]; ok {
					qt.inFlight = false
					p.queued[localCfg.ID] = qt
				} else {
					// GracefulShutdown or Cancel removed it while we were unlocked.
					// Since we were in-flight, they didn't decrement wg, so we must.
					p.wg.Done()
				}
				p.taskCond.Signal()
				p.mu.Unlock()
				// ponytail: naive backoff blocks worker thread, but naturally limits retry storms
				select {
				case <-time.After(time.Second * time.Duration(qt.retries)):
				case <-p.progressDone:
					// Wake up early if shutting down
				}
				continue
			}

			// All retries exhausted (or error is permanent/canceled): collect event
			// metadata under the lock, then send outside it so a full progress
			// channel cannot block the scheduler mutex.
			isCancel := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
			var errEvent *types.DownloadEvent
			if !isCancel && localCfg.ProgressCh != nil {
				var finalDestPath string
				var finalFilename string
				if localCfg.ProgressState != nil {
					p := progress.CfgProgress(&localCfg)
					finalDestPath = p.GetDestPath()
					finalFilename = p.GetFilename()
				}
				if finalDestPath == "" {
					finalDestPath = resolveDestPath(&localCfg)
				}
				if finalFilename == "" {
					finalFilename = localCfg.Filename
				}
				errEvent = &types.DownloadEvent{
					Type:       types.EventError,
					DownloadID: localCfg.ID,
					Filename:   finalFilename,
					DestPath:   finalDestPath,
					Err:        err,
				}
				if localCfg.ProgressState != nil {
					if pending := progress.CfgProgress(&localCfg).TakePendingResumeState(); pending != nil {
						errEvent.State = pending
						if pending.Downloaded > 0 {
							errEvent.Downloaded = pending.Downloaded
						}
					}
				}
			}

			if !isCancel && localCfg.ProgressState != nil {
				progress.CfgProgress(&localCfg).SetError(err)
			}
			delete(p.downloadLimiters, localCfg.ID)
			p.mu.Unlock()

			// Send outside the lock: safeSendProgress may block on a full
			// channel and must not hold p.mu while doing so.
			if errEvent != nil {
				safeSendProgress(localCfg.ProgressCh, *errEvent, p.progressDone)
			}
		} else {
			// Only mark as done if not paused
			if localCfg.ProgressState != nil {
				progress.CfgProgress(&localCfg).Done.Store(true)
			}
			// Note: DownloadCompleteMsg is sent by the progress reporter when it detects Done=true

			// Clean up from tracking
			p.mu.Lock()
			delete(p.downloads, localCfg.ID)
			delete(p.downloadLimiters, localCfg.ID)
			p.mu.Unlock()
		}
		// If paused, we keep it in downloads map for potential resume
		p.wg.Done()
	}
}

// shouldRetryFailedDownload reports whether a failed download should be
// requeued for another attempt. Cancel, deadline, permanent HTTP, and
// disk-full errors are excluded — retrying them is pointless.
func shouldRetryFailedDownload(isShuttingDown bool, err error, retries int) bool {
	if isShuttingDown {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if types.IsPermanentHTTPError(err) {
		return false
	}
	if types.IsInsufficientDiskSpace(err) {
		return false
	}
	return retries < 10
}

// GetStatus returns the status of an active download
func (p *Scheduler) GetStatus(id string) *types.DownloadStatus {
	var adURL, adFilename, adDestPath string
	var adRateLimitBps int64
	var adRateLimitSet bool
	var adState *progress.DownloadProgress

	p.mu.RLock()
	ad, exists := p.downloads[id]
	qCfg, qExists := p.queued[id]
	if exists {
		adURL = ad.config.URL
		adFilename = ad.config.Filename
		adDestPath = ad.config.DestPath
		adRateLimitBps = ad.config.RateLimit
		adRateLimitSet = ad.config.RateLimitSet
		adState = progress.CfgProgress(&ad.config)
	}
	p.mu.RUnlock()

	if !exists && !qExists {
		return nil
	}

	if qExists {
		return &types.DownloadStatus{
			ID:           id,
			URL:          qCfg.cfg.URL,
			Filename:     qCfg.cfg.Filename,
			DestPath:     resolveDestPath(&qCfg.cfg),
			Status:       "queued",
			Downloaded:   0,
			TotalSize:    0, // Metadata not yet fetched
			RateLimit:    qCfg.cfg.RateLimit,
			RateLimitSet: qCfg.cfg.RateLimitSet,
		}
	}

	state := adState
	if state == nil {
		return nil
	}

	// Use state filename/destpath if available (thread-safe)
	filename := adFilename
	if str := state.GetFilename(); str != "" {
		filename = str
	}

	// Calculate progress and speed (thread-safe)
	downloaded, totalSize, _, sessionElapsed, _, sessionStart := state.GetProgress()

	status := &types.DownloadStatus{
		ID:           id,
		URL:          adURL,
		Filename:     filename,
		TotalSize:    totalSize,
		Downloaded:   downloaded,
		Status:       "downloading",
		RateLimit:    adRateLimitBps,
		RateLimitSet: adRateLimitSet,
	}
	if dp := state.GetDestPath(); dp != "" {
		status.DestPath = dp
	} else {
		status.DestPath = adDestPath
	}

	if state.IsPausing() {
		status.Status = "pausing"
	} else if state.IsPaused() {
		status.Status = "paused"
	} else if state.Done.Load() {
		status.Status = "completed"
	}

	if err := state.GetError(); err != nil {
		status.Status = "error"
		status.Error = err.Error()
	}

	// Calculate progress
	if status.TotalSize > 0 {
		status.Progress = float64(status.Downloaded) * 100 / float64(status.TotalSize)
	}

	// Calculate speed (bytes/s) only for active downloads.
	if status.Status == "downloading" {
		sessionDownloaded := downloaded - sessionStart
		if sessionElapsed.Seconds() > 0 && sessionDownloaded > 0 {
			bytesPerSec := float64(sessionDownloaded) / sessionElapsed.Seconds()
			status.Speed = bytesPerSec
		}
	}

	return status
}

// GracefulShutdown pauses all downloads and waits for them to save state
func (p *Scheduler) GracefulShutdown() {
	p.shutdownOnce.Do(func() {
		p.PauseAll()

		// Discard all queued-but-not-yet-started downloads so that idle workers
		// do not pick them up and begin downloading after shutdown is initiated.
		// Workers already guard against this with the p.queued check at loop entry,
		// so clearing the map here is sufficient; draining taskChan is belt-and-suspenders.
		p.mu.Lock()
		for id, qt := range p.queued {
			delete(p.queued, id)
			if !qt.inFlight {
				p.wg.Done()
			}
		}
		p.queueOrder = nil
		p.isShuttingDown = true
		p.taskCond.Broadcast()
		p.mu.Unlock()

		// Wait for any downloads in "Pausing" state to finish transitioning
		// This ensures we don't exit while a database write is pending/active
		ticker := time.NewTicker(gracefulShutdownPausePollInterval)
		defer ticker.Stop()
		start := time.Now()
		warned := false

		for {
			p.mu.Lock()
			stillPausing := false
			for _, ad := range p.downloads {
				if ad.config.ProgressState != nil && progress.CfgProgress(&ad.config).IsPausing() {
					// If no worker is running this download anymore, pausing is stale.
					// Normalize it so shutdown can proceed.
					if !ad.running.Load() {
						progress.CfgProgress(&ad.config).SetPausing(false)
						continue
					}
					stillPausing = true
					break
				}
			}
			p.mu.Unlock()

			if !stillPausing {
				break
			}

			if !warned && time.Since(start) >= gracefulShutdownPauseSoftTimeout {
				utils.Debug("GracefulShutdown: downloads still pausing after %v, continuing to wait for durable pause", gracefulShutdownPauseSoftTimeout)
				warned = true
			}
			if time.Since(start) >= gracefulShutdownPauseHardTimeout {
				utils.Debug("GracefulShutdown: forcing exit from pausing wait after hard timeout %v", gracefulShutdownPauseHardTimeout)
				break
			}
			<-ticker.C
		}

		// Signal that progressCh must no longer be sent to, so that
		// safeSendProgress calls in workers can abort if the channel is full.
		// This must happen before wg.Wait() to break deadlocks where a worker
		// is blocked on a full channel and preventing wg from completing.
		close(p.progressDone)

		p.wg.Wait() // Blocks until all workers call Done()
	})
}
