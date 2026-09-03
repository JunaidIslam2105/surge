package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/store"
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

func TestLifecycleManager_EnqueueCoalescesConcurrentRequests(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		<-release
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pool := scheduler.New(make(chan types.DownloadEvent, 10), 1)
	mgr := NewLifecycleManager(pool, nil, nil)
	defer mgr.Shutdown()
	joined := make(chan struct{}, 1)
	mgr.inflightJoinHook = func() { joined <- struct{}{} }

	req := &DownloadRequest{URL: ts.URL + "/duplicate.bin", Filename: "duplicate.bin", Path: t.TempDir()}
	type result struct {
		id  string
		err error
	}
	results := make(chan result, 2)
	go func() {
		id, _, err := mgr.Enqueue(context.Background(), req)
		results <- result{id, err}
	}()
	<-started
	go func() {
		id, _, err := mgr.Enqueue(context.Background(), req)
		results <- result{id, err}
	}()
	<-joined
	close(release)

	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("Enqueue errors = %v, %v", first.err, second.err)
	}
	if first.id == "" || first.id != second.id {
		t.Fatalf("enqueue ids = %q, %q; want one shared id", first.id, second.id)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("probe requests = %d, want 1", got)
	}
}

func TestLifecycleManager_EnqueueDoesNotCoalesceDifferentRequests(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pool := scheduler.New(make(chan types.DownloadEvent, 10), 1)
	mgr := NewLifecycleManager(pool, nil, nil)
	defer mgr.Shutdown()
	entered := make(chan struct{}, 2)
	mgr.inflightStartHook = func() { entered <- struct{}{} }

	dest := t.TempDir()
	type result struct {
		filename string
		err      error
	}
	results := make(chan result, 2)
	for _, filename := range []string{"first.bin", "second.bin"} {
		go func(filename string) {
			_, finalFilename, err := mgr.Enqueue(context.Background(), &DownloadRequest{
				URL: ts.URL + "/same-resource", Filename: filename, Path: dest,
			})
			results <- result{finalFilename, err}
		}(filename)
	}
	// Wait until both requests have passed the in-flight map before releasing
	// the first same-host probe; the probe layer serializes hosts deliberately.
	<-entered
	<-entered
	close(release)
	<-started
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("Enqueue errors = %v, %v", first.err, second.err)
	}
	if (first.filename != "first.bin" || second.filename != "second.bin") &&
		(first.filename != "second.bin" || second.filename != "first.bin") {
		t.Fatalf("resolved filenames = %q, %q; want both distinct requested names", first.filename, second.filename)
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
