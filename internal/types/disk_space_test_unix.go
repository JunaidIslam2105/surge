//go:build unix

package types

import (
	"os"
	"syscall"
)

func makeDiskFullPathError() error {
	return &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}
}
