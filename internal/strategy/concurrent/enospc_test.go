package concurrent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// diskFullWriteErr returns a platform disk-full PathError for writeAtFn injection.
func diskFullWriteErr() error {
	return makeDiskFullPathError()
}

func TestWorker_ENOSPC_NoRetry(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)

	fileSize := int64(1 * utils.MiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "enospc_noretry.bin")
	state := progress.New("enospc-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 1}

	downloader := NewConcurrentDownloader("enospc-test", nil, state, runtime)

	// Inject ENOSPC on WriteAt.
	orig := writeAtFn
	defer func() { writeAtFn = orig }()
	writeAtFn = func(f *os.File, b []byte, off int64) (int, error) {
		return 0, diskFullWriteErr()
	}

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err == nil {
		t.Fatal("expected ENOSPC error, got nil")
	}
	if !types.IsInsufficientDiskSpace(err) {
		t.Fatalf("expected ErrInsufficientDiskSpace wrap, got %v", err)
	}
	if !errors.Is(err, types.ErrInsufficientDiskSpace) {
		t.Fatalf("errors.Is(err, ErrInsufficientDiskSpace) should be true, got %v", err)
	}
}

func TestWorker_ENOSPC_ClearsActiveTask(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)

	fileSize := int64(1 * utils.MiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "enospc_clear.bin")
	state := progress.New("enospc-clear-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 1}

	downloader := NewConcurrentDownloader("enospc-clear-test", nil, state, runtime)

	orig := writeAtFn
	defer func() { writeAtFn = orig }()
	writeAtFn = func(f *os.File, b []byte, off int64) (int, error) {
		return 0, diskFullWriteErr()
	}

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)

	// After ENOSPC, the worker should have cleared its active task entry.
	downloader.activeMu.Lock()
	if len(downloader.activeTasks) != 0 {
		t.Errorf("expected activeTasks empty after ENOSPC, got %d entries", len(downloader.activeTasks))
	}
	downloader.activeMu.Unlock()

	// ActiveWorkers should be back to 0.
	if got := state.ActiveWorkers.Load(); got != 0 {
		t.Errorf("expected ActiveWorkers=0 after ENOSPC, got %d", got)
	}
}
