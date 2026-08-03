//go:build windows

package types

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func isDiskFull(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == windows.ERROR_DISK_FULL || errno == windows.ERROR_DISK_QUOTA_EXCEEDED
}
