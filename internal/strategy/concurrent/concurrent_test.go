package concurrent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func initTestState(t *testing.T) (string, func()) {
	tmpDir := testutil.SetupStateDB(t)
	return tmpDir, func() {}
}

func TestConcurrentDownloader_Download(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(1 * utils.MiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "test_download.bin")
	state := progress.New("test-id", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 4}

	downloader := NewConcurrentDownloader("test-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// Advanced Integration Tests - Latency & Timeouts
// =============================================================================

func TestConcurrentDownloader_WithLatency(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(64 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithLatency(100*time.Millisecond), // 100ms per request
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "latency_test.bin")
	state := progress.New("latency-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 2}

	downloader := NewConcurrentDownloader("latency-id", nil, state, runtime)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Should take at least 100ms due to latency
	if elapsed < 100*time.Millisecond {
		t.Errorf("Download completed too fast (%v), latency not applied", elapsed)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

func TestConcurrentDownloader_SlowDownload(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(32 * utils.KiB)
	// Very slow byte-by-byte latency
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(10*time.Microsecond),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "slow_test.bin")
	state := progress.New("slow-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 4}

	downloader := NewConcurrentDownloader("slow-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Slow download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// Advanced Integration Tests - Connection Limits
// =============================================================================

func TestConcurrentDownloader_RespectServerConnectionLimit(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	maxConns := 2
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithMaxConcurrentRequests(maxConns),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "connlimit_test.bin")
	state := progress.New("connlimit-test", fileSize)
	// Client configured for more connections than server allows
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 8, // More than server allows
		MinChunkSize:              16 * utils.KiB,
	}

	downloader := NewConcurrentDownloader("connlimit-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	stats := server.Stats()
	t.Logf("Server stats: TotalRequests=%d, RangeRequests=%d", stats.TotalRequests, stats.RangeRequests)
}

// =============================================================================
// Advanced Integration Tests - Content Verification
// =============================================================================

func TestConcurrentDownloader_ContentIntegrity(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(128 * utils.KiB)
	// Use random data so we can verify content integrity
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithRandomData(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "integrity_test.bin")
	state := progress.New("integrity-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 4,
		MinChunkSize:              16 * utils.KiB,
	}

	downloader := NewConcurrentDownloader("integrity-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify file size matches
	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	// Read first and last chunks and verify they're not all zeros
	first, err := testutil.ReadFileChunk(destPath+types.IncompleteSuffix, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	last, err := testutil.ReadFileChunk(destPath+types.IncompleteSuffix, fileSize-1024, 1024)
	if err != nil {
		t.Fatal(err)
	}

	// Random data shouldn't be all zeros
	allZero := true
	for _, b := range first {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("First chunk is all zeros - random data not applied correctly")
	}

	allZero = true
	for _, b := range last {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("Last chunk is all zeros - random data not applied correctly")
	}
}

func TestConcurrentDownloader_SmallFile(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(64 * 1024) // 64KB
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithFilename("small_test.bin"),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "small_test.bin")
	state := progress.New("test-download", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 4,
		MinChunkSize:              16 * utils.KiB,
		WorkerBufferSize:          8 * utils.KiB,
		MaxTaskRetries:            3,
	}

	downloader := NewConcurrentDownloader("test-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

func TestConcurrentDownloader_MediumFile(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(1 * utils.MiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "medium_test.bin")
	state := progress.New("test-download", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 8,
		MinChunkSize:              64 * utils.KiB,
		WorkerBufferSize:          32 * utils.KiB,
		MaxTaskRetries:            3,
	}

	downloader := NewConcurrentDownloader("test-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	stats := server.Stats()
	if stats.RangeRequests == 0 {
		t.Error("Expected range requests for concurrent download")
	}
}

func TestConcurrentDownloader_Cancellation(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(10 * utils.MiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(100*time.Microsecond),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "cancel_test.bin")
	state := progress.New("cancel-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 4}

	downloader := NewConcurrentDownloader("cancel-id", nil, state, runtime)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		// Pre-create incomplete file (simulating processing layer)
		if f, err := os.Create(destPath + ".surge"); err == nil {
			_ = f.Close()
		}

		done <- downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Download didn't respond to cancellation")
	}
}

func TestConcurrentDownloader_PauseAtCompletionFinalizesAsCompleted(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "pause_completion_test.bin")
	progressState := progress.New("pause-complete-test", fileSize)
	progressState.Pause()
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 4,
		MinChunkSize:              32 * utils.KiB,
	}
	downloader := NewConcurrentDownloader("pause-complete-id", nil, progressState, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if progressState.IsPaused() {
		t.Fatal("progress state should be resumed after completion-boundary pause handling")
	}
	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDownloader_ProgressTracking(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(512 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "progress_test.bin")
	state := progress.New("progress-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 4}

	downloader := NewConcurrentDownloader("progress-id", nil, state, runtime)

	// Since we can't easily access atomic counters inside the test helper without modifying imports or visibility,
	// we will trust the progress state updates which are public.
	// But the key is to run it and ensure it passes.

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	finalDownloaded := state.Bytes.Downloaded.Load()
	if finalDownloaded != fileSize {
		t.Errorf("Final downloaded %d != file size %d", finalDownloaded, fileSize)
	}
}

func TestConcurrentDownloader_RetryOnFailure(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	// Server fails after 50KB per-request, forcing retries
	// With 64KB chunks, each request will fail mid-way
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithFailAfterBytes(50*utils.KiB),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "retry_test.bin")
	state := progress.New("retry-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 2,
		MaxTaskRetries:            5,
		MinChunkSize:              64 * utils.KiB,
	}

	downloader := NewConcurrentDownloader("retry-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download with retries failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	stats := server.Stats()
	if stats.FailedRequests == 0 {
		t.Error("Expected some failed requests that triggered retries")
	}
}

func TestConcurrentDownloader_FailOnNthRequest(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	// Fail the 2nd request - use 1 connection for predictable ordering
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithFailOnNthRequest(1),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "failnth_test.bin")
	state := progress.New("failnth-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1, // Single connection for predictable request order
		MaxTaskRetries:            5,
		MinChunkSize:              64 * utils.KiB,
	}

	downloader := NewConcurrentDownloader("failnth-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download should recover from Nth request failure: %v", err)
	}

	stats := server.Stats()
	if stats.FailedRequests < 1 {
		t.Errorf("Expected at least 1 failed request, got %d", stats.FailedRequests)
	}
}

func TestConcurrentDownloader_ResumePartialDownload(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "resume_test.bin")

	// Create partial .surge file (simulate interrupted download)
	partialSize := int64(100 * utils.KiB)
	// Check if CreateTestFile needs to be adjusted.
	// Assuming testutil.CreateTestFile is available.
	_, err := testutil.CreateTestFile(tmpDir, "resume_test.bin.surge", partialSize, false)
	if err != nil {
		t.Fatal(err)
	}

	downloadID := "resume-id"

	// Create saved state for resume
	remainingTasks := []types.Task{
		{Offset: partialSize, Length: fileSize - partialSize},
	}
	// Need to check if DownloadRecord struct is compatible
	savedState := &types.DownloadRecord{
		ID:         downloadID,
		URL:        server.URL(),
		DestPath:   destPath,
		TotalSize:  fileSize,
		Downloaded: partialSize,
		Tasks:      remainingTasks,
		Filename:   "resume_test.bin",
		URLHash:    store.URLHash(server.URL()),
	}
	if err := store.SaveState(server.URL(), destPath, savedState); err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Now resume download
	progressState := progress.New("resume-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 2}

	downloader := NewConcurrentDownloader(downloadID, nil, progressState, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err = downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Resume download failed: %v", err)
	}
}

// =============================================================================
// createTasks Tests
// =============================================================================

func TestCreateTasks_Basic(t *testing.T) {
	fileSize := int64(1024 * 1024) // 1MB
	chunkSize := int64(256 * 1024) // 256KB

	tasks := createTasks(fileSize, chunkSize)

	if len(tasks) != 4 {
		t.Errorf("Expected 4 tasks, got %d", len(tasks))
	}

	// Verify tasks cover the entire file
	var totalLength int64
	for i, task := range tasks {
		totalLength += task.Length
		expectedOffset := int64(i) * chunkSize
		if task.Offset != expectedOffset {
			t.Errorf("Task %d: got offset %d, want %d", i, task.Offset, expectedOffset)
		}
	}

	if totalLength != fileSize {
		t.Errorf("Total length %d doesn't cover file size %d", totalLength, fileSize)
	}
}

func TestCreateTasks_UnevenDivision(t *testing.T) {
	fileSize := int64(1000)
	chunkSize := int64(300)

	tasks := createTasks(fileSize, chunkSize)

	if len(tasks) != 4 {
		t.Errorf("Expected 4 tasks, got %d", len(tasks))
	}

	lastTask := tasks[len(tasks)-1]
	if lastTask.Length != 100 {
		t.Errorf("Last task length should be 100, got %d", lastTask.Length)
	}
}

func TestCreateTasks_SmallFile(t *testing.T) {
	fileSize := int64(100)
	chunkSize := int64(1024)

	tasks := createTasks(fileSize, chunkSize)

	if len(tasks) != 1 {
		t.Errorf("Small file should have 1 task, got %d", len(tasks))
	}
	if tasks[0].Length != 100 {
		t.Errorf("Task length should equal file size, got %d", tasks[0].Length)
	}
}

func TestCreateTasks_ExactDivision(t *testing.T) {
	fileSize := int64(4096)
	chunkSize := int64(1024)

	tasks := createTasks(fileSize, chunkSize)

	if len(tasks) != 4 {
		t.Errorf("Expected 4 tasks, got %d", len(tasks))
	}

	for _, task := range tasks {
		if task.Length != 1024 {
			t.Errorf("Each task should be 1024 bytes, got %d", task.Length)
		}
	}
}

func TestCreateTasks_ZeroChunkSize(t *testing.T) {
	tasks := createTasks(1000, 0)
	if tasks != nil {
		t.Error("createTasks should return nil for zero chunk size")
	}

	tasks = createTasks(1000, -1)
	if tasks != nil {
		t.Error("createTasks should return nil for negative chunk size")
	}
}

// =============================================================================
// Bootstrap Metadata Tests
// =============================================================================

func TestConcurrentDownloader_Download_BootstrapSize(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	expectedSize := int64(1024)
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", expectedSize))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
			return
		}
		// Subsequent chunk requests
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "bootstrap_test.bin")
	state := progress.New("bootstrap-id", 0) // Unknown size
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 1}

	downloader := NewConcurrentDownloader("bootstrap-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL, nil, nil, destPath, 0)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if downloader.TotalSize != expectedSize {
		t.Errorf("Expected TotalSize %d, got %d", expectedSize, downloader.TotalSize)
	}
}

func TestConcurrentDownloader_Download_BootstrapFail_Non206(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // Not 206
		_, _ = w.Write([]byte("full content"))
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "bootstrap_fail.bin")
	state := progress.New("bootstrap-fail-id", 0)
	downloader := NewConcurrentDownloader("bootstrap-fail-id", nil, state, nil)

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(context.Background(), server.URL, nil, nil, destPath, 0)
	if err == nil {
		t.Fatal("Expected error when bootstrap fails (non-206)")
	}
	if !strings.Contains(err.Error(), "requires 206 response") {
		t.Errorf("Expected 206 error, got: %v", err)
	}
}

func TestConcurrentDownloader_Download_BootstrapFail_InvalidRange(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "garbage")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "bootstrap_invalid.bin")
	state := progress.New("bootstrap-invalid-id", 0)
	downloader := NewConcurrentDownloader("bootstrap-invalid-id", nil, state, nil)

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(context.Background(), server.URL, nil, nil, destPath, 0)
	if err == nil {
		t.Fatal("Expected error when bootstrap fails (invalid range)")
	}
	if !strings.Contains(err.Error(), "invalid Content-Range header") {
		t.Errorf("Expected range error, got: %v", err)
	}
}

func TestConcurrentDownloader_PauseDuringRetryBackoff(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithFailOnNthRequest(1),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "pause_backoff_test.bin")
	state := progress.New("pause-backoff-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            5,
		MinChunkSize:              64 * utils.KiB,
	}

	progressCh := make(chan types.DownloadEvent, 10)
	downloader := NewConcurrentDownloader("pause-backoff-id", progressCh, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	go func() {
		time.Sleep(150 * time.Millisecond) // Ensure worker hits error and sleeps
		state.Pause()
	}()

	err := downloader.Download(ctx, server.URL(), nil, nil, destPath, fileSize)
	if !errors.Is(err, types.ErrPaused) {
		t.Fatalf("Expected ErrPaused, got: %v", err)
	}

	var pausedEvent *types.DownloadEvent
	close(progressCh)
	for event := range progressCh {
		if event.Type == types.EventPaused {
			pausedEvent = &event
			break
		}
	}

	if pausedEvent == nil {
		t.Fatalf("Expected EventPaused on progress channel")
	}

	savedState := pausedEvent.State
	if savedState == nil {
		t.Fatalf("Expected state in paused event")
	}

	if savedState.Downloaded > 0 {
		t.Errorf("Expected 0 bytes downloaded in saved state, but got %d (progress jump bug!)", savedState.Downloaded)
	}
}
