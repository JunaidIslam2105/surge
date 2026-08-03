package types

import (
	"errors"
	"io"
	"testing"
)

func TestAnnotateInsufficientDiskSpace(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if got := AnnotateInsufficientDiskSpace(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("non-disk error passes through", func(t *testing.T) {
		orig := io.ErrUnexpectedEOF
		got := AnnotateInsufficientDiskSpace(orig)
		if !errors.Is(got, orig) {
			t.Fatalf("expected original error preserved, got %v", got)
		}
		if errors.Is(got, ErrInsufficientDiskSpace) {
			t.Fatalf("non-disk error should not be annotated")
		}
	})

	t.Run("already annotated is idempotent", func(t *testing.T) {
		annotated := AnnotateInsufficientDiskSpace(diskFullErr())
		again := AnnotateInsufficientDiskSpace(annotated)
		if again != annotated {
			t.Fatalf("idempotent annotation should return same error, got %v vs %v", again, annotated)
		}
	})

	t.Run("disk-full error is annotated with sentinel", func(t *testing.T) {
		annotated := AnnotateInsufficientDiskSpace(diskFullErr())
		if !errors.Is(annotated, ErrInsufficientDiskSpace) {
			t.Fatalf("expected ErrInsufficientDiskSpace wrap, got %v", annotated)
		}
		if !IsInsufficientDiskSpace(annotated) {
			t.Fatalf("IsInsufficientDiskSpace should be true for annotated error")
		}
	})

	t.Run("IsInsufficientDiskSpace false for non-disk error", func(t *testing.T) {
		if IsInsufficientDiskSpace(io.ErrUnexpectedEOF) {
			t.Fatalf("IsInsufficientDiskSpace should be false for non-disk error")
		}
		if IsInsufficientDiskSpace(nil) {
			t.Fatalf("IsInsufficientDiskSpace should be false for nil")
		}
	})
}

// diskFullErr returns a platform disk-full error wrapped in *os.PathError.
func diskFullErr() error {
	return makeDiskFullPathError()
}
