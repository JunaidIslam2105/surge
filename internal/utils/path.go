package utils

import (
	"path/filepath"
)

// EnsureAbsPath takes a clean path and forces it to be absolute.
// If it fails to get absolute path (rare), it checks if it's already absolute,
// otherwise relies on the input.
func EnsureAbsPath(path string) string {
	if path == "" {
		path = "."
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
