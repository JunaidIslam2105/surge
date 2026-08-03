//go:build unix

package types

import (
	"os"
	"syscall"
	"testing"
)

func TestIsDiskFull_Unix(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ENOSPC", &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}, true},
		{"EDQUOT", &os.PathError{Op: "write", Path: "x", Err: syscall.EDQUOT}, true},
		{"EIO not disk-full", &os.PathError{Op: "write", Path: "x", Err: syscall.EIO}, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDiskFull(tt.err); got != tt.want {
				t.Fatalf("isDiskFull(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
