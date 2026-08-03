package orchestrator

import (
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
