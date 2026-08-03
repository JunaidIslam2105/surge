//go:build unix

package utils

import "syscall"

func freeDiskBytesAt(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	// Bavail: free blocks available to unprivileged writer.
	return clampFreeBytesProduct(uint64(st.Bavail), uint64(st.Bsize)), nil
}
