//go:build unix

package utils

import (
	"errors"
	"syscall"
)

// IsOSDiskFull reports whether the error is a unix disk full error.
func IsOSDiskFull(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == syscall.ENOSPC || errno == syscall.EDQUOT
}

func freeDiskBytesAt(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	// Bavail: free blocks available to unprivileged writer.
	return clampFreeBytesProduct(uint64(st.Bavail), uint64(st.Bsize)), nil
}
