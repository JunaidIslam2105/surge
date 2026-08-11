package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HasSufficientDiskSpace reports whether freeBytes can hold fileSize
// plus the safety buffer. fileSize <= 0 returns true (unknown size).
// When freeBytes <= buffer, any positive fileSize is rejected
// (underflow-safe: headroom treated as zero).
func HasSufficientDiskSpace(fileSize, freeBytes, buffer int64) bool {
	if fileSize <= 0 {
		return true
	}
	if freeBytes <= buffer {
		return false
	}
	return fileSize <= freeBytes-buffer
}

// FreeDiskBytes returns available bytes on the volume owning path.
// Walks to an existing ancestor when path is not yet created.
func FreeDiskBytes(path string) (int64, error) {
	if path == "" {
		return 0, fmt.Errorf("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}

	var lastErr error
	for {
		free, err := freeDiskBytesAt(abs)
		if err == nil {
			return free, nil
		}
		lastErr = err

		if _, statErr := os.Stat(abs); !errors.Is(statErr, os.ErrNotExist) {
			return 0, lastErr
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return 0, lastErr
		}
		abs = parent
	}
}
