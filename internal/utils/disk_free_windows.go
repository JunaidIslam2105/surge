//go:build windows

package utils

import (
	"errors"
	"math"
	"syscall"

	"golang.org/x/sys/windows"
)

// IsOSDiskFull reports whether the error is a windows disk full error.
func IsOSDiskFull(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == windows.ERROR_DISK_FULL || errno == windows.ERROR_DISK_QUOTA_EXCEEDED
}

func freeDiskBytesAt(path string) (int64, error) {
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes); err != nil {
		return 0, err
	}
	// freeBytesAvailable is quota-aware (caller's usable free space).
	if freeBytesAvailable > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(freeBytesAvailable), nil
}
