package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestScheduler_ExcludesENOSPC(t *testing.T) {
	diskErr := makeDiskFullPathError()
	sentinelErr := types.AnnotateInsufficientDiskSpace(diskErr)
	wrappedErr := fmt.Errorf("write error: %w", sentinelErr)

	tests := []struct {
		name      string
		err       error
		retries   int
		shutting  bool
		wantRetry bool
	}{
		{"ENOSPC excluded", wrappedErr, 0, false, false},
		{"raw ENOSPC errno excluded", diskErr, 0, false, false},
		{"permanent HTTP excluded", fmt.Errorf("status 404: %w", types.ErrPermanentHTTP), 0, false, false},
		{"cancel excluded", context.Canceled, 0, false, false},
		{"deadline excluded", context.DeadlineExceeded, 0, false, false},
		{"shutdown excluded", errors.New("some error"), 0, true, false},
		{"max retries excluded", errors.New("some error"), 10, false, false},
		{"retryable error", errors.New("some transient error"), 0, false, true},
		{"retryable under limit", errors.New("some transient error"), 5, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryFailedDownload(tt.shutting, tt.err, tt.retries)
			if got != tt.wantRetry {
				t.Fatalf("shouldRetryFailedDownload(shutting=%v, err=%v, retries=%d) = %v, want %v", tt.shutting, tt.err, tt.retries, got, tt.wantRetry)
			}
		})
	}
}
