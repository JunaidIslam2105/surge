package single

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
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

	err = copyFile(srcPath, dstPath)
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

	err := copyFile(filepath.Join(tmpDir, "nonexistent.bin"), filepath.Join(tmpDir, "dst.bin"))
	if err == nil {
		t.Error("Expected error for nonexistent source")
	}
}

func TestCopyFile_InvalidDestination(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-test")
	defer cleanup()

	srcPath, _ := testutil.CreateTestFile(tmpDir, "src.bin", 100, false)

	// Try to copy to an invalid path (non-existent directory)
	err := copyFile(srcPath, filepath.Join(tmpDir, "nonexistent", "subdir", "dst.bin"))
	if err == nil {
		t.Error("Expected error for invalid destination")
	}
}

func TestCopyFile_EmptyFile(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-copy-test")
	defer cleanup()

	srcPath, _ := testutil.CreateTestFile(tmpDir, "empty.bin", 0, false)
	dstPath := filepath.Join(tmpDir, "empty_copy.bin")

	err := copyFile(srcPath, dstPath)
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

	size := int64(5 * types.MB)
	srcPath, _ := testutil.CreateTestFile(tmpDir, "large.bin", size, false)
	dstPath := filepath.Join(tmpDir, "large_copy.bin")

	err := copyFile(srcPath, dstPath)
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

	size := int64(128 * types.KB)
	srcPath, _ := testutil.CreateTestFile(tmpDir, "random.bin", size, true) // Random data
	dstPath := filepath.Join(tmpDir, "random_copy.bin")

	err := copyFile(srcPath, dstPath)
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

// =============================================================================
// SingleDownloader - Streaming Server
// =============================================================================

func TestSingleDownloader_StreamingServer(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-stream-single")
	defer cleanup()

	fileSize := int64(1 * types.MB)
	server := testutil.NewStreamingMockServer(fileSize,
		testutil.WithRangeSupport(false),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "stream_single.bin")
	state := types.NewProgressState("stream-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("stream-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "stream.bin", false)
	if err != nil {
		t.Fatalf("Streaming download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// SingleDownloader - FailAfterBytes
// =============================================================================

func TestSingleDownloader_FailAfterBytes(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-failafter-single")
	defer cleanup()

	fileSize := int64(256 * types.KB)
	// Server fails after sending 50KB
	server := testutil.NewMockServer(
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(false),
		testutil.WithFailAfterBytes(50*types.KB),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "failafter_single.bin")
	state := types.NewProgressState("failafter-single", fileSize)
	runtime := &types.RuntimeConfig{}

	downloader := NewSingleDownloader("failafter-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "failafter.bin", false)
	// Should fail since SingleDownloader doesn't retry
	if err == nil {
		t.Error("Expected error when server fails mid-transfer")
	}

	// Partial file should exist with .surge suffix
	stats := server.Stats()
	if stats.BytesServed < 50*types.KB {
		t.Errorf("Expected at least 50KB served before failure, got %d", stats.BytesServed)
	}
}

// =============================================================================
// SingleDownloader - NilState handling
// =============================================================================

func TestSingleDownloader_NilState(t *testing.T) {
	tmpDir, cleanup, _ := testutil.TempDir("surge-nilstate-single")
	defer cleanup()

	fileSize := int64(32 * types.KB)
	server := testutil.NewMockServer(
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

	err := downloader.Download(ctx, server.URL(), destPath, fileSize, "nilstate.bin", false)
	if err != nil {
		t.Fatalf("Download with nil state failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}
