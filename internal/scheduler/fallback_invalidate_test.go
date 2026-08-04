package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestManager_NoTruncateOnENOSPC(t *testing.T) {
	diskErr := makeDiskFullPathError()
	wrappedErr := fmt.Errorf("write error: %w", diskErr)

	tests := []struct {
		name         string
		err          error
		downloaded   int64
		wantFallback bool
	}{
		{"ENOSPC with zero downloaded", wrappedErr, 0, false},
		{"ENOSPC with progress", wrappedErr, 100, false},
		{"paused excluded", types.ErrPaused, 0, false},
		{"cancel excluded", context.Canceled, 0, false},
		{"deadline excluded", context.DeadlineExceeded, 0, false},
		{"nil error", nil, 0, false},
		{"retryable error with no progress", errors.New("network error"), 0, true},
		{"retryable error with progress", errors.New("network error"), 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFallbackToSingle(tt.err, tt.downloaded)
			if got != tt.wantFallback {
				t.Fatalf("shouldFallbackToSingle(err=%v, downloaded=%d) = %v, want %v", tt.err, tt.downloaded, got, tt.wantFallback)
			}
		})
	}
}
