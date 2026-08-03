//go:build unix

package single

import (
	"os"
	"syscall"
)

func makeDiskFullPathError() error {
	return &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}
}
