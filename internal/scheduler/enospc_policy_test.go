package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

var (
	diskFullErr      = makeDiskFullPathError()
	errWrappedDisk   = fmt.Errorf("write error: %w", diskFullErr)
	cancelErr        = context.Canceled
	deadlineErr      = context.DeadlineExceeded
	errPermanentHTTP = fmt.Errorf("status 404: %w", types.ErrPermanentHTTP)
)

func TestEnospcPolicy(t *testing.T) {
	t.Run("ExcludesENOSPC", func(t *testing.T) {
		tests := []struct {
			name      string
			err       error
			retries   int
			shutting  bool
			wantRetry bool
		}{
			{"ENOSPC excluded", errWrappedDisk, 0, false, false},
			{"raw ENOSPC errno excluded", diskFullErr, 0, false, false},
			{"permanent HTTP excluded", errPermanentHTTP, 0, false, false},
			{"cancel excluded", cancelErr, 0, false, false},
			{"deadline excluded", deadlineErr, 0, false, false},
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
	})

	t.Run("NoTruncateOnENOSPC", func(t *testing.T) {
		tests := []struct {
			name         string
			err          error
			downloaded   int64
			wantFallback bool
		}{
			{"ENOSPC with zero downloaded", errWrappedDisk, 0, false},
			{"ENOSPC with progress", errWrappedDisk, 100, false},
			{"paused excluded", types.ErrPaused, 0, false},
			{"cancel excluded", cancelErr, 0, false},
			{"deadline excluded", deadlineErr, 0, false},
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
	})
}
