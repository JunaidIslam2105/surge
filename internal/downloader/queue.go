package downloader

import (
	"context"
	"surge/internal/messages"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

const maxDownloads = 3 //We limit the max no of downloads to 3 at a time(XDM does this)

// activeDownload tracks a download that's currently running
type activeDownload struct {
	config DownloadConfig
	cancel context.CancelFunc
}

type WorkerPool struct {
	taskChan   chan DownloadConfig
	progressCh chan<- tea.Msg
	downloads  map[int]*activeDownload // Track active downloads for pause/resume
	mu         sync.RWMutex
}

func NewWorkerPool(progressCh chan<- tea.Msg) *WorkerPool {
	pool := &WorkerPool{
		taskChan:   make(chan DownloadConfig, 100), //We make it buffered to avoid blocking add
		progressCh: progressCh,
		downloads:  make(map[int]*activeDownload),
	}
	for i := 0; i < maxDownloads; i++ {
		go pool.worker()
	}
	return pool
}

func (p *WorkerPool) Add(cfg DownloadConfig) {
	p.taskChan <- cfg
}

// Pause pauses a specific download by ID
func (p *WorkerPool) Pause(downloadID int) {
	p.mu.RLock()
	ad, exists := p.downloads[downloadID]
	p.mu.RUnlock()

	if !exists || ad == nil {
		return
	}

	// Set paused flag and cancel context
	if ad.config.State != nil {
		ad.config.State.Pause()
	}

	// Send pause message
	if p.progressCh != nil {
		downloaded := int64(0)
		if ad.config.State != nil {
			downloaded = ad.config.State.Downloaded.Load()
		}
		p.progressCh <- messages.DownloadPausedMsg{
			DownloadID: downloadID,
			Downloaded: downloaded,
		}
	}
}

// PauseAll pauses all active downloads (for graceful shutdown)
func (p *WorkerPool) PauseAll() {
	p.mu.RLock()
	ids := make([]int, 0, len(p.downloads))
	for id, ad := range p.downloads {
		// Only pause downloads that are actually active (not already paused or done)
		if ad != nil && ad.config.State != nil && !ad.config.State.IsPaused() && !ad.config.State.Done.Load() {
			ids = append(ids, id)
		}
	}
	p.mu.RUnlock()

	for _, id := range ids {
		p.Pause(id)
	}
}

// Cancel cancels and removes a download by ID
func (p *WorkerPool) Cancel(downloadID int) {
	p.mu.Lock()
	ad, exists := p.downloads[downloadID]
	if exists {
		delete(p.downloads, downloadID)
	}
	p.mu.Unlock()

	if !exists || ad == nil {
		return
	}

	// Cancel the context to stop workers
	if ad.cancel != nil {
		ad.cancel()
	}

	// Mark as done to stop polling
	if ad.config.State != nil {
		ad.config.State.Done.Store(true)
	}
}

// Resume resumes a paused download by ID
func (p *WorkerPool) Resume(downloadID int) {
	p.mu.RLock()
	ad, exists := p.downloads[downloadID]
	p.mu.RUnlock()

	if !exists || ad == nil {
		return
	}

	// Clear paused flag
	if ad.config.State != nil {
		ad.config.State.Resume()
	}

	// Re-queue the download
	p.Add(ad.config)

	// Send resume message
	if p.progressCh != nil {
		p.progressCh <- messages.DownloadResumedMsg{
			DownloadId: downloadID,
		}
	}
}

func (p *WorkerPool) worker() {
	for cfg := range p.taskChan {
		// Create cancellable context
		ctx, cancel := context.WithCancel(context.Background())

		// Register active download
		ad := &activeDownload{
			config: cfg,
			cancel: cancel,
		}
		p.mu.Lock()
		p.downloads[cfg.ID] = ad
		p.mu.Unlock()

		err := TUIDownload(ctx, cfg)

		// Check if this was a pause (not an error)
		isPaused := cfg.State != nil && cfg.State.IsPaused()

		if err != nil && !isPaused {
			if cfg.State != nil {
				cfg.State.SetError(err)
			}
			if p.progressCh != nil {
				p.progressCh <- messages.DownloadErrorMsg{DownloadID: cfg.ID, Err: err}
			}
		} else if !isPaused {
			// Only mark as done if not paused
			if cfg.State != nil {
				cfg.State.Done.Store(true)
			}
			// Note: DownloadCompleteMsg is sent by the progress reporter when it detects Done=true

			// Clean up from tracking
			p.mu.Lock()
			delete(p.downloads, cfg.ID)
			p.mu.Unlock()
		}
		// If paused, we keep it in downloads map for potential resume
	}
}
