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

// setupPrecheckTest creates a lifecycle manager backed by a mock HTTP server
// and a temp destination directory. The caller overrides freeDiskBytes and
// defers the restore themselves. The returned server reports the given
// contentLength (0 means no Content-Length header).
func setupPrecheckTest(t *testing.T, contentLength int64) (*LifecycleManager, *httptest.Server, string) {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentLength > 0 {
			w.Header().Set("Content-Length", "10485760")
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	progressCh := make(chan types.DownloadEvent, 10)
	pool := scheduler.New(progressCh, 1)
	eb := NewEventBus()
	mgr := NewLifecycleManager(pool, eb, nil)
	t.Cleanup(mgr.Shutdown)

	destDir := t.TempDir()
	return mgr, ts, destDir
}

func TestEnqueuePrecheck_Rejects(t *testing.T) {
	mgr, ts, destDir := setupPrecheckTest(t, 10*1024*1024)

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
	mgr, ts, destDir := setupPrecheckTest(t, 10*1024*1024)

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
	mgr, ts, destDir := setupPrecheckTest(t, 0)

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
