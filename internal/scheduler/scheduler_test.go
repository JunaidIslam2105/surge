package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestNew(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	if pool == nil {
		t.Fatal("Expected non-nil Scheduler")
	}

	if pool.taskCond == nil {
		t.Error("Expected taskCond to be initialized")
	}

	if pool.progressCh != ch {
		t.Error("Expected progressCh to be set correctly")
	}

	if pool.downloads == nil {
		t.Error("Expected downloads map to be initialized")
	}

	if pool.maxDownloads != 3 {
		t.Errorf("Expected maxDownloads=3, got %d", pool.maxDownloads)
	}
}

func TestNewScheduler_MaxDownloadsValidation(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)

	tests := []struct {
		name         string
		maxDownloads int
		wantMax      int
	}{
		{"zero defaults to 3", 0, 3},
		{"negative defaults to 3", -1, 3},
		{"valid value 1", 1, 1},
		{"valid value 5", 5, 5},
		{"valid value 10", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := New(ch, tt.maxDownloads)
			if pool.maxDownloads != tt.wantMax {
				t.Errorf("maxDownloads = %d, want %d", pool.maxDownloads, tt.wantMax)
			}
		})
	}
}

func TestNewScheduler_NilChannel(t *testing.T) {
	pool := New(nil, 3)

	if pool == nil {
		t.Fatal("Expected non-nil Scheduler even with nil channel")
	}

	if pool.progressCh != nil {
		t.Error("Expected progressCh to be nil")
	}
}

func TestScheduler_Add_QueuesToChannel(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	cfg := types.DownloadRecord{
		ID:            "test-id",
		URL:           "http://example.com/file.zip",
		ProgressState: progress.New("test-id", 1000),
	}

	// Add should not block (buffered channel)
	done := make(chan bool)
	go func() {
		pool.Add(cfg)
		done <- true
	}()

	select {
	case <-done:
		// Success - Add completed
	case <-time.After(100 * time.Millisecond):
		t.Error("Add() blocked unexpectedly")
	}
}

func TestScheduler_Pause_NonExistentDownload(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Should not panic when pausing non-existent download
	pool.Pause("non-existent-id")

	// No message should be sent
	select {
	case <-ch:
		t.Error("Should not send message for non-existent download")
	default:
		// Expected - no message
	}
}

func TestScheduler_Pause_ActiveDownload(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Create a progress state
	state := progress.New("test-id", 1000)
	state.Bytes.Downloaded.Store(500)
	state.Bytes.VerifiedProgress.Store(700)

	// Manually add an active download
	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	pool.Pause("test-id")

	// Check that state is paused
	if !state.IsPaused() {
		t.Error("Expected state to be marked as paused")
	}
}

func TestScheduler_Pause_NilState(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)
	canceled := make(chan struct{}, 1)

	// Add download with nil state
	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: nil,
		},
		cancel: func() {
			select {
			case canceled <- struct{}{}:
			default:
			}
		},
	}
	pool.mu.Unlock()

	// Should not panic with nil state
	pool.Pause("test-id")

	select {
	case <-canceled:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected pause to cancel worker context")
	}
}

func TestScheduler_PauseAll_NoDownloads(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Should not panic with no downloads
	pool.PauseAll()

	// No messages should be sent
	select {
	case <-ch:
		t.Error("Should not send message when no downloads exist")
	default:
		// Expected
	}
}

func TestScheduler_PauseAll_MultipleDownloads(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Add multiple active downloads
	states := make([]*progress.DownloadProgress, 3)
	for i := 0; i < 3; i++ {
		id := string(rune('a' + i))
		states[i] = progress.New(id, 1000)
		pool.mu.Lock()
		pool.downloads[id] = &activeDownload{
			config: types.DownloadRecord{
				ID:            id,
				ProgressState: states[i],
			},
		}
		pool.mu.Unlock()
	}

	pool.PauseAll()

	// All should be paused
	for i, state := range states {
		if !state.IsPaused() {
			t.Errorf("Download %d should be paused", i)
		}
	}
}

func TestScheduler_PauseAll_SkipsAlreadyPaused(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Add one paused and one active download
	activeState := progress.New("active", 1000)
	pausedState := progress.New("paused", 1000)
	pausedState.Paused.Store(true)

	pool.mu.Lock()
	pool.downloads["active"] = &activeDownload{
		config: types.DownloadRecord{ID: "active", ProgressState: activeState},
	}
	pool.downloads["paused"] = &activeDownload{
		config: types.DownloadRecord{ID: "paused", ProgressState: pausedState},
	}
	pool.mu.Unlock()

	pool.PauseAll()
}

func TestScheduler_PauseAll_SkipsCompletedDownloads(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Add one completed and one active download
	activeState := progress.New("active", 1000)
	doneState := progress.New("done", 1000)
	doneState.Done.Store(true)

	pool.mu.Lock()
	pool.downloads["active"] = &activeDownload{
		config: types.DownloadRecord{ID: "active", ProgressState: activeState},
	}
	pool.downloads["done"] = &activeDownload{
		config: types.DownloadRecord{ID: "done", ProgressState: doneState},
	}
	pool.mu.Unlock()

	pool.PauseAll()
}

func TestScheduler_Cancel_NonExistentDownload(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Should not panic
	pool.Cancel("non-existent-id")
}

func TestScheduler_Cancel_RemovesFromMap(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("test-id", 1000)

	pool.mu.Lock()
	ad := &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	ad.running.Store(true)
	pool.downloads["test-id"] = ad
	pool.mu.Unlock()
	pool.mu.Lock()
	pool.downloadLimiters["test-id"] = pool.globalLimiter
	pool.mu.Unlock()

	result := pool.Cancel("test-id")

	if !result.Found {
		t.Error("Expected CancelResult.Found to be true")
	}

	// Pool must NOT emit any event - that's the caller's responsibility
	select {
	case msg := <-ch:
		t.Errorf("Pool should not emit events on cancel, got %T", msg)
	default:
		// expected
	}

	pool.mu.RLock()
	_, exists := pool.downloads["test-id"]
	pool.mu.RUnlock()

	if exists {
		t.Error("Expected download to be removed from map after cancel")
	}

	pool.mu.Lock()
	_, limiterExists := pool.downloadLimiters["test-id"]
	pool.mu.Unlock()
	if limiterExists {
		t.Error("Expected download limiter to be removed after cancel")
	}
}

func TestScheduler_Cancel_CallsCancelFunc(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	ctx, cancel := context.WithCancel(context.Background())
	state := progress.New("test-id", 1000)

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
		cancel: cancel,
	}
	pool.mu.Unlock()

	result := pool.Cancel("test-id")
	if !result.Found {
		t.Error("Expected CancelResult.Found")
	}

	// No event should be emitted by pool
	select {
	case msg := <-ch:
		t.Errorf("Pool should not emit events on cancel, got %T", msg)
	default:
		// expected
	}

	// Context should be canceled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Expected context to be canceled")
	}
}

func TestScheduler_Cancel_MarksDone(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("test-id", 1000)

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	result := pool.Cancel("test-id")
	if !result.Found {
		t.Error("Expected CancelResult.Found")
	}

	if !state.Done.Load() {
		t.Error("Expected state.Done to be true after cancel")
	}
}

func TestScheduler_Cancel_DoesNotRemoveIncompleteFile(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "cancel.bin")
	incompletePath := destPath + types.IncompleteSuffix
	if err := os.WriteFile(incompletePath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("failed to create .surge file: %v", err)
	}

	state := progress.New("test-id", 1000)
	state.SetDestPath(destPath)

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	result := pool.Cancel("test-id")
	if !result.Found {
		t.Fatal("expected cancel to find download")
	}

	if _, err := os.Stat(incompletePath); err != nil {
		t.Fatalf("expected .surge file to remain for centralized delete cleanup, stat err: %v", err)
	}
}

func TestScheduler_Cancel_QueuedDownload_RemovesFromQueueAndReturnsResult(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := &Scheduler{
		progressCh: ch,
		downloads:  make(map[string]*activeDownload),
		queued: map[string]*queuedTask{
			"queued-id": {
				cfg: types.DownloadRecord{
					ID:       "queued-id",
					Filename: "queued.bin",
				},
			},
		},
	}
	pool.taskCond = sync.NewCond(&pool.mu)
	pool.wg.Add(1) // match the queued task

	result := pool.Cancel("queued-id")

	pool.mu.RLock()
	_, exists := pool.queued["queued-id"]
	pool.mu.RUnlock()
	if exists {
		t.Fatal("expected queued download to be removed from queue")
	}

	if !result.Found {
		t.Fatal("expected CancelResult.Found for queued cancel")
	}
	if !result.WasQueued {
		t.Fatal("expected CancelResult.WasQueued for queued cancel")
	}
	if result.Filename != "queued.bin" {
		t.Fatalf("result.Filename = %q, want queued.bin", result.Filename)
	}

	// Pool must NOT emit events - caller handles that
	select {
	case msg := <-ch:
		t.Fatalf("pool should not emit events on cancel, got %T", msg)
	default:
		// expected
	}
}

// Resume orchestration (hot/cold path, DB hydration, event emission) was promoted to
// LifecycleManager so the pool remains a pure executor with no knowledge of persistence
// or types. Tests for pool-level extraction live below; LifecycleManager integration
// tests live in internal/processing/manager_test.go (see TestLifecycleManager_Cancel_NotFound).

func TestScheduler_GracefulShutdown_PausesAll(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("test-id", 1000)

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	// GracefulShutdown should call PauseAll
	// PauseAll will set IsPausing() = true
	// GracefulShutdown waits for IsPausing() = false
	// We verify that PauseAll was called by checking state.IsPausing()
	// Then we clear it to unblock shutdown

	done := make(chan bool)
	stopCheck := make(chan struct{})
	defer close(stopCheck)
	go func() {
		// Wait for PauseAll to be called (Pausing=true)
		for {
			select {
			case <-stopCheck:
				return
			default:
				if state.IsPausing() {
					state.SetPausing(false)
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	go func() {
		pool.GracefulShutdown()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("GracefulShutdown took too long")
	}

	if !state.IsPaused() {
		t.Error("Expected state to be paused after GracefulShutdown")
	}
}

func TestScheduler_GracefulShutdown_WaitsPastSoftTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ch := make(chan types.DownloadEvent, 10)
		pool := New(ch, 1)

		ps := progress.New("wait-test-id", 1000)
		pool.mu.Lock()
		ad := &activeDownload{config: types.DownloadRecord{ID: "wait-test-id", ProgressState: ps}}
		ad.running.Store(true)
		pool.downloads["wait-test-id"] = ad
		pool.mu.Unlock()

		origSoftTimeout := gracefulShutdownPauseSoftTimeout
		origPollInterval := gracefulShutdownPausePollInterval
		origHardTimeout := gracefulShutdownPauseHardTimeout
		gracefulShutdownPauseSoftTimeout = 30 * time.Millisecond
		gracefulShutdownPausePollInterval = 5 * time.Millisecond
		gracefulShutdownPauseHardTimeout = 5 * time.Second
		defer func() {
			gracefulShutdownPauseSoftTimeout = origSoftTimeout
			gracefulShutdownPausePollInterval = origPollInterval
			gracefulShutdownPauseHardTimeout = origHardTimeout
		}()

		done := make(chan struct{})
		go func() {
			pool.GracefulShutdown()
			close(done)
		}()
		synctest.Wait()
		if !ps.IsPausing() {
			t.Fatal("expected graceful shutdown to set pausing=true")
		}

		time.Sleep(gracefulShutdownPauseSoftTimeout + 20*time.Millisecond)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("GracefulShutdown returned before pausing was cleared")
		default:
		}

		ps.SetPausing(false)
		time.Sleep(gracefulShutdownPausePollInterval)
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("GracefulShutdown did not return after pausing was cleared")
		}
	})
}

func TestScheduler_GracefulShutdown_ClearsStalePausingWithoutWorker(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 1)

	ps := progress.New("stale-pausing-id", 1000)
	ps.Pause()
	ps.SetPausing(true)

	pool.mu.Lock()
	pool.downloads["stale-pausing-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "stale-pausing-id",
			ProgressState: ps,
		},
	}
	pool.mu.Unlock()

	origPollInterval := gracefulShutdownPausePollInterval
	origHardTimeout := gracefulShutdownPauseHardTimeout
	gracefulShutdownPausePollInterval = 5 * time.Millisecond
	gracefulShutdownPauseHardTimeout = 2 * time.Second
	defer func() {
		gracefulShutdownPausePollInterval = origPollInterval
		gracefulShutdownPauseHardTimeout = origHardTimeout
	}()

	done := make(chan struct{})
	go func() {
		pool.GracefulShutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("GracefulShutdown should not block on stale pausing state")
	}

	if ps.IsPausing() {
		t.Fatal("expected stale pausing flag to be cleared during shutdown")
	}
}

func TestScheduler_ConcurrentPauseCancel(t *testing.T) {
	ch := make(chan types.DownloadEvent, 100)
	pool := New(ch, 3)

	// Add multiple downloads
	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		state := progress.New(id, 1000)
		pool.mu.Lock()
		pool.downloads[id] = &activeDownload{
			config: types.DownloadRecord{ID: id, ProgressState: state},
		}
		pool.mu.Unlock()
	}

	// Concurrently pause and cancel
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		id := string(rune('a' + i))
		go func(id string) {
			defer wg.Done()
			pool.Pause(id)
			pool.Cancel(id)
		}(id)
	}

	wg.Wait()

	// All should be removed from map
	pool.mu.RLock()
	remaining := len(pool.downloads)
	pool.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("Expected 0 remaining downloads, got %d", remaining)
	}
}

func TestScheduler_HasDownload(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// 1. Test Active Download
	activeURL := "http://example.com/active.zip"
	pool.mu.Lock()
	pool.downloads["active"] = &activeDownload{
		config: types.DownloadRecord{
			ID:  "active",
			URL: activeURL,
		},
	}
	pool.mu.Unlock()

	if !pool.HasDownload(activeURL) {
		t.Error("Expected HasDownload to return true for active download")
	}

	// 2. Test Non-Existent Download
	if pool.HasDownload("http://example.com/missing.zip") {
		t.Error("Expected HasDownload to return false for missing download")
	}

	// For now, this unit test covers the memory-check part of HasDownload which was the critical logic add.
}

// --- ExtractPausedConfig Tests (replaces old pool.Resume tests) ---

func TestScheduler_ExtractPausedConfig_NonExistent(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	// Should return nil for non-existent download
	if cfg := pool.ExtractPausedConfig("non-existent-id"); cfg != nil {
		t.Errorf("Expected nil for non-existent download, got %+v", cfg)
	}
}

func TestScheduler_ExtractPausedConfig_WhilePausing(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("test-id", 1000)
	state.Paused.Store(true)
	state.SetPausing(true)

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	// Should return nil - still pausing (not safe to extract)
	if cfg := pool.ExtractPausedConfig("test-id"); cfg != nil {
		t.Fatal("Expected nil while still pausing")
	}

	// Download must still be in pool
	pool.mu.RLock()
	_, exists := pool.downloads["test-id"]
	pool.mu.RUnlock()
	if !exists {
		t.Error("Expected download to remain in pool while pausing")
	}
}

func TestScheduler_ExtractPausedConfig_NotPaused(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("test-id", 1000)
	// NOT paused

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	if cfg := pool.ExtractPausedConfig("test-id"); cfg != nil {
		t.Fatal("Expected nil for non-paused download")
	}
}

func TestScheduler_ExtractPausedConfig_Success(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("test-id", 1000)
	state.Paused.Store(true)
	state.SetDestPath("/tmp/final.bin")
	state.SetFilename("final.bin")
	staleLimiter := transport.NewMultiLimiter(pool.globalLimiter, transport.NewRateLimiter(1024, rateLimiterBurst(1024)))

	pool.mu.Lock()
	pool.downloads["test-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "test-id",
			URL:           "http://example.com/file.zip",
			Filename:      "stale.bin",
			ProgressState: state,
			Limiter:       staleLimiter,
		},
	}
	pool.mu.Unlock()
	pool.mu.Lock()
	pool.downloadLimiters["test-id"] = pool.globalLimiter
	pool.mu.Unlock()

	cfg := pool.ExtractPausedConfig("test-id")
	if cfg == nil {
		t.Fatal("Expected config to be returned")
	}

	// State sync: filename and destpath must come from live state
	if cfg.Filename != "final.bin" {
		t.Errorf("Filename = %q, want final.bin", cfg.Filename)
	}
	if cfg.DestPath != "/tmp/final.bin" {
		t.Errorf("DestPath = %q, want /tmp/final.bin", cfg.DestPath)
	}
	if cfg.Limiter != nil {
		t.Error("Expected limiter to be cleared so resume installs a fresh tracked limiter")
	}

	// Download must be removed from pool
	pool.mu.RLock()
	_, exists := pool.downloads["test-id"]
	pool.mu.RUnlock()
	if exists {
		t.Error("Expected download to be removed from pool after extract")
	}

	pool.mu.Lock()
	_, limiterExists := pool.downloadLimiters["test-id"]
	pool.mu.Unlock()
	if limiterExists {
		t.Error("Expected download limiter to be removed from pool after extract")
	}

	// Pause state should be cleared
	if state.IsPaused() {
		t.Error("Expected pause state to be cleared after extract")
	}

	// No events emitted by pool
	select {
	case msg := <-ch:
		t.Errorf("Pool should not emit events on ExtractPausedConfig, got %T", msg)
	default:
		// expected
	}
}

func TestScheduler_PauseResume_Idempotency(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	state := progress.New("idempotent-test", 1000)

	pool.mu.Lock()
	pool.downloads["idempotent-test"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "idempotent-test",
			ProgressState: state,
		},
	}
	pool.mu.Unlock()

	// 1. First Pause
	pool.Pause("idempotent-test")

	// Should be Pausing
	if !state.IsPausing() {
		t.Error("Expected state to be Pausing after first Pause")
	}

	// 2. Second Pause (Idempotent)
	pool.Pause("idempotent-test")

	// Manually transition to Paused (simulating worker finish)
	state.SetPausing(false)
	state.Pause()

	// 3. ExtractPausedConfig (replaces Resume)
	cfg := pool.ExtractPausedConfig("idempotent-test")
	if cfg == nil {
		t.Fatal("Expected config to be extracted after true pause")
	}
	if state.IsPaused() {
		t.Error("Expected state to be cleared after extract")
	}

	// 4. Second ExtractPausedConfig (idempotent - already extracted)
	if cfg2 := pool.ExtractPausedConfig("idempotent-test"); cfg2 != nil {
		t.Error("Expected nil on second extract (already removed from pool)")
	}
}

func TestScheduler_GetStatus_IncludesDestPath(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 1)

	destPath := "/tmp/status-dest.bin"
	st := progress.New("status-id", 1024)
	st.SetDestPath(destPath)

	pool.mu.Lock()
	pool.downloads["status-id"] = &activeDownload{
		config: types.DownloadRecord{
			ID:            "status-id",
			URL:           "https://example.com/file.bin",
			ProgressState: st,
		},
	}
	pool.mu.Unlock()

	got := pool.GetStatus("status-id")
	if got == nil {
		t.Fatal("expected status, got nil")
	}
	if got.DestPath != destPath {
		t.Fatalf("dest_path = %q, want %q", got.DestPath, destPath)
	}
}

func TestScheduler_UpdateURL(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	pool := New(ch, 3)

	activeState := progress.New("active-id", 1000)
	pool.mu.Lock()
	ad := &activeDownload{
		config: types.DownloadRecord{
			ID:            "active-id",
			URL:           "http://example.com/old.zip",
			ProgressState: activeState,
		},
	}
	ad.running.Store(true)
	pool.downloads["active-id"] = ad
	pool.mu.Unlock()

	// 1. Try updating a running download - should fail
	err := pool.UpdateURL("active-id", "http://example.com/new.zip")
	if err == nil {
		t.Error("Expected error when updating URL for active download")
	}

	// 2. Try updating a paused download - pool only updates in-memory (no DB)
	activeState.Paused.Store(true)
	ad.running.Store(false)

	err = pool.UpdateURL("active-id", "http://example.com/new.zip")
	if err != nil {
		t.Errorf("Expected no error for paused download, got %v", err)
	}

	// Verify in-memory URL was updated
	pool.mu.RLock()
	gotURL := pool.downloads["active-id"].config.URL
	pool.mu.RUnlock()
	if gotURL != "http://example.com/new.zip" {
		t.Errorf("in-memory URL not updated: got %q", gotURL)
	}

	// 3. Try updating a queued download - should fail
	pool.mu.Lock()
	pool.queued["queued-id"] = &queuedTask{cfg: types.DownloadRecord{ID: "queued-id"}}
	pool.mu.Unlock()

	err = pool.UpdateURL("queued-id", "http://example.com/new.zip")
	if err == nil || err.Error() != "cannot update URL for a queued download, please cancel or wait for it to start" {
		t.Errorf("Expected queued error, got %v", err)
	}
}

// Note: UpdateURL DB persistence is now tested in internal/processing tests
// since LifecycleManager.UpdateURL() is responsible for calling store.UpdateURL().

// --- GracefulShutdown: queued download discard tests ---

// TestScheduler_GracefulShutdown_ClearsQueuedMap verifies that GracefulShutdown
// removes all entries from p.queued so that idle workers skip any items they
// drain from taskChan after shutdown has started.
func TestScheduler_GracefulShutdown_ClearsQueuedMap(t *testing.T) {
	ch := make(chan types.DownloadEvent, 10)
	// Use a pool with no workers so nothing auto-starts.
	pool := &Scheduler{
		progressCh:   ch,
		progressDone: make(chan struct{}),
		downloads:    make(map[string]*activeDownload),
		queued:       make(map[string]*queuedTask),
		maxDownloads: 0,
	}
	pool.taskCond = sync.NewCond(&pool.mu)

	// Seed the queued map directly (simulating Add() without a live worker).
	pool.mu.Lock()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("queued-%d", i)
		pool.queued[id] = &queuedTask{cfg: types.DownloadRecord{ID: id, URL: "http://example.com/file.zip"}}
		pool.wg.Add(1)
	}
	pool.mu.Unlock()

	done := make(chan struct{})
	go func() {
		pool.GracefulShutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("GracefulShutdown hung with no active downloads")
	}

	pool.mu.RLock()
	remaining := len(pool.queued)
	pool.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("expected queued map to be empty after GracefulShutdown, got %d entries", remaining)
	}
}

// TestScheduler_GracefulShutdown_WorkerSkipsQueuedAfterShutdown confirms the
// worker-side guard: a worker that pulls a cfg from taskChan after shutdown has
// cleared p.queued will skip the item without starting a download.
func TestScheduler_GracefulShutdown_WorkerSkipsQueuedAfterShutdown(t *testing.T) {
	ch := make(chan types.DownloadEvent, 50)
	// Single worker, small taskChan.
	pool := New(ch, 1)

	// Add via public API to ensure correct internal state (WaitGroup, queued map).
	id := "skip-me"
	pool.Add(types.DownloadRecord{ID: id})

	// Shutdown should drain the queue and close taskChan.
	done := make(chan struct{})
	go func() {
		pool.GracefulShutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("GracefulShutdown timed out \u2014 worker may have started a download it should have skipped")
	}

	// The queued map must be empty.
	pool.mu.RLock()
	_, stillQueued := pool.queued[id]
	pool.mu.RUnlock()
	if stillQueued {
		t.Error("expected queued map to be cleared after GracefulShutdown")
	}
}

func TestWorker_SkipsRetriesOnPermanentError(t *testing.T) {
	ch := make(chan types.DownloadEvent, 100)
	pool := New(ch, 1)

	// A server that returns a 403 Forbidden, which should trigger ErrPermanentHTTP
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	id := "test-permanent-error"
	pool.Add(types.DownloadRecord{
		ID:            id,
		URL:           server.URL,
		ProgressState: progress.New(id, 0),
		Runtime:       &types.RuntimeConfig{},
	})

	// Since max retries is 3, if it were to retry, it would take multiple attempts.
	// But because 403 is a permanent error, it should fail immediately and emit exactly ONE EventError.
	timeout := time.After(3 * time.Second)
	errorCount := 0

	for {
		select {
		case msg := <-ch:
			if msg.Type == types.EventError {
				errorCount++
				if !types.IsPermanentHTTPError(msg.Err) {
					t.Errorf("expected error to be permanent HTTP error, got: %v", msg.Err)
				}
			}
		case <-timeout:
			t.Fatal("timed out waiting for worker to finish")
		}

		pool.mu.RLock()
		_, inActive := pool.downloads[id]
		_, inQueue := pool.queued[id]
		pool.mu.RUnlock()

		if !inActive && !inQueue && errorCount > 0 {
			break // Worker finished processing the download
		}
	}

	if errorCount != 1 {
		t.Errorf("expected exactly 1 EventError for permanent failure, got %d", errorCount)
	}
}
