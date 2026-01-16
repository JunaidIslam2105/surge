package types

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewProgressState(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)

	if ps.ID != "test-id-1" {
		t.Errorf("ID = %s, want test-id-1", ps.ID)
	}
	if ps.TotalSize != 1000000 {
		t.Errorf("TotalSize = %d, want 1000000", ps.TotalSize)
	}
	if ps.Downloaded.Load() != 0 {
		t.Errorf("Downloaded = %d, want 0", ps.Downloaded.Load())
	}
	if ps.IsPaused() {
		t.Error("New ProgressState should not be paused")
	}
	if ps.Done.Load() {
		t.Error("New ProgressState should not be done")
	}
}

func TestProgressState_SetTotalSize(t *testing.T) {
	ps := NewProgressState("test-id-1", 0)
	oldStartTime := ps.StartTime

	// Small delay to ensure start time changes
	time.Sleep(10 * time.Millisecond)

	ps.SetTotalSize(5000000)

	if ps.TotalSize != 5000000 {
		t.Errorf("TotalSize = %d, want 5000000", ps.TotalSize)
	}
	if !ps.StartTime.After(oldStartTime) {
		t.Error("StartTime should be reset when SetTotalSize is called")
	}
}

func TestProgressState_Downloaded(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)

	// Simulate downloading
	ps.Downloaded.Add(100000)
	ps.Downloaded.Add(200000)

	if ps.Downloaded.Load() != 300000 {
		t.Errorf("Downloaded = %d, want 300000", ps.Downloaded.Load())
	}
}

func TestProgressState_ActiveWorkers(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)

	ps.ActiveWorkers.Add(1)
	ps.ActiveWorkers.Add(1)

	if ps.ActiveWorkers.Load() != 2 {
		t.Errorf("ActiveWorkers = %d, want 2", ps.ActiveWorkers.Load())
	}

	ps.ActiveWorkers.Add(-1)

	if ps.ActiveWorkers.Load() != 1 {
		t.Errorf("ActiveWorkers = %d, want 1", ps.ActiveWorkers.Load())
	}
}

func TestProgressState_Error(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)

	// Initially no error
	if ps.GetError() != nil {
		t.Error("Initial error should be nil")
	}

	// Set an error
	testErr := errors.New("test error")
	ps.SetError(testErr)

	if ps.GetError() == nil {
		t.Error("GetError should return the set error")
	}
	if ps.GetError().Error() != "test error" {
		t.Errorf("Error message = %s, want 'test error'", ps.GetError().Error())
	}
}

func TestProgressState_PauseResume(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)

	// Set up a cancel function
	ctx, cancel := context.WithCancel(context.Background())
	ps.CancelFunc = cancel

	if ps.IsPaused() {
		t.Error("Should not be paused initially")
	}

	// Pause
	ps.Pause()

	if !ps.IsPaused() {
		t.Error("Should be paused after Pause()")
	}

	// Check context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be cancelled after Pause()")
	}

	// Resume
	ps.Resume()

	if ps.IsPaused() {
		t.Error("Should not be paused after Resume()")
	}
}

func TestProgressState_GetProgress(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)
	ps.Downloaded.Store(500000)
	ps.ActiveWorkers.Store(4)

	downloaded, total, elapsed, connections, sessionStart := ps.GetProgress()

	if downloaded != 500000 {
		t.Errorf("downloaded = %d, want 500000", downloaded)
	}
	if total != 1000000 {
		t.Errorf("total = %d, want 1000000", total)
	}
	// elapsed can be 0 on fast machines, just check it's not negative
	if elapsed < 0 {
		t.Error("elapsed should not be negative")
	}
	if connections != 4 {
		t.Errorf("connections = %d, want 4", connections)
	}
	// For new state, sessionStart should be 0
	if sessionStart != 0 {
		t.Errorf("sessionStart = %d, want 0", sessionStart)
	}
}

func TestProgressState_Done(t *testing.T) {
	ps := NewProgressState("test-id-1", 1000000)

	if ps.Done.Load() {
		t.Error("Should not be done initially")
	}

	ps.Done.Store(true)

	if !ps.Done.Load() {
		t.Error("Should be done after setting")
	}
}
