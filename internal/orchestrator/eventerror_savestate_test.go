package orchestrator

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestEventError_SaveState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "surge.db")
	store.Configure(dbPath)
	t.Cleanup(func() { store.CloseDB() })

	mgr := NewLifecycleManager(nil, nil, config.DefaultSettings())
	defer mgr.Shutdown()

	// Seed a master-list entry so the EventError handler has an existing record.
	seedErr := types.DownloadRecord{
		ID:        "err-test",
		URL:       "http://example.com/file.bin",
		URLHash:   store.URLHash("http://example.com/file.bin"),
		DestPath:  filepath.Join(tmpDir, "file.bin"),
		Filename:  "file.bin",
		Status:    "downloading",
		TotalSize: 10 * 1024 * 1024,
	}
	if err := store.AddToMasterList(seedErr); err != nil {
		t.Fatalf("seed AddToMasterList failed: %v", err)
	}

	// Build an EventError with a pause-grade State snapshot.
	snapshot := &types.DownloadRecord{
		ID:           "err-test",
		URL:          "http://example.com/file.bin",
		DestPath:     filepath.Join(tmpDir, "file.bin"),
		TotalSize:    10 * 1024 * 1024,
		Downloaded:   5 * 1024 * 1024,
		Workers:      4,
		MinChunkSize: 2 * 1024 * 1024,
		RateLimit:    0,
		RateLimitSet: false,
	}

	ch := make(chan types.DownloadEvent, 1)
	go mgr.StartEventWorker(ch)

	ch <- types.DownloadEvent{
		Type:       types.EventError,
		DownloadID: "err-test",
		Filename:   "file.bin",
		Err:        types.ErrInsufficientDiskSpace,
		State:      snapshot,
	}

	// Wait for the event worker to process by closing and waiting.
	close(ch)
	// Give the worker a moment to flush. The StartEventWorker loop exits on close.
	// Use a short poll to verify persistence.
	record, err := store.GetDownload("err-test")
	if err != nil {
		t.Fatalf("GetDownload failed: %v", err)
	}
	if record == nil {
		t.Fatal("expected non-nil record after EventError")
	}

	// Retry loop: the worker may not have flushed yet.
	for i := 0; i < 50 && record.Status != "error"; i++ {
		time.Sleep(10 * time.Millisecond)
		record, _ = store.GetDownload("err-test")
	}
	if record == nil || record.Status != "error" {
		t.Fatalf("expected status=error, got %+v", record)
	}

	// Verify the error string was persisted.
	if record.Error != types.ErrInsufficientDiskSpace.Error() {
		t.Errorf("expected Error=%q, got %q", types.ErrInsufficientDiskSpace.Error(), record.Error)
	}

	// Verify the detail state was persisted via SaveStateWithOptions.
	saved, err := store.LoadState("http://example.com/file.bin", filepath.Join(tmpDir, "file.bin"))
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if saved == nil {
		t.Fatal("expected non-nil saved state after EventError")
	}
	if saved.Downloaded != 5*1024*1024 {
		t.Errorf("expected Downloaded=%d, got %d", 5*1024*1024, saved.Downloaded)
	}
}

func TestEventError_ElapsedMonotonicBump(t *testing.T) {
	tmpDir := t.TempDir()
	store.Configure(filepath.Join(tmpDir, "surge.db"))
	t.Cleanup(func() { store.CloseDB() })

	mgr := NewLifecycleManager(nil, nil, config.DefaultSettings())
	defer mgr.Shutdown()

	url := "http://example.com/elapsed.bin"
	destPath := filepath.Join(tmpDir, "elapsed.bin")

	// Master record has 5s TimeTaken and 100 bytes downloaded.
	store.AddToMasterList(types.DownloadRecord{
		ID:         "err-elapsed",
		URL:        url,
		URLHash:    store.URLHash(url),
		DestPath:   destPath,
		Filename:   "elapsed.bin",
		Status:     "downloading",
		TotalSize:  1000,
		Downloaded: 100,
		TimeTaken:  5000, // 5s already on master
	})

	// Snapshot advanced Downloaded to 400 but Elapsed is only 1s — below
	// the master's candidateElapsed (5000ms). The bump must raise Elapsed
	// to 5001ms so TimeTaken stays monotonic.
	snapshot := &types.DownloadRecord{
		URL:        url,
		ID:         "err-elapsed",
		DestPath:   destPath,
		TotalSize:  1000,
		Downloaded: 400,
		Elapsed:    int64(time.Second), // 1s — below master candidateElapsed
		Tasks:      []types.Task{{Offset: 400, Length: 600}},
		Filename:   "elapsed.bin",
	}

	ch := make(chan types.DownloadEvent, 1)
	go mgr.StartEventWorker(ch)
	ch <- types.DownloadEvent{
		Type:       types.EventError,
		DownloadID: "err-elapsed",
		Filename:   "elapsed.bin",
		DestPath:   destPath,
		Err:        errors.New("boom"),
		State:      snapshot,
	}
	close(ch)

	deadline := time.Now().Add(3 * time.Second)
	var entry *types.DownloadRecord
	for {
		got, err := store.GetDownload("err-elapsed")
		if err == nil && got != nil && got.Status == "error" {
			entry = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Status=error")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Downloaded advanced and Elapsed was ≤ candidate → +1ms bump.
	if entry.TimeTaken < 5001 {
		t.Fatalf("TimeTaken=%d, want >=5001 (monotonic bump vs master)", entry.TimeTaken)
	}

	saved, err := store.LoadState(url, destPath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if saved.Elapsed < int64(5001*time.Millisecond) {
		t.Fatalf("detail Elapsed=%d, want >=5001ms monotonic", saved.Elapsed)
	}
}

func TestEventError_FieldFallbacks(t *testing.T) {
	tmpDir := t.TempDir()
	store.Configure(filepath.Join(tmpDir, "surge.db"))
	t.Cleanup(func() { store.CloseDB() })

	mgr := NewLifecycleManager(nil, nil, config.DefaultSettings())
	defer mgr.Shutdown()

	url := "http://example.com/fallback.bin"
	destPath := filepath.Join(tmpDir, "fallback.bin")

	// Master record has rich metadata that the sparse snapshot will lack.
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:           "err-fallback",
		URL:          url,
		URLHash:      store.URLHash(url),
		DestPath:     destPath,
		Filename:     "fallback.bin",
		Status:       "downloading",
		TotalSize:    2000,
		Downloaded:   500,
		Workers:      8,
		MinChunkSize: 1024,
		RateLimit:    500000,
		RateLimitSet: true,
	}); err != nil {
		t.Fatalf("seed AddToMasterList failed: %v", err)
	}

	seeded, err := store.GetDownload("err-fallback")
	if err != nil {
		t.Fatalf("GetDownload after seed failed: %v", err)
	}
	if seeded == nil {
		t.Fatal("seeded record not found")
	}

	// Sparse snapshot: zero Workers/MinChunkSize, zero TotalSize, zero Downloaded.
	snapshot := &types.DownloadRecord{
		URL:      url,
		ID:       "err-fallback",
		DestPath: destPath,
		Elapsed:  int64(2 * time.Second),
	}

	ch := make(chan types.DownloadEvent, 1)
	go mgr.StartEventWorker(ch)
	ch <- types.DownloadEvent{
		Type:       types.EventError,
		DownloadID: "err-fallback",
		Err:        errors.New("boom"),
		State:      snapshot,
	}
	close(ch)

	deadline := time.Now().Add(3 * time.Second)
	var entry *types.DownloadRecord
	for {
		got, err := store.GetDownload("err-fallback")
		if err == nil && got != nil && got.Status == "error" {
			entry = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Status=error")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if entry.Filename != "fallback.bin" {
		t.Errorf("Filename=%q, want %q", entry.Filename, "fallback.bin")
	}
	if entry.TotalSize != 2000 {
		t.Errorf("TotalSize=%d, want 2000", entry.TotalSize)
	}
	if entry.Downloaded != 500 {
		t.Errorf("Downloaded=%d, want 500 (preserved from master)", entry.Downloaded)
	}
	if entry.Workers != 8 {
		t.Errorf("Workers=%d, want 8", entry.Workers)
	}
	if entry.MinChunkSize != 1024 {
		t.Errorf("MinChunkSize=%d, want 1024", entry.MinChunkSize)
	}
	if !entry.RateLimitSet || entry.RateLimit != 500000 {
		t.Errorf("RateLimit=%d RateLimitSet=%v, want 500000/true", entry.RateLimit, entry.RateLimitSet)
	}
}
