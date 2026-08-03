package scheduler

import (
	"errors"
	"testing"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestScheduler_EventErrorState(t *testing.T) {
	state := progress.New("state-test", 10*1024*1024)

	// Stash a pause-grade snapshot via SetPendingResumeState.
	snapshot := &types.DownloadRecord{
		URL:         "http://example.com/state.bin",
		DestPath:    "/tmp/state.bin",
		TotalSize:   10 * 1024 * 1024,
		Downloaded:  3 * 1024 * 1024,
		Workers:     4,
		MinChunkSize: 2 * 1024 * 1024,
	}
	state.SetPendingResumeState(snapshot)

	// TakePendingResumeState should return the snapshot and clear it.
	got := state.TakePendingResumeState()
	if got == nil {
		t.Fatal("expected non-nil pending resume state")
	}
	if got.Downloaded != 3*1024*1024 {
		t.Errorf("expected Downloaded=%d, got %d", 3*1024*1024, got.Downloaded)
	}
	if got.TotalSize != 10*1024*1024 {
		t.Errorf("expected TotalSize=%d, got %d", 10*1024*1024, got.TotalSize)
	}

	// Second Take should return nil (consumed).
	if got2 := state.TakePendingResumeState(); got2 != nil {
		t.Errorf("expected nil on second Take, got %+v", got2)
	}

	// Verify the errEvent fill logic: pending.Downloaded should populate errEvent.Downloaded.
	errEvent := &types.DownloadEvent{
		Type:       types.EventError,
		DownloadID: "state-test",
		Err:        errors.New("disk full"),
	}
	if pending := state.TakePendingResumeState(); pending != nil {
		errEvent.State = pending
		if pending.Downloaded > 0 {
			errEvent.Downloaded = pending.Downloaded
		}
	}
	// Since we already consumed the snapshot, errEvent.State should be nil.
	if errEvent.State != nil {
		t.Errorf("expected nil State after consumption, got %+v", errEvent.State)
	}

	// Now test with a fresh snapshot to verify the full fill.
	state.SetPendingResumeState(snapshot)
	errEvent2 := &types.DownloadEvent{
		Type:       types.EventError,
		DownloadID: "state-test",
		Err:        errors.New("disk full"),
	}
	if pending := state.TakePendingResumeState(); pending != nil {
		errEvent2.State = pending
		if pending.Downloaded > 0 {
			errEvent2.Downloaded = pending.Downloaded
		}
	}
	if errEvent2.State == nil {
		t.Fatal("expected non-nil State on errEvent2")
	}
	if errEvent2.State.Downloaded != 3*1024*1024 {
		t.Errorf("expected errEvent2.State.Downloaded=%d, got %d", 3*1024*1024, errEvent2.State.Downloaded)
	}
	if errEvent2.Downloaded != 3*1024*1024 {
		t.Errorf("expected errEvent2.Downloaded=%d, got %d", 3*1024*1024, errEvent2.Downloaded)
	}

	// SessionReset should clear pending resume state.
	state.SetPendingResumeState(snapshot)
	state.SessionReset()
	if got3 := state.TakePendingResumeState(); got3 != nil {
		t.Errorf("expected nil after SessionReset, got %+v", got3)
	}
}
