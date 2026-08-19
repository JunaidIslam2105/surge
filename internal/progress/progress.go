package progress

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

// CfgProgress returns the *DownloadProgress associated with cfg, or
// nil if cfg.ProgressState is nil. This safely narrows the untyped State field.
func CfgProgress(cfg *types.DownloadRecord) *DownloadProgress {
	if cfg == nil || cfg.ProgressState == nil {
		return nil
	}
	dp, _ := cfg.ProgressState.(*DownloadProgress)
	return dp
}

// DownloadProgress is the facade that coordinates all trackers.
type DownloadProgress struct {
	ID string

	Bytes   ByteTracker
	Session SessionTimer
	Bitmap  BitmapTracker

	ActiveWorkers atomic.Int32
	Done          atomic.Bool
	Paused        atomic.Bool
	Pausing       atomic.Bool // Intermediate state: Pause requested but workers not yet exited
	RateLimited   atomic.Bool // Set when the downloader is backing off due to HTTP 429/rate-limit
	Error         atomic.Pointer[error]

	mu         sync.Mutex // Protects metadata only (Mirrors, limits, strings)
	cancelFunc context.CancelFunc

	// pendingResumeState holds a pause-grade snapshot stashed by the
	// concurrent error path so the scheduler can attach it to EventError.
	pendingResumeState *types.DownloadRecord

	destPath     string
	filename     string
	url          string
	mirrors      []types.MirrorStatus
	rateLimit    int64
	rateLimitSet bool
}

func New(id string, totalSize int64) *DownloadProgress {
	dp := &DownloadProgress{
		ID: id,
	}
	dp.Bytes.SetTotalSize(totalSize)
	// Initialize session start
	dp.Session.SyncSessionStart(0)
	return dp
}

func (ps *DownloadProgress) SetDestPath(path string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.destPath = path
}

func (ps *DownloadProgress) GetDestPath() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.destPath
}

func (ps *DownloadProgress) SetFilename(filename string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.filename = filename
}

func (ps *DownloadProgress) GetFilename() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.filename
}

func (ps *DownloadProgress) SetURL(url string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.url = url
}

func (ps *DownloadProgress) GetURL() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.url
}

func (ps *DownloadProgress) SetRateLimit(rate int64, explicit bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.rateLimit = rate
	ps.rateLimitSet = explicit
}

func (ps *DownloadProgress) GetRateLimit() (int64, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.rateLimit, ps.rateLimitSet
}

func (ps *DownloadProgress) SetTotalSize(size int64) {
	if ps.Bytes.TotalSize.Load() == size && !ps.Session.StartTime().IsZero() {
		return
	}
	ps.Bytes.SetTotalSize(size)
	ps.Session.SyncSessionStart(ps.Bytes.VerifiedProgress.Load())
}

func (ps *DownloadProgress) SyncSessionStart() {
	ps.Session.SyncSessionStart(ps.Bytes.VerifiedProgress.Load())
}

func (ps *DownloadProgress) SetError(err error) {
	ps.Error.Store(&err)
}

func (ps *DownloadProgress) GetError() error {
	if e := ps.Error.Load(); e != nil {
		return *e
	}
	return nil
}

// SetPendingResumeState stashes a pause-grade DownloadRecord for lifecycle
// fallback delivery and EventError recovery. Nil-safe; overwrites prior.
func (ps *DownloadProgress) SetPendingResumeState(s *types.DownloadRecord) {
	if ps == nil {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.pendingResumeState = s
}

// TakePendingResumeState returns and clears the pending resume snapshot.
// Nil-safe; a second Take returns nil.
func (ps *DownloadProgress) TakePendingResumeState() *types.DownloadRecord {
	if ps == nil {
		return nil
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s := ps.pendingResumeState
	ps.pendingResumeState = nil
	return s
}

func (ps *DownloadProgress) GetProgress() (downloaded int64, total int64, totalElapsed time.Duration, sessionElapsed time.Duration, connections int32, sessionStartBytes int64) {
	downloaded = ps.Bytes.VerifiedProgress.Load()
	total = ps.Bytes.TotalSize.Load()
	connections = ps.ActiveWorkers.Load()
	paused := ps.Paused.Load()

	sessionElapsed, totalElapsed, sessionStartBytes = ps.Session.GetElapsed(paused)
	return
}

func (ps *DownloadProgress) Pause() {
	ps.Paused.Store(true)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.cancelFunc != nil {
		ps.cancelFunc()
	}
}

func (ps *DownloadProgress) SetCancelFunc(cancel context.CancelFunc) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.cancelFunc = cancel
}

func (ps *DownloadProgress) Resume() {
	ps.Paused.Store(false)
}

func (ps *DownloadProgress) IsPaused() bool {
	return ps.Paused.Load()
}

func (ps *DownloadProgress) SetPausing(pausing bool) {
	ps.Pausing.Store(pausing)
}

func (ps *DownloadProgress) IsPausing() bool {
	return ps.Pausing.Load()
}

func (ps *DownloadProgress) SetSavedElapsed(d time.Duration) {
	ps.Session.SetSavedElapsed(d)
}

func (ps *DownloadProgress) GetSavedElapsed() time.Duration {
	return ps.Session.GetSavedElapsed()
}

func (ps *DownloadProgress) FinalizeSession(downloaded int64) (time.Duration, time.Duration) {
	if downloaded < 0 {
		downloaded = ps.Bytes.VerifiedProgress.Load()
	}

	sessionElapsed, totalElapsed := ps.Session.FinalizeSession(downloaded)

	ps.Bytes.Downloaded.Store(downloaded)
	ps.Bytes.VerifiedProgress.Store(downloaded)

	return sessionElapsed, totalElapsed
}

func (ps *DownloadProgress) SessionReset() {
	ps.Bytes.Downloaded.Store(0)
	ps.Bytes.VerifiedProgress.Store(0)
	ps.ActiveWorkers.Store(0)
	ps.Done.Store(false)
	ps.Paused.Store(false)
	ps.Pausing.Store(false)
	ps.RateLimited.Store(false)
	ps.Error.Store(nil)

	ps.Session.SessionReset()
	ps.Bitmap.Reset()

	ps.mu.Lock()
	defer ps.mu.Unlock()
	for i := range ps.mirrors {
		ps.mirrors[i].Error = false
	}
	ps.pendingResumeState = nil
}

func (ps *DownloadProgress) FinalizePauseSession(downloaded int64) time.Duration {
	_, total := ps.FinalizeSession(downloaded)
	return total
}

func (ps *DownloadProgress) SetMirrors(mirrors []types.MirrorStatus) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.mirrors = make([]types.MirrorStatus, len(mirrors))
	copy(ps.mirrors, mirrors)
}

func (ps *DownloadProgress) GetMirrors() []types.MirrorStatus {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if len(ps.mirrors) == 0 {
		return nil
	}
	mirrors := make([]types.MirrorStatus, len(ps.mirrors))
	copy(mirrors, ps.mirrors)
	return mirrors
}

func (ps *DownloadProgress) InitBitmap(totalSize int64, chunkSize int64) {
	ps.Bitmap.InitBitmap(totalSize, chunkSize)
}

func (ps *DownloadProgress) RestoreBitmap(bitmap []byte, actualChunkSize int64) {
	ps.Bitmap.RestoreBitmap(ps.Bytes.TotalSize.Load(), bitmap, actualChunkSize)
}

func (ps *DownloadProgress) SetChunkProgress(progress []int64) {
	ps.Bitmap.SetChunkProgress(progress)
}

func (ps *DownloadProgress) SetChunkState(index int, status types.ChunkStatus) {
	ps.Bitmap.SetChunkState(index, status)
}

func (ps *DownloadProgress) GetChunkState(index int) types.ChunkStatus {
	return ps.Bitmap.GetChunkState(index)
}

func (ps *DownloadProgress) UpdateChunkStatus(offset, length int64, status types.ChunkStatus) {
	increment := ps.Bitmap.UpdateChunkStatus(ps.Bytes.TotalSize.Load(), offset, length, status)
	if increment > 0 {
		ps.Bytes.VerifiedProgress.Add(increment)
	}
}

func (ps *DownloadProgress) RecalculateProgress(remainingTasks []types.Task) {
	totalVerified := ps.Bitmap.RecalculateProgress(ps.Bytes.TotalSize.Load(), remainingTasks)
	ps.Bytes.VerifiedProgress.Store(totalVerified)
}

func (ps *DownloadProgress) GetBitmap() ([]byte, int, int64, int64, []int64) {
	return ps.GetBitmapSnapshot(true)
}

func (ps *DownloadProgress) GetBitmapSnapshot(includeProgress bool) ([]byte, int, int64, int64, []int64) {
	return ps.Bitmap.GetBitmapSnapshot(ps.Bytes.TotalSize.Load(), includeProgress)
}

// GetBitmapVersion returns the snapshot version of the bitmap. It changes
// whenever chunk state is mutated, so callers can cheaply detect that a
// previously computed bitmap snapshot is still current without re-scanning.
func (ps *DownloadProgress) GetBitmapVersion() uint64 {
	if ps == nil {
		return 0
	}
	return ps.Bitmap.snapVersion.Load()
}
