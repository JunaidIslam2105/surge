package utils

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveFile_WindowsRetry_Success(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "testfile_retry.txt")

	// Create a file and hold it open to lock it
	f, err := os.Create(file)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Start a goroutine to close the file after a short delay
	// This will cause the first few Remove attempts to fail,
	// but an attempt after 200ms should succeed.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = f.Close()
	}()

	// Remove it (should block, retry, and eventually succeed)
	if err := RemoveFile(file); err != nil {
		t.Fatalf("RemoveFile failed despite retry: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("File still exists after RemoveFile")
	}
}

func TestRemoveFile_WindowsRetry_Exhausted(t *testing.T) {
	dir := t.TempDir()
	blockedDir := filepath.Join(dir, "non-empty")
	if err := os.Mkdir(blockedDir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	child := filepath.Join(blockedDir, "child.txt")
	if err := os.WriteFile(child, []byte("keep"), 0o644); err != nil {
		t.Fatalf("Failed to create directory child: %v", err)
	}

	// A non-empty directory is a stable, non-transient removal failure on
	// Windows; unlike an open file, its locking behavior does not vary with
	// the runtime's file-sharing flags.
	if err := RemoveFile(blockedDir); err == nil {
		t.Fatal("RemoveFile succeeded unexpectedly for a non-empty directory")
	}
}
