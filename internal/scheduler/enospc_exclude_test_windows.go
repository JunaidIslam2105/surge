//go:build windows

package scheduler

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func makeDiskFullPathError() error {
	return &os.PathError{Op: "write", Path: "x", Err: windows.ERROR_DISK_FULL}
}

func TestScheduler_ExcludesENOSPC_Windows(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ERROR_DISK_FULL", windows.ERROR_DISK_FULL},
		{"ERROR_DISK_QUOTA_EXCEEDED", windows.ERROR_DISK_QUOTA_EXCEEDED},
		{"ERROR_HANDLE_DISK_FULL", windows.ERROR_HANDLE_DISK_FULL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryFailedDownload(false, tt.err, 0); got != false {
				t.Fatalf("shouldRetryFailedDownload(err=%v) = %v, want false", tt.err, got)
			}
		})
	}
}
