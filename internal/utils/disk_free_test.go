package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasSufficientDiskSpace(t *testing.T) {
	tests := []struct {
		name     string
		fileSize int64
		free     int64
		buffer   int64
		want     bool
	}{
		{"unknown size always ok", 0, 100, 50, true},
		{"negative size always ok", -1, 100, 50, true},
		{"free <= buffer rejected", 1, 50, 50, false},
		{"free < buffer rejected", 1, 49, 50, false},
		{"size fits with buffer", 40, 100, 50, true},
		{"size exactly fits", 50, 100, 50, true},
		{"size exceeds headroom", 51, 100, 50, false},
		{"zero buffer exact fit", 100, 100, 0, true},
		{"zero buffer over", 101, 100, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSufficientDiskSpace(tt.fileSize, tt.free, tt.buffer); got != tt.want {
				t.Fatalf("HasSufficientDiskSpace(%d, %d, %d) = %v, want %v", tt.fileSize, tt.free, tt.buffer, got, tt.want)
			}
		})
	}
}

func TestFreeDiskBytes(t *testing.T) {
	// A not-yet-created subdirectory should walk to an existing ancestor.
	tmp := os.TempDir()
	nonExistent := filepath.Join(tmp, "surge_disk_free_nonexistent_subdir_for_test")
	free, err := FreeDiskBytes(nonExistent)
	if err != nil {
		t.Fatalf("FreeDiskBytes(%q) failed: %v", nonExistent, err)
	}
	if free < 0 {
		t.Fatalf("FreeDiskBytes returned negative: %d", free)
	}

	// Empty path should error.
	if _, err := FreeDiskBytes(""); err == nil {
		t.Fatalf("FreeDiskBytes(\"\") should error")
	}
}
