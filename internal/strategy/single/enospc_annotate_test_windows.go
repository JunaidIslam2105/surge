//go:build windows

package single

import (
	"os"

	"golang.org/x/sys/windows"
)

func makeDiskFullPathError() error {
	return &os.PathError{Op: "write", Path: "x", Err: windows.ERROR_DISK_FULL}
}
