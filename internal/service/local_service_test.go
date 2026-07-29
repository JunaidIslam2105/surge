package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

func setupTestService(t *testing.T) (*LocalDownloadService, *httptest.Server, string) {
	testutil.SetupStateDB(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 1024))
	}))

	progressCh := make(chan types.DownloadEvent, 10)
	pool := scheduler.New(progressCh, 1)
	eb := orchestrator.NewEventBus()
	mgr := orchestrator.NewLifecycleManager(pool, eb, nil)

	// Ensure config directory exists for settings tests
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	svc := NewLocalDownloadService(mgr)

	// Mock event worker to handle EventRemoved
	ch, cleanup := eb.Subscribe()
	go func() {
		for e := range ch {
			if e.Type == types.EventRemoved {
				_ = store.DeleteState(e.DownloadID)
			}
		}
	}()
	t.Cleanup(cleanup)

	return svc, ts, tmpDir
}

func TestLocalDownloadService_AddWithID_UsesProvidedID(t *testing.T) {
	svc, globalTs, tmpDir := setupTestService(t)
	defer globalTs.Close()
	t.Cleanup(func() { _ = svc.Shutdown() })

	blockCh := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if r.Header.Get("Range") != "bytes=0-0" {
			select {
			case <-blockCh:
			case <-r.Context().Done():
			}
		}
	}))
	defer ts.Close()
	defer close(blockCh)

	customID := "test-id-123"
	id, err := svc.AddWithID(ts.URL, tmpDir, "test.txt", nil, nil, customID, false, 1, 0)

	if err != nil {
		t.Fatalf("AddWithID failed: %v", err)
	}
	if id != customID {
		t.Errorf("expected ID %s, got %s", customID, id)
	}

	status, err := svc.GetStatus(customID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.ID != customID {
		t.Errorf("expected status ID %s, got %s", customID, status.ID)
	}
}

func TestLocalDownloadService_StreamEvents(t *testing.T) {
	svc, ts, tmpDir := setupTestService(t)
	defer ts.Close()

	ch, cleanup, err := svc.StreamEvents(context.Background())
	if err != nil {
		t.Fatalf("StreamEvents failed: %v", err)
	}
	defer cleanup()

	// Add a download to generate an event
	_, _ = svc.Add(ts.URL, tmpDir, "event.txt", nil, nil, false, 1, 0)

	select {
	case <-ch:
		// Received an event
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Shutting down should close the channel
	if err := svc.Shutdown(); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Drain the channel until it is closed
	closed := false
	timeout := time.After(2 * time.Second)
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for channel to close")
		}
	}
}

func TestLocalDownloadService_RateLimits(t *testing.T) {
	svc, ts, tmpDir := setupTestService(t)
	defer ts.Close()
	t.Cleanup(func() { _ = svc.Shutdown() })

	err := svc.SetRateLimit("invalid", 100)
	if err == nil {
		t.Error("expected error setting rate limit on unknown ID")
	}

	err = svc.SetGlobalRateLimit(-1)
	if err == nil {
		t.Error("expected error for negative global rate limit")
	}

	err = svc.SetDefaultRateLimit(-1)
	if err == nil {
		t.Error("expected error for negative default rate limit")
	}

	// Test Global Rate Limit (which saves settings)
	err = svc.SetGlobalRateLimit(5000)
	if err != nil {
		t.Errorf("SetGlobalRateLimit failed: %v", err)
	}

	settings := svc.lifecycle.GetSettings()
	if settings.Network.GlobalRateLimit.Value != "4.9 KiB/s" && settings.Network.GlobalRateLimit.Value != "5000" {
		t.Errorf("expected GlobalRateLimit to be set to 4.9 KiB/s, got %s", settings.Network.GlobalRateLimit.Value)
	}

	// Test Default Rate Limit
	err = svc.SetDefaultRateLimit(1000)
	if err != nil {
		t.Errorf("SetDefaultRateLimit failed: %v", err)
	}

	// Use a blocking server so the download stays active in the scheduler
	// (prevents it from completing before we can test rate limit operations)
	blockCh := make(chan struct{})
	rateTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if r.Header.Get("Range") != "bytes=0-0" {
			select {
			case <-blockCh:
			case <-r.Context().Done():
			}
		}
	}))
	defer rateTs.Close()
	defer close(blockCh)

	// Add a download and set its specific rate limit
	id, _ := svc.Add(rateTs.URL, tmpDir, "rate.txt", nil, nil, false, 1, 0)

	// Wait briefly for the download to be picked up by the scheduler
	time.Sleep(100 * time.Millisecond)

	err = svc.SetRateLimit(id, 2000)
	if err != nil {
		t.Errorf("SetRateLimit failed: %v", err)
	}

	status, _ := svc.GetStatus(id)
	if status.RateLimit != 2000 {
		t.Errorf("expected rate limit 2000, got %d", status.RateLimit)
	}

	// Clear it
	err = svc.ClearRateLimit(id)
	if err != nil {
		t.Errorf("ClearRateLimit failed: %v", err)
	}

	status, _ = svc.GetStatus(id)
	if status.RateLimit != 1000 { // fallback to default
		t.Errorf("expected rate limit 1000 (default) after clear, got %d", status.RateLimit)
	}
}

func TestLocalDownloadService_Purge(t *testing.T) {
	svc, ts, tmpDir := setupTestService(t)
	defer ts.Close()
	t.Cleanup(func() { _ = svc.Shutdown() })

	blockCh := make(chan struct{})
	purgeTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if r.Header.Get("Range") != "bytes=0-0" {
			select {
			case <-blockCh:
			case <-r.Context().Done():
			}
		}
	}))
	defer purgeTs.Close()
	defer close(blockCh)

	id, _ := svc.AddWithID(purgeTs.URL, tmpDir, "purge.txt", nil, nil, "purge-id", false, 1, 0)

	// Wait a tiny bit for the file to be created
	time.Sleep(100 * time.Millisecond)

	status, err := svc.GetStatus(id)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	t.Logf("GetStatus before purge returned destPath: %s", status.DestPath)

	err = svc.Purge(id)
	if err != nil {
		t.Errorf("Purge failed: %v", err)
	}

	// Wait a tiny bit to ensure worker exited and no re-creation happened
	time.Sleep(100 * time.Millisecond)

	// Check that the file was deleted
	if _, err := os.Stat(filepath.Join(tmpDir, "purge.txt")); !os.IsNotExist(err) {
		t.Errorf("file purge.txt was not deleted")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "purge.txt"+types.IncompleteSuffix)); !os.IsNotExist(err) {
		t.Errorf("file purge.txt.surge was not deleted")
	}
}

func TestLocalDownloadService_HistoryAndList(t *testing.T) {
	svc, ts, tmpDir := setupTestService(t)
	defer ts.Close()
	t.Cleanup(func() { _ = svc.Shutdown() })

	blockCh := make(chan struct{})
	listTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if r.Header.Get("Range") != "bytes=0-0" {
			select {
			case <-blockCh:
			case <-r.Context().Done():
			}
		}
	}))
	defer listTs.Close()
	defer close(blockCh)

	_, err := svc.Add(listTs.URL, tmpDir, "list1.txt", nil, nil, false, 1, 0)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Add a dummy completed download to store
	testutil.SeedMasterList(t, types.DownloadRecord{
		ID:       "db-id",
		Status:   "completed",
		Filename: "db.txt",
	})

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(list) < 2 {
		t.Errorf("expected at least 2 items in list, got %d", len(list))
	}

	history, err := svc.History()
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("expected 1 history item, got %d", len(history))
	}

	count, _ := svc.ClearCompleted()
	if count != 1 {
		t.Errorf("expected 1 completed item to be cleared, got %d", count)
	}
}

func TestLocalDownloadService_Delete(t *testing.T) {
	svc, ts, tmpDir := setupTestService(t)
	defer ts.Close()
	t.Cleanup(func() { _ = svc.Shutdown() })

	id, _ := svc.Add(ts.URL, tmpDir, "delete.txt", nil, nil, false, 1, 0)

	err := svc.Delete(id)
	if err != nil {
		t.Errorf("Delete failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Check if it's gone
	_, err = svc.GetStatus(id)
	if err == nil {
		t.Error("expected error getting status of deleted download")
	}
}
