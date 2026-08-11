//go:build unix

package utils

import (
	"errors"
	"math"
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

// clampFreeBytesProduct multiplies blocks by block size without wrapping
// to a negative int64 (clamps to MaxInt64 on overflow).
func clampFreeBytesProduct(blocks, blockSize uint64) int64 {
	if blocks == 0 || blockSize == 0 {
		return 0
	}
	if blocks > math.MaxInt64/blockSize {
		return math.MaxInt64
	}
	return int64(blocks * blockSize)
}
