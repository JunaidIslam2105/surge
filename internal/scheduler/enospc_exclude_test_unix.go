//go:build unix

package scheduler

import (
	"os"
	"syscall"
)

func makeDiskFullPathError() error {
	return &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}
}
