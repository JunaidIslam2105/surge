package concurrent

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestHandlePause_CompletionBoundary(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	destPath := filepath.Join(tmpDir, "test.bin")
	state := progress.New("test-id", fileSize)
	downloader := &ConcurrentDownloader{
		ID:      "test-id",
		State:   state,
		Runtime: &types.RuntimeConfig{},
	}

	queue := NewTaskQueue()
	// No tasks in queue means remainingBytes == 0

	err := downloader.handlePause(destPath, fileSize, queue, nil)
	if err != nil {
		t.Fatalf("handlePause returned error on completion boundary: %v", err)
	}

	if state.IsPaused() {
		t.Errorf("State should not be paused on completion boundary")
	}
}

func TestHandlePause_Normal(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	destPath := filepath.Join(tmpDir, "test.bin")
	state := progress.New("test-id", fileSize)
	downloader := &ConcurrentDownloader{
		ID:      "test-id",
		State:   state,
		Runtime: &types.RuntimeConfig{},
	}

	queue := NewTaskQueue()
	queue.Push(types.Task{Offset: 500, Length: 500})

	err := downloader.handlePause(destPath, fileSize, queue, nil)
	if !errors.Is(err, types.ErrPaused) {
		t.Fatalf("Expected ErrPaused, got %v", err)
	}
}

func TestHandlePause_UsesLiveRateLimitFromState(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	destPath := filepath.Join(tmpDir, "test.bin")
	state := progress.New("test-id", fileSize)
	state.SetRateLimit(3*1024*1024, true)
	progressCh := make(chan types.DownloadEvent, 1)
	downloader := &ConcurrentDownloader{
		ID:           "test-id",
		URL:          "http://example.com/file.bin",
		State:        state,
		ProgressChan: progressCh,
		Runtime:      &types.RuntimeConfig{},
		RateLimitBps: 1,
		RateLimitSet: false,
	}

	queue := NewTaskQueue()
	queue.Push(types.Task{Offset: 500, Length: 500})

	err := downloader.handlePause(destPath, fileSize, queue, nil)
	if !errors.Is(err, types.ErrPaused) {
		t.Fatalf("Expected ErrPaused, got %v", err)
	}

	msg, ok := <-progressCh
	if !ok {
		t.Fatalf("expected DownloadPausedMsg, got %T", msg)
	}
	if msg.RateLimit != 3*1024*1024 || !msg.RateLimitSet {
		t.Fatalf("pause msg rate limit = (%d, %v), want (%d, true)", msg.RateLimit, msg.RateLimitSet, 3*1024*1024)
	}
	if msg.State == nil {
		t.Fatal("expected pause state")
	}
}

func TestSetupTasks_NewDownload(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	chunkSize := int64(500)
	destPath := filepath.Join(tmpDir, "new.bin")
	workingPath := destPath + types.IncompleteSuffix

	f, err := os.Create(workingPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	state := progress.New("test-id", fileSize)
	downloader := &ConcurrentDownloader{
		ID:      "test-id",
		State:   state,
		Runtime: &types.RuntimeConfig{},
	}

	tasks, err := downloader.setupTasks(destPath, fileSize, chunkSize, f, nil, false)
	if err != nil {
		t.Fatalf("setupTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("Expected 2 tasks, got %d", len(tasks))
	}
}

func TestGetWorkerMirrors(t *testing.T) {
	d := &ConcurrentDownloader{URL: "http://primary.com"}
	active := []string{"http://primary.com", "http://mirror1.com", "http://mirror2.com"}

	mirrors := d.getWorkerMirrors(active)

	if len(mirrors) != 3 {
		t.Errorf("Expected 3 mirrors, got %d", len(mirrors))
	}
	if mirrors[0] != "http://primary.com" {
		t.Errorf("Primary URL should be first, got %s", mirrors[0])
	}
}

func TestInitMirrorStatus(t *testing.T) {
	state := progress.New("test-id", 1000)
	d := &ConcurrentDownloader{ID: "test-id", State: state}

	primary := "http://primary.com"
	candidates := []string{"http://mirror1.com", "http://mirror2.com"}
	active := []string{"http://primary.com", "http://mirror1.com"}

	d.initMirrorStatus(primary, candidates, active, "/path/to/dest")

	statuses := state.GetMirrors()
	if len(statuses) != 3 {
		t.Errorf("Expected 3 statuses, got %d", len(statuses))
	}

	foundMirror2 := false
	for _, s := range statuses {
		if s.URL == "http://mirror2.com" {
			foundMirror2 = true
			if s.Active {
				t.Error("Mirror2 should be inactive")
			}
			if !s.Error {
				t.Error("Mirror2 should have error (as it is not active)")
			}
		}
	}
	if !foundMirror2 {
		t.Error("Mirror2 status not found")
	}
}

func TestSetupTasks_BitmapRestoration(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	chunkSize := int64(100)
	destPath := filepath.Join(tmpDir, "resume.bin")

	// Create a saved state
	savedBitmap := []byte{0xFF, 0x00, 0x00} // 10 chunks need 3 bytes
	savedState := &types.DownloadRecord{
		ID:              "test-id",
		URL:             "http://example.com",
		DestPath:        destPath,
		TotalSize:       fileSize,
		Downloaded:      500,
		ActualChunkSize: chunkSize,
		ChunkBitmap:     savedBitmap,
		Tasks:           []types.Task{{Offset: 500, Length: 500}},
	}
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:         "test-id",
		URL:        "http://example.com",
		DestPath:   destPath,
		Filename:   filepath.Base(destPath),
		Status:     "paused",
		TotalSize:  fileSize,
		Downloaded: 500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState("http://example.com", destPath, savedState); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Create(destPath + types.IncompleteSuffix)
	defer func() { _ = f.Close() }()

	progState := progress.New("test-id", fileSize)
	downloader := &ConcurrentDownloader{
		ID:    "test-id",
		URL:   "http://example.com",
		State: progState,
	}

	// This simulates the fixed order in Download():
	// 1. InitBitmap
	progState.InitBitmap(fileSize, chunkSize)
	// 2. setupTasks (which calls RestoreBitmap)
	_, err := downloader.setupTasks(destPath, fileSize, chunkSize, f, savedState, true)
	if err != nil {
		t.Fatal(err)
	}

	// Verify bitmap is NOT empty (it should have the restored data)
	bitmap, _, _, _, _ := progState.GetBitmapSnapshot(false)
	if len(bitmap) == 0 {
		t.Error("Bitmap should have been restored, but it is empty")
	}
	if bitmap[0] != 0xAA {
		t.Errorf("Bitmap[0] should be 0xAA (all chunks completed), got 0x%02X", bitmap[0])
	}
}

func TestHandlePause_CompletionFinalization(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	destPath := filepath.Join(tmpDir, "test.bin")
	progState := progress.New("test-id", fileSize)
	downloader := &ConcurrentDownloader{
		ID:    "test-id",
		State: progState,
	}

	queue := NewTaskQueue()
	// No tasks left

	err := downloader.handlePause(destPath, fileSize, queue, nil)
	if err != nil {
		t.Errorf("Expected nil error for completion boundary, got %v", err)
	}

	if progState.IsPaused() {
		t.Error("Should have resumed state for completion")
	}
}

func TestSaveStateSnapshot_HedgeOrderingAndResume(t *testing.T) {
	tmpDir := testutil.SetupStateDB(t)
	cleanup := func() {}
	defer cleanup()

	fileSize := int64(1000)
	destPath := filepath.Join(tmpDir, "hedge.bin")
	state := progress.New("test-id", fileSize)
	downloader := &ConcurrentDownloader{
		ID:      "test-id",
		State:   state,
		Runtime: &types.RuntimeConfig{},
		URL:     "http://example.com/hedge.bin",
		activeTasks: make(map[int]*ActiveTask),
	}
	
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:         "test-id",
		URL:        "http://example.com/hedge.bin",
		DestPath:   destPath,
		Status:     "downloading",
		TotalSize:  fileSize,
	}); err != nil {
		t.Fatal(err)
	}

	queue := NewTaskQueue()
	
	// Create a shared offset pointer
	sharedOffset := &atomic.Int64{}
	sharedOffset.Store(500)

	// Simulate a queued hedge task that was created when the offset was 500
	queue.Push(types.Task{
		Offset:          500,
		Length:          500, // 1000 - 500
		SharedMaxOffset: sharedOffset,
	})

	// Simulate an active worker that has advanced to 600
	active := &ActiveTask{
		SharedMaxOffset: sharedOffset,
	}
	active.CurrentOffset.Store(600)
	active.StopAt.Store(1000)
	
	downloader.activeTasks[0] = active

	// Run saveStateSnapshot directly (false to not emit EventPaused, just write to store)
	err := downloader.saveStateSnapshot(destPath, fileSize, queue, nil, false)
	if err != nil {
		t.Fatalf("saveStateSnapshot failed: %v", err)
	}

	// Load the saved state to verify
	saved, err := store.LoadState(downloader.URL, destPath)
	if err != nil {
		t.Fatalf("failed to load saved state: %v", err)
	}

	if len(saved.Tasks) != 1 {
		t.Fatalf("Expected exactly 1 deduplicated task, got %d", len(saved.Tasks))
	}

	task := saved.Tasks[0]
	// If active worker won deduplication, Length should be 400 (1000-600).
	// If queued task won, Length would be 500.
	if task.Length != 400 {
		t.Errorf("Expected remaining length 400 (active task won), got %d (queued task won?)", task.Length)
	}
	if task.Offset != 600 {
		t.Errorf("Expected remaining offset 600, got %d", task.Offset)
	}

	// Verify SharedMaxOffset is cleared to prevent hot-resume pinning
	if task.SharedMaxOffset != nil {
		t.Errorf("Expected SharedMaxOffset to be cleared, but it was not nil")
	}
}
