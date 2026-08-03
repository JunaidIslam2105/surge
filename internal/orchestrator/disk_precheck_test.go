package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestEnqueuePrecheck_Rejects(t *testing.T) {
	// fileSize > free - buffer → reject with ErrInsufficientDiskSpace.
	// The probe returns Content-Length, so we need a server that reports a size.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10485760") // 10 MiB
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

	orig := freeDiskBytes
	defer func() { freeDiskBytes = orig }()
	// Free = 1 MiB, buffer default = 500 MiB → 10 MiB file rejected.
	freeDiskBytes = func(path string) (int64, error) {
		return 1 * 1024 * 1024, nil
	}

	req := &DownloadRequest{
		URL:      ts.URL + "/testfile.bin",
		Filename: "testfile.bin",
		Path:     destDir,
	}

	_, _, err := mgr.Enqueue(context.Background(), req)
	if !errors.Is(err, types.ErrInsufficientDiskSpace) {
		t.Fatalf("expected ErrInsufficientDiskSpace, got %v", err)
	}

	// Verify no .surge orphan was left behind.
	surgePath := filepath.Join(destDir, "testfile.bin") + types.IncompleteSuffix
	if _, statErr := os.Stat(surgePath); !os.IsNotExist(statErr) {
		t.Errorf("expected no .surge orphan, but file exists at %s", surgePath)
	}
}

func TestEnqueuePrecheck_FailOpen(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10485760")
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

	orig := freeDiskBytes
	defer func() { freeDiskBytes = orig }()
	// freeDiskBytes returns an error → fail-open (proceed with download).
	freeDiskBytes = func(path string) (int64, error) {
		return 0, errors.New("statfs: operation not supported")
	}

	req := &DownloadRequest{
		URL:      ts.URL + "/testfile.bin",
		Filename: "testfile.bin",
		Path:     destDir,
	}

	id, _, err := mgr.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("fail-open should proceed with enqueue, got error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID on fail-open enqueue")
	}
}

func TestEnqueuePrecheck_UnknownSize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Length → unknown size.
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

	orig := freeDiskBytes
	defer func() { freeDiskBytes = orig }()
	// Even if free is tiny, unknown size skips precheck.
	freeDiskBytes = func(path string) (int64, error) {
		return 0, nil
	}

	req := &DownloadRequest{
		URL:      ts.URL + "/testfile.bin",
		Filename: "testfile.bin",
		Path:     destDir,
	}

	id, _, err := mgr.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("unknown size should skip precheck and proceed, got error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID on unknown-size enqueue")
	}
}
