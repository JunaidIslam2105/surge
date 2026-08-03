//go:build windows

package types

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsDiskFull_Windows(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ERROR_DISK_FULL", &os.PathError{Op: "write", Path: "x", Err: windows.ERROR_DISK_FULL}, true},
		{"ERROR_DISK_QUOTA_EXCEEDED", &os.PathError{Op: "write", Path: "x", Err: windows.ERROR_DISK_QUOTA_EXCEEDED}, true},
		{"non-disk error", &os.PathError{Op: "write", Path: "x", Err: windows.ERROR_FILE_NOT_FOUND}, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDiskFull(tt.err); got != tt.want {
				t.Fatalf("isDiskFull(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
