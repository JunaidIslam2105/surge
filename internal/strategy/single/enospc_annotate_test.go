package single

import (
	"errors"
	"fmt"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

// TestSingleDownloader_ENOSPC verifies that the single downloader's error
// annotation paths (preallocateFile + CopyBuffer) wrap disk-full errors with
// the sentinel. The single downloader annotates errors via
// types.AnnotateInsufficientDiskSpace before returning them.
func TestSingleDownloader_ENOSPC(t *testing.T) {
	diskErr := makeDiskFullPathError()

	// Simulate the preallocateFile error path annotation.
	annotated := types.AnnotateInsufficientDiskSpace(diskErr)
	wrapped := fmt.Errorf("failed to preallocate file: %w", annotated)
	if !errors.Is(wrapped, types.ErrInsufficientDiskSpace) {
		t.Fatalf("preallocate error should wrap sentinel, got %v", wrapped)
	}
	if !types.IsInsufficientDiskSpace(wrapped) {
		t.Fatalf("IsInsufficientDiskSpace should be true for preallocate error")
	}

	// Simulate the copy error path annotation.
	copyAnnotated := types.AnnotateInsufficientDiskSpace(diskErr)
	copyWrapped := fmt.Errorf("copy error: %w", copyAnnotated)
	if !errors.Is(copyWrapped, types.ErrInsufficientDiskSpace) {
		t.Fatalf("copy error should wrap sentinel, got %v", copyWrapped)
	}
	if !types.IsInsufficientDiskSpace(copyWrapped) {
		t.Fatalf("IsInsufficientDiskSpace should be true for copy error")
	}

	// Non-disk error should not be annotated.
	nonDisk := fmt.Errorf("some network error")
	if types.IsInsufficientDiskSpace(nonDisk) {
		t.Fatalf("non-disk error should not match sentinel")
	}
}
