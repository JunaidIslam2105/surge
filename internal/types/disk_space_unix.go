//go:build unix

package types

import (
	"errors"
	"syscall"
)

func isDiskFull(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == syscall.ENOSPC || errno == syscall.EDQUOT
}
