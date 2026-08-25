package concurrent

import (
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestRetryStopAt_ClampedToActiveStopAt(t *testing.T) {
	task := types.Task{Offset: 0, Length: 80}
	active := &ActiveTask{Task: task}
	active.CurrentOffset.Store(20)
	active.StopAt.Store(50)

	resumeOnRetryOffset(&task, active)

	if task.Offset+task.Length > active.StopAt.Load() {
		t.Fatalf("task end=%d not clamped to StopAt=%d", task.Offset+task.Length, active.StopAt.Load())
	}
	if task.Offset != 20 {
		t.Fatalf("task.Offset=%d, want 20 (current)", task.Offset)
	}
	if task.Length != 30 {
		t.Fatalf("task.Length=%d, want 30", task.Length)
	}
	if active.Task != task {
		t.Fatal("activeTask.Task was not published")
	}
}

func TestResumeOnRetryOffset_NoProgressClampsToStopAt(t *testing.T) {
	task := types.Task{Offset: 10, Length: 80}
	active := &ActiveTask{Task: task}
	active.CurrentOffset.Store(10)
	active.StopAt.Store(40)

	resumeOnRetryOffset(&task, active)

	if task.Offset+task.Length > active.StopAt.Load() {
		t.Fatalf("task end=%d not clamped to StopAt=%d (no-progress case)",
			task.Offset+task.Length, active.StopAt.Load())
	}
	if task.Offset != 10 {
		t.Fatalf("task.Offset=%d, want 10 (no progress)", task.Offset)
	}
	if task.Length != 30 {
		t.Fatalf("task.Length=%d, want 30 (clamped even with no progress)", task.Length)
	}
}
