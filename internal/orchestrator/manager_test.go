package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestLifecycleManager_Settings(t *testing.T) {
	mgr := NewLifecycleManager(nil, nil, nil)
	defer mgr.Shutdown()

	s := mgr.GetSettings()
	if s == nil {
		t.Fatal("expected default settings, got nil")
	}

	newSettings := config.DefaultSettings()
	newSettings.Network.MaxConcurrentProbes.Value = 10
	mgr.ApplySettings(newSettings)

	s2 := mgr.GetSettings()
	if s2.Network.MaxConcurrentProbes.Value != 10 {
		t.Errorf("expected MaxConcurrentProbes to be 10, got %v", s2.Network.MaxConcurrentProbes.Value)
	}
}

func TestLifecycleManager_EventErrorPersistsMessage(t *testing.T) {
	testutil.SetupStateDB(t)

	const downloadID = "error-persistence-id"
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:           downloadID,
		URL:          "https://example.com/stale.bin",
		DestPath:     "/tmp/stale.bin",
		Status:       "downloading",
		Filename:     "stale.bin",
		TotalSize:    100,
		Downloaded:   10,
		RateLimit:    10,
		RateLimitSet: true,
	}); err != nil {
		t.Fatalf("failed to seed download: %v", err)
	}

	mgr := NewLifecycleManager(nil, nil, nil)
	events := make(chan types.DownloadEvent, 1)
	done := make(chan struct{})
	go func() {
		mgr.StartEventWorker(events)
		close(done)
	}()

	events <- types.DownloadEvent{
		Type:         types.EventError,
		DownloadID:   downloadID,
		URL:          "https://example.com/failed.bin",
		DestPath:     "/tmp/failed.bin",
		Filename:     "failed.bin",
		Total:        4096,
		Downloaded:   2048,
		RateLimit:    128,
		RateLimitSet: true,
		Err:          errors.New("connection reset by peer"),
	}
	close(events)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event worker did not stop")
	}

	entry, err := store.GetDownload(downloadID)
	if err != nil {
		t.Fatalf("failed to load persisted download: %v", err)
	}
	if entry == nil {
		t.Fatal("persisted download missing")
	}
	if entry.Status != "error" {
		t.Errorf("status = %q, want %q", entry.Status, "error")
	}
	if entry.Error != "connection reset by peer" {
		t.Errorf("error = %q, want %q", entry.Error, "connection reset by peer")
	}
	if entry.URL != "https://example.com/failed.bin" {
		t.Errorf("url = %q, want %q", entry.URL, "https://example.com/failed.bin")
	}
	if entry.DestPath != "/tmp/failed.bin" || entry.Filename != "failed.bin" {
		t.Errorf("destination = %q/%q, want /tmp/failed.bin/failed.bin", entry.DestPath, entry.Filename)
	}
	if entry.TotalSize != 4096 || entry.Downloaded != 2048 {
		t.Errorf("progress = %d/%d, want 2048/4096", entry.Downloaded, entry.TotalSize)
	}
	if entry.RateLimit != 128 || !entry.RateLimitSet {
		t.Errorf("rate limit = %d (set=%v), want 128 (set=true)", entry.RateLimit, entry.RateLimitSet)
	}
}

func TestLifecycleManager_EventErrorCreatesMissingRecord(t *testing.T) {
	testutil.SetupStateDB(t)

	const downloadID = "missing-error-record-id"
	mgr := NewLifecycleManager(nil, nil, nil)
	events := make(chan types.DownloadEvent, 1)
	done := make(chan struct{})
	go func() {
		mgr.StartEventWorker(events)
		close(done)
	}()

	events <- types.DownloadEvent{
		Type:         types.EventError,
		DownloadID:   downloadID,
		URL:          "https://example.com/failed.bin",
		Filename:     "failed.bin",
		DestPath:     "/tmp/failed.bin",
		Total:        4096,
		Downloaded:   2048,
		RateLimit:    128,
		RateLimitSet: true,
		Err:          errors.New("connection refused"),
	}
	close(events)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event worker did not stop")
	}

	entry, err := store.GetDownload(downloadID)
	if err != nil {
		t.Fatalf("failed to load persisted download: %v", err)
	}
	if entry == nil {
		t.Fatal("persisted download missing")
	}
	if entry.Status != "error" {
		t.Errorf("status = %q, want %q", entry.Status, "error")
	}
	if entry.Error != "connection refused" {
		t.Errorf("error = %q, want %q", entry.Error, "connection refused")
	}
	if entry.URL != "https://example.com/failed.bin" {
		t.Errorf("url = %q, want %q", entry.URL, "https://example.com/failed.bin")
	}
	if entry.TotalSize != 4096 || entry.Downloaded != 2048 {
		t.Errorf("progress = %d/%d, want 2048/4096", entry.Downloaded, entry.TotalSize)
	}
	if entry.RateLimit != 128 || !entry.RateLimitSet {
		t.Errorf("rate limit = %d (set=%v), want 128 (set=true)", entry.RateLimit, entry.RateLimitSet)
	}
}

func TestLifecycleManager_EventCompletePersistsFinalizationError(t *testing.T) {
	testutil.SetupStateDB(t)

	const downloadID = "finalization-error-id"
	destPath := filepath.Join(t.TempDir(), "completed.bin")
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:       downloadID,
		URL:      "https://example.com/completed.bin",
		DestPath: destPath,
		Filename: "completed.bin",
		Status:   "downloading",
	}); err != nil {
		t.Fatalf("failed to seed download: %v", err)
	}

	originalRename := renameCompletedFile
	renameCompletedFile = func(_, _ string) error {
		return errors.New("forced finalization failure")
	}
	t.Cleanup(func() {
		renameCompletedFile = originalRename
	})

	mgr := NewLifecycleManager(nil, nil, nil)
	events := make(chan types.DownloadEvent, 1)
	done := make(chan struct{})
	go func() {
		mgr.StartEventWorker(events)
		close(done)
	}()

	events <- types.DownloadEvent{
		Type:       types.EventComplete,
		DownloadID: downloadID,
		Filename:   "completed.bin",
	}
	close(events)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event worker did not stop")
	}

	entry, err := store.GetDownload(downloadID)
	if err != nil {
		t.Fatalf("failed to load persisted download: %v", err)
	}
	if entry == nil {
		t.Fatal("persisted download missing")
	}
	if entry.Status != "error" {
		t.Errorf("status = %q, want %q", entry.Status, "error")
	}
	if entry.Error != "forced finalization failure" {
		t.Errorf("error = %q, want %q", entry.Error, "forced finalization failure")
	}
}

func TestLifecycleManager_EnqueueSuccess(t *testing.T) {
	// Create a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	progressCh := make(chan types.DownloadEvent, 10)
	pool := scheduler.New(progressCh, 1)
	eb := NewEventBus()
	mgr := NewLifecycleManager(pool, eb, nil)
	defer mgr.Shutdown()

	destDir := t.TempDir()

	req := &DownloadRequest{
		URL:      ts.URL + "/testfile.txt",
		Filename: "testfile.txt",
		Path:     destDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	id, finalName, err := mgr.Enqueue(ctx, req)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty ID")
	}
	if finalName != "testfile.txt" {
		t.Errorf("expected testfile.txt, got %s", finalName)
	}

	// Verify working file was created
	surgePath := filepath.Join(destDir, finalName) + types.IncompleteSuffix
	if _, err := os.Stat(surgePath); os.IsNotExist(err) {
		t.Errorf("expected working file to be created at %s", surgePath)
	}

	// Verify DownloadQueuedMsg was published
	sub, cleanup := eb.Subscribe()
	defer cleanup()

	// Wait a moment for async event to be broadcasted if any, though Enqueue synchronously calls eb.Publish
	// We need to check if the event reached the subscriber.
	found := false
	timeout := time.After(500 * time.Millisecond)
	for !found {
		select {
		case <-sub:
			found = true
		case <-timeout:
			t.Fatal("timed out waiting for DownloadQueuedMsg")
		}
	}
}

func TestLifecycleManager_EnqueueWithID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	progressCh := make(chan types.DownloadEvent, 10)
	pool := scheduler.New(progressCh, 1)
	eb := NewEventBus()
	mgr := NewLifecycleManager(pool, eb, nil)
	defer mgr.Shutdown()

	destDir := t.TempDir()

	req := &DownloadRequest{
		URL:      ts.URL + "/test.zip",
		Filename: "test.zip",
		Path:     destDir,
	}

	customID := "my-custom-uuid-1234"
	id, _, err := mgr.EnqueueWithID(context.Background(), req, customID)
	if err != nil {
		t.Fatalf("EnqueueWithID failed: %v", err)
	}

	if id != customID {
		t.Errorf("expected custom ID %s, got %s", customID, id)
	}
}

func TestLifecycleManager_IsNameActive(t *testing.T) {
	activeFunc := func(dir, name string) bool {
		return name == "active.txt"
	}

	mgr := NewLifecycleManager(nil, nil, nil, activeFunc)

	if !mgr.IsNameActive("/tmp", "active.txt") {
		t.Error("expected true for active.txt")
	}
	if mgr.IsNameActive("/tmp", "other.txt") {
		t.Error("expected false for other.txt")
	}
}

func TestLifecycleManager_EnqueueInvalid(t *testing.T) {
	mgr := NewLifecycleManager(nil, nil, nil)

	// Missing Pool
	_, _, err := mgr.Enqueue(context.Background(), &DownloadRequest{URL: "http://example.com", Path: "/tmp"})
	if !errors.Is(err, types.ErrServiceUnavailable) {
		t.Errorf("expected ErrServiceUnavailable, got %v", err)
	}

	pool := scheduler.New(make(chan types.DownloadEvent, 1), 1)
	mgr = NewLifecycleManager(pool, nil, nil)
	defer mgr.Shutdown()

	// Missing URL
	_, _, err = mgr.Enqueue(context.Background(), &DownloadRequest{Path: "/tmp"})
	if !errors.Is(err, types.ErrURLRequired) {
		t.Errorf("expected ErrURLRequired, got %v", err)
	}

	// Missing Path
	_, _, err = mgr.Enqueue(context.Background(), &DownloadRequest{URL: "http://example.com"})
	if !errors.Is(err, types.ErrDestRequired) {
		t.Errorf("expected ErrDestRequired, got %v", err)
	}
}

func TestLifecycleManager_ResumeBatch_CorruptStateIgnored(t *testing.T) {
	// Setup store
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "surge.db")
	store.Configure(dbPath)
	t.Cleanup(func() { store.CloseDB() })

	// Add two entries to master list
	entry1 := types.DownloadRecord{
		ID:       "id-valid",
		URL:      "http://example.com/valid",
		DestPath: filepath.Join(tmpDir, "valid"),
		Status:   "paused",
	}
	entry2 := types.DownloadRecord{
		ID:       "id-corrupt",
		URL:      "http://example.com/corrupt",
		DestPath: filepath.Join(tmpDir, "corrupt"),
		Status:   "paused",
	}
	if err := store.AddToMasterList(entry1); err != nil {
		t.Fatalf("failed to add entry1: %v", err)
	}
	if err := store.AddToMasterList(entry2); err != nil {
		t.Fatalf("failed to add entry2: %v", err)
	}

	// Write a corrupt gob state for entry2
	corruptStateFile := filepath.Join(tmpDir, "details", "id-corrupt.gob")
	if err := os.MkdirAll(filepath.Dir(corruptStateFile), 0755); err != nil {
		t.Fatalf("failed to create details dir: %v", err)
	}
	if err := os.WriteFile(corruptStateFile, []byte("not-a-gob"), 0644); err != nil {
		t.Fatalf("failed to write corrupt state file: %v", err)
	}

	// Initialize manager
	progressCh := make(chan types.DownloadEvent, 10)
	pool := scheduler.New(progressCh, 2)
	eb := NewEventBus()
	mgr := NewLifecycleManager(pool, eb, nil)
	defer mgr.Shutdown()

	errs := mgr.ResumeBatch([]string{"id-valid", "id-corrupt"})

	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errs))
	}
	if errs[0] != nil {
		t.Errorf("expected no error for id-valid, got %v", errs[0])
	}
	if errs[1] != nil {
		// Even for the corrupt one, it should fallback to the master list and successfully enqueue a fresh resume
		t.Errorf("expected no error for id-corrupt, got %v", errs[1])
	}

	// Check that pool has both downloads enqueued/started
	if pool.GetStatus("id-valid") == nil {
		t.Errorf("expected id-valid to be in pool")
	}
	if pool.GetStatus("id-corrupt") == nil {
		t.Errorf("expected id-corrupt to be in pool")
	}
}
