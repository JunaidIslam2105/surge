package concurrent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestActiveTask_RemainingBytes(t *testing.T) {
	at := &ActiveTask{
		Task:          types.Task{Offset: 0, Length: 1000},
		CurrentOffset: 0,
		StopAt:        1000,
	}

	// Initially full remaining
	if got := at.RemainingBytes(); got != 1000 {
		t.Errorf("RemainingBytes = %d, want 1000", got)
	}

	// After some progress
	atomic.StoreInt64(&at.CurrentOffset, 400)
	if got := at.RemainingBytes(); got != 600 {
		t.Errorf("RemainingBytes = %d, want 600", got)
	}

	// Completed
	atomic.StoreInt64(&at.CurrentOffset, 1000)
	if got := at.RemainingBytes(); got != 0 {
		t.Errorf("RemainingBytes = %d, want 0", got)
	}
}

func TestActiveTask_RemainingTask(t *testing.T) {
	at := &ActiveTask{
		Task:          types.Task{Offset: 0, Length: 1000},
		CurrentOffset: 0,
		StopAt:        1000,
	}

	// Initially full task remaining
	remaining := at.RemainingTask()
	if remaining == nil {
		t.Fatal("RemainingTask returned nil")
	}
	if remaining.Offset != 0 || remaining.Length != 1000 {
		t.Errorf("RemainingTask = %+v, want Offset=0, Length=1000", remaining)
	}

	// After some progress
	atomic.StoreInt64(&at.CurrentOffset, 600)
	remaining = at.RemainingTask()
	if remaining.Offset != 600 || remaining.Length != 400 {
		t.Errorf("RemainingTask = %+v, want Offset=600, Length=400", remaining)
	}

	// Completed
	atomic.StoreInt64(&at.CurrentOffset, 1000)
	if at.RemainingTask() != nil {
		t.Error("RemainingTask should return nil when complete")
	}
}

func TestActiveTask_GetSpeed(t *testing.T) {
	at := &ActiveTask{
		Speed: 1024.0 * 1024.0, // 1 MB/s
	}

	if got := at.GetSpeed(); got != 1024.0*1024.0 {
		t.Errorf("GetSpeed = %f, want %f", got, 1024.0*1024.0)
	}
}

func TestActiveTask_RemainingBytesWithStolenWork(t *testing.T) {
	at := &ActiveTask{
		Task:          types.Task{Offset: 0, Length: 1000},
		CurrentOffset: 200,
		StopAt:        500, // Work was stolen, stop early
	}

	// Should only count up to StopAt
	if got := at.RemainingBytes(); got != 300 {
		t.Errorf("RemainingBytes = %d, want 300 (500 - 200)", got)
	}

	// After passing StopAt
	atomic.StoreInt64(&at.CurrentOffset, 500)
	if got := at.RemainingBytes(); got != 0 {
		t.Errorf("RemainingBytes = %d, want 0", got)
	}
}

func TestActiveTask_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	at := &ActiveTask{
		Task:   types.Task{Offset: 0, Length: 1000},
		Cancel: cancel,
	}

	// Verify context is not cancelled
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled")
	default:
	}

	// Cancel the task
	at.Cancel()

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be cancelled after Cancel()")
	}
}

func TestActiveTask_WindowTracking(t *testing.T) {
	now := time.Now()
	at := &ActiveTask{
		Task:        types.Task{Offset: 0, Length: 1000},
		WindowStart: now,
		WindowBytes: 0,
	}

	// Add bytes to window
	atomic.AddInt64(&at.WindowBytes, 500)

	if at.WindowBytes != 500 {
		t.Errorf("WindowBytes = %d, want 500", at.WindowBytes)
	}

	// Swap and reset (as done in worker)
	bytes := atomic.SwapInt64(&at.WindowBytes, 0)
	if bytes != 500 {
		t.Errorf("Swapped bytes = %d, want 500", bytes)
	}
	if at.WindowBytes != 0 {
		t.Errorf("WindowBytes after swap = %d, want 0", at.WindowBytes)
	}
}
