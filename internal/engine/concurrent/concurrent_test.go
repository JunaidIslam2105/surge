package concurrent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
)

// Helper to init state just for tests (avoiding global init if possible,
// using temporary directories for each test)
func initTestState(t *testing.T) (string, func()) {
	tmpDir, cleanup, err := testutil.TempDir("surge-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "surge.db")
	state.Configure(dbPath)

	return tmpDir, func() {
		cleanup()
	}
}

func TestConcurrentDownloader_Download(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(1 * types.MB)
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "test_download.bin")
	state := types.NewProgressState("test-id", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerHost: 4}

	downloader := NewConcurrentDownloader("test-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// Advanced Integration Tests - Latency & Timeouts
// =============================================================================

func TestConcurrentDownloader_WithLatency(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(64 * types.KB)
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithLatency(100*time.Millisecond), // 100ms per request
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "latency_test.bin")
	state := types.NewProgressState("latency-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerHost: 2}

	downloader := NewConcurrentDownloader("latency-id", nil, state, runtime)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Should take at least 100ms due to latency
	if elapsed < 100*time.Millisecond {
		t.Errorf("Download completed too fast (%v), latency not applied", elapsed)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}

func TestConcurrentDownloader_SlowDownload(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(32 * types.KB)
	// Very slow byte-by-byte latency
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(10*time.Microsecond),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "slow_test.bin")
	state := types.NewProgressState("slow-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerHost: 4}

	downloader := NewConcurrentDownloader("slow-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Slow download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// Advanced Integration Tests - Connection Limits
// =============================================================================

func TestConcurrentDownloader_RespectServerConnectionLimit(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * types.KB)
	maxConns := 2
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithMaxConcurrentRequests(maxConns),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "connlimit_test.bin")
	state := types.NewProgressState("connlimit-test", fileSize)
	// Client configured for more connections than server allows
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 8, // More than server allows
		MinChunkSize:          16 * types.KB,
	}

	downloader := NewConcurrentDownloader("connlimit-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
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

	fileSize := int64(128 * types.KB)
	// Use random data so we can verify content integrity
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithRandomData(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "integrity_test.bin")
	state := types.NewProgressState("integrity-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 4,
		MinChunkSize:          16 * types.KB,
	}

	downloader := NewConcurrentDownloader("integrity-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify file size matches
	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}

	// Read first and last chunks and verify they're not all zeros
	first, err := testutil.ReadFileChunk(destPath, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	last, err := testutil.ReadFileChunk(destPath, fileSize-1024, 1024)
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
