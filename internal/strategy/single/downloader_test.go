package single

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func TestCopyFile(t *testing.T) {
	tmpDir, cleanup, err := testutil.TempDir("surge-copy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Create source file
	srcPath, err := testutil.CreateTestFile(tmpDir, "src.bin", 1024, true)
	if err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(tmpDir, "dst.bin")

	err = utils.CopyFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// Verify destination exists
	if !testutil.FileExists(dstPath) {
		t.Error("Destination file should exist")
	}

	// Verify sizes match
	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)
	if srcInfo.Size() != dstInfo.Size() {
		t.Error("File sizes don't match")
	}

	// Verify contents match
	match, err := testutil.CompareFiles(srcPath, dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("File contents don't match")
	}
}

func TestCopyFile_SourceNotExists(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-test")
	defer cleanup()

	err := utils.CopyFile(filepath.Join(tmpDir, "nonexistent.bin"), filepath.Join(tmpDir, "dst.bin"))
	if err == nil {
		t.Error("Expected error for nonexistent source")
	}
}

func TestCopyFile_InvalidDestination(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-test")
	defer cleanup()

	srcPath, _ := testutil.CreateTestFile(tmpDir, "src.bin", 100, false)

	// Try to copy to an invalid path (non-existent directory)
	err := utils.CopyFile(srcPath, filepath.Join(tmpDir, "nonexistent", "subdir", "dst.bin"))
	if err == nil {
		t.Error("Expected error for invalid destination")
	}
}

func TestCopyFile_EmptyFile(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-test")
	defer cleanup()

	srcPath, _ := testutil.CreateTestFile(tmpDir, "empty.bin", 0, false)
	dstPath := filepath.Join(tmpDir, "empty_copy.bin")

	err := utils.CopyFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("copyFile failed for empty file: %v", err)
	}

	if err := testutil.VerifyFileSize(dstPath, 0); err != nil {
		t.Error(err)
	}
}

func TestCopyFile_LargeFile(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-test")
	defer cleanup()

	size := int64(5 * utils.MiB)
	srcPath, _ := testutil.CreateTestFile(tmpDir, "large.bin", size, false)
	dstPath := filepath.Join(tmpDir, "large_copy.bin")

	err := utils.CopyFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("copyFile failed for large file: %v", err)
	}

	if err := testutil.VerifyFileSize(dstPath, size); err != nil {
		t.Error(err)
	}
}

func TestCopyFile_ContentVerification(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-content")
	defer cleanup()

	size := int64(128 * utils.KiB)
	srcPath, _ := testutil.CreateTestFile(tmpDir, "random.bin", size, true) // Random data
	dstPath := filepath.Join(tmpDir, "random_copy.bin")

	err := utils.CopyFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	match, err := testutil.CompareFiles(srcPath, dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("Copied file content doesn't match source")
	}
}

func TestPreallocateFile(t *testing.T) {
	tmpDir, cleanup, err := testutil.TempDir("surge-prealloc-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	filePath := filepath.Join(tmpDir, "prealloc.bin")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	const size = int64(2 * utils.MiB)
	if err := preallocateFile(file, size); err != nil {
		t.Fatalf("preallocateFile failed: %v", err)
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size {
		t.Fatalf("file size = %d, want %d", info.Size(), size)
	}
}

// =============================================================================
// SingleDownloader - Streaming Server
// =============================================================================

func TestSingleDownloader_StreamingServer(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-stream-single")
	defer cleanup()

	fileSize := int64(1 * utils.MiB)
	server := testutil.NewStreamingMockServerT(t, fileSize,
		testutil.WithRangeSupport(false),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "stream_single.bin")
	state := progress.New("stream-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("stream-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "stream.bin")
	if err != nil {
		t.Fatalf("Streaming download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// SingleDownloader - FailAfterBytes
// =============================================================================

func TestSingleDownloader_FailAfterBytes(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-failafter-single")
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	// Server fails after sending 50KB
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
		testutil.WithFailAfterBytes(50*utils.KiB),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "failafter_single.bin")
	state := progress.New("failafter-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("failafter-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "failafter.bin")
	// Should fail since SingleDownloader doesn't retry
	if err == nil {
		t.Error("Expected error when server fails mid-transfer")
	}

	// Partial file should exist with .surge suffix
	stats := server.Stats()
	if stats.BytesServed < 50*utils.KiB {
		t.Errorf("Expected at least 50KB served before failure, got %d", stats.BytesServed)
	}
}

// =============================================================================
// SingleDownloader - NilState handling
// =============================================================================

func TestSingleDownloader_NilState(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-nilstate-single")
	defer cleanup()

	fileSize := int64(32 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "nilstate_single.bin")
	runtime := &types.RuntimeConfig{}

	// Create downloader with nil state
	downloader := NewSingleDownloader("nilstate-id", nil, nil, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "nilstate.bin")
	if err != nil {
		t.Fatalf("Download with nil state failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// Restored Standard Tests
// =============================================================================

func TestNewSingleDownloader(t *testing.T) {
	state := progress.New("test", 1000)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("test-id", nil, state, runtime)

	if downloader == nil {
		t.Fatal("NewSingleDownloader returned nil")
		return
	}
	if downloader.ID != "test-id" {
		t.Errorf("ID mismatch: got %s, want test-id", downloader.ID)
	}
	if downloader.State != state {
		t.Error("State not set correctly")
	}
}

func TestNewSingleDownloader_TransportReuse(t *testing.T) {
	runtime := &types.RuntimeConfig{MaxConnectionsPerDownload: 8}
	t1 := transport.DefaultNetworkPool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, runtime.GetMaxConnectionsPerDownload())
	defer transport.DefaultNetworkPool.ReleaseTransport(t1)

	t2 := transport.DefaultNetworkPool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, runtime.GetMaxConnectionsPerDownload())
	defer transport.DefaultNetworkPool.ReleaseTransport(t2)

	if t1 != t2 {
		t.Fatal("expected transport reuse for identical runtime config")
	}
}

func TestNewSingleDownloader_TransportIsolationByProxy(t *testing.T) {
	r1 := &types.RuntimeConfig{ProxyURL: "http://127.0.0.1:8080"}
	r2 := &types.RuntimeConfig{ProxyURL: "http://127.0.0.1:9090"}

	t1 := transport.DefaultNetworkPool.AcquireTransport(r1.ProxyURL, r1.CustomDNS, r1.GetMaxConnectionsPerDownload())
	defer transport.DefaultNetworkPool.ReleaseTransport(t1)

	t2 := transport.DefaultNetworkPool.AcquireTransport(r2.ProxyURL, r2.CustomDNS, r2.GetMaxConnectionsPerDownload())
	defer transport.DefaultNetworkPool.ReleaseTransport(t2)

	if t1 == t2 {
		t.Fatal("expected different transports for different proxy settings")
	}
}

func TestSingleDownloader_Download_Success(t *testing.T) {
	tmpDir, cleanup, err := testutil.TempDir("surge-single-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	fileSize := int64(64 * 1024) // 64KB
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false), // SingleDownloader doesn't use ranges
		testutil.WithFilename("single_test.bin"),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "single_test.bin")
	state := progress.New("single-test", fileSize)
	runtime := &types.RuntimeConfig{WorkerBufferSize: 8 * utils.KiB}

	downloader := NewSingleDownloader("single-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err = downloader.Download(ctx, server.URL(), destPath, fileSize, "single_test.bin")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify file exists and has correct size
	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	// Verify progress was tracked
	if state.Bytes.Downloaded.Load() != fileSize {
		t.Errorf("Downloaded %d != fileSize %d", state.Bytes.Downloaded.Load(), fileSize)
	}
	if state.ActiveWorkers.Load() != 0 {
		t.Errorf("ActiveWorkers = %d, want 0 (should clear after download completes)", state.ActiveWorkers.Load())
	}
}

func TestSingleDownloader_StripsCallerRangeHeader(t *testing.T) {
	// Regression: a caller-supplied Range header (e.g. forwarded from the
	// browser) must NOT be sent by the single downloader. Otherwise a
	// range-capable server replies 206 and the strict 200 check aborts an
	// otherwise valid download with "unexpected status code: 206". The fix uses
	// strings.EqualFold, so non-canonical casings (some clients/proxies forward
	// a lowercase "range") must be stripped as well.
	for _, headerKey := range []string{"Range", "range", "RANGE"} {
		t.Run(headerKey, func(t *testing.T) {
			tmpDir, cleanup, err := testutil.TempDir("surge-single-range")
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()

			fileSize := int64(64 * 1024)
			server := testutil.NewMockServerT(t,
				testutil.WithFileSize(fileSize),
				testutil.WithRangeSupport(true), // server WOULD answer 206 if Range leaks through
				testutil.WithFilename("range_test.bin"),
			)
			defer server.Close()

			destPath := filepath.Join(tmpDir, "range_test.bin")
			state := progress.New("range-test", fileSize)
			runtime := &types.RuntimeConfig{WorkerBufferSize: 8 * utils.KiB}

			downloader := NewSingleDownloader("range-id", nil, state, runtime)
			downloader.Headers = map[string]string{headerKey: "bytes=100-"}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Pre-create incomplete file (simulating processing layer)
			if f, err := os.Create(destPath + ".surge"); err == nil {
				_ = f.Close()
			}

			err = downloader.Download(ctx, server.URL(), destPath, fileSize, "range_test.bin")
			if err != nil {
				t.Fatalf("Download failed; caller %q header should have been stripped: %v", headerKey, err)
			}

			// Whole file should be fetched, not a partial range.
			if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
				t.Error(err)
			}
			if state.Bytes.Downloaded.Load() != fileSize {
				t.Errorf("Downloaded %d != fileSize %d", state.Bytes.Downloaded.Load(), fileSize)
			}
		})
	}
}

func TestSingleDownloader_Download_Cancellation(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-cancel-single")
	defer cleanup()

	// Large file with latency
	fileSize := int64(5 * utils.MiB)
	requestStarted := make(chan struct{}, 1)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
		testutil.WithByteLatency(500*time.Microsecond),
		testutil.WithRequestStarted(requestStarted),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "cancel_single.bin")
	state := progress.New("cancel-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("cancel-id", nil, state, runtime)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		// Pre-create incomplete file (simulating processing layer)
		if f, err := os.Create(destPath + ".surge"); err == nil {
			_ = f.Close()
		}

		done <- downloader.Download(ctx, server.URL(), destPath, fileSize, "cancel.bin")
	}()

	<-requestStarted
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

func TestSingleDownloader_Download_ProgressTracking(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-progress-single")
	defer cleanup()

	fileSize := int64(256 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
		testutil.WithByteLatency(5*time.Microsecond), // Slow down to allow progress monitoring
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "progress_single.bin")
	state := progress.New("progress-single", fileSize)
	runtime := &types.RuntimeConfig{WorkerBufferSize: 16 * utils.KiB}

	downloader := NewSingleDownloader("progress-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "progress.bin")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify final progress equals file size
	finalProgress := state.Bytes.Downloaded.Load()
	if finalProgress != fileSize {
		t.Errorf("Final progress %d != file size %d", finalProgress, fileSize)
	}
	if state.Bytes.VerifiedProgress.Load() != fileSize {
		t.Errorf("Verified progress %d != file size %d", state.Bytes.VerifiedProgress.Load(), fileSize)
	}
	if state.ActiveWorkers.Load() != 0 {
		t.Errorf("ActiveWorkers = %d, want 0 (should clear after download completes)", state.ActiveWorkers.Load())
	}
}

func TestSingleDownloader_Download_ServerError(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-error-single")
	defer cleanup()

	// Server that fails on first request
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(1024),
		testutil.WithFailOnNthRequest(1),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "error_single.bin")
	state := progress.New("error-single", 1024)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("error-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, 1024, "error.bin")
	if err == nil {
		t.Error("Expected error from failed server")
	}
}

func TestSingleDownloader_Download_WithLatency(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-latency-single")
	defer cleanup()

	fileSize := int64(32 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
		testutil.WithLatency(100*time.Millisecond),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "latency_single.bin")
	state := progress.New("latency-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("latency-id", nil, state, runtime)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "latency.bin")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if elapsed < 100*time.Millisecond {
		t.Errorf("Download completed too fast (%v), latency not applied", elapsed)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}
}

func TestSingleDownloader_Download_ContentIntegrity(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-content-single")
	defer cleanup()

	fileSize := int64(64 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
		testutil.WithRandomData(true),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "content_single.bin")
	state := progress.New("content-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("content-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-create incomplete file (simulating processing layer)
	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "content.bin")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	// Verify content is not all zeros (random data was used)
	chunk, err := testutil.ReadFileChunk(destPath+types.IncompleteSuffix, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}

	allZero := true
	for _, b := range chunk {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("Content should not be all zeros with random data")
	}
}

// =============================================================================
// PreallocateFailure - file handle release
// =============================================================================

func TestSingleDownloader_PreallocateFailure_ReleasesFileHandle(t *testing.T) {
	// Cenário
	tmpDir, cleanup, _ := testutil.TempDir("surge-prealloc-fail")
	defer cleanup()

	fileSize := int64(64 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "prealloc_fail.bin")
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("prealloc-fail-id", nil, nil, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Cenário: criar o .surge como read-only para que preallocateFile (Truncate) falhe
	surgePath := destPath + types.IncompleteSuffix
	f, err := os.Create(surgePath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := os.Chmod(surgePath, 0o444); err != nil {
		t.Fatal(err)
	}
	// Restaurar permissões no cleanup para que TempDir possa remover
	defer func() { _ = os.Chmod(surgePath, 0o644) }()

	// Ação
	err = downloader.Download(ctx, server.URL(), destPath, fileSize, "prealloc_fail.bin")

	// Validação
	if err == nil {
		t.Fatal("Expected error when preallocate fails on read-only file")
	}
	if !strings.Contains(err.Error(), "preallocate") && !strings.Contains(err.Error(), "permission") {
		t.Logf("Got error: %v (acceptable - file handle should still be released)", err)
	}

	// Verificar que o file handle foi liberado: o arquivo pode ser removido
	_ = os.Chmod(surgePath, 0o644)
	if err := os.Remove(surgePath); err != nil {
		t.Errorf("Failed to remove .surge file after preallocate failure - possible file handle leak: %v", err)
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkSingleDownloader(b *testing.B) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-bench-single")
	defer cleanup()

	fileSize := int64(10 * utils.MiB)
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "bench_single.bin")
	state := progress.New("bench-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("bench-id", nil, state, runtime)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		// Pre-create incomplete file (simulating processing layer)
		if f, err := os.Create(destPath + ".surge"); err == nil {
			_ = f.Close()
		}

		err := downloader.Download(ctx, server.URL(), destPath, fileSize, "bench.bin")
		if err != nil {
			b.Fatalf("Download failed: %v", err)
		}
		cancel()
		_ = os.Remove(destPath)
	}
}

func TestSingleDownloader_Download_BootstrapSize(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-bootstrap-single")
	defer cleanup()

	expectedSize := int64(1024)
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", expectedSize))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "bootstrap_single.bin")
	state := progress.New("bootstrap-id", 0) // Unknown size
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("bootstrap-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL, destPath, 0, "bootstrap.bin")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if downloader.TotalSize != expectedSize {
		t.Errorf("Expected TotalSize %d, got %d", expectedSize, downloader.TotalSize)
	}
	if state.Bytes.TotalSize.Load() != expectedSize {
		t.Errorf("Expected state.Bytes.TotalSize %d, got %d", expectedSize, state.Bytes.TotalSize.Load())
	}
}

type stubLimiter struct {
	err error
}

func (s stubLimiter) WaitN(context.Context, int64) error {
	return s.err
}

type partialErrorReader struct {
	n   int
	err error
}

func (r partialErrorReader) Read(p []byte) (int, error) {
	if r.n > len(p) {
		r.n = len(p)
	}
	for i := 0; i < r.n; i++ {
		p[i] = byte(i)
	}
	return r.n, r.err
}

func TestThrottledReader_PreservesUnderlyingReadError(t *testing.T) {
	readErr := io.ErrUnexpectedEOF
	waitErr := errors.New("limiter wait failed")
	reader := &throttledReader{
		reader:  partialErrorReader{n: 7, err: readErr},
		limiter: stubLimiter{err: waitErr},
		ctx:     context.Background(),
	}

	buf := make([]byte, 16)
	n, err := reader.Read(buf)
	if n != 7 {
		t.Fatalf("Read bytes = %d, want 7", n)
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("Read error = %v, want %v", err, readErr)
	}
}

func TestThrottledReader_UsesLimiterErrorForCleanRead(t *testing.T) {
	waitErr := errors.New("limiter wait failed")
	reader := &throttledReader{
		reader:  partialErrorReader{n: 7, err: nil},
		limiter: stubLimiter{err: waitErr},
		ctx:     context.Background(),
	}

	buf := make([]byte, 16)
	n, err := reader.Read(buf)
	if n != 7 {
		t.Fatalf("Read bytes = %d, want 7", n)
	}
	if !errors.Is(err, waitErr) {
		t.Fatalf("Read error = %v, want %v", err, waitErr)
	}
}
