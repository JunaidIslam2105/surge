package concurrent

import (
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestTaskQueue_PushPop(t *testing.T) {
	q := NewTaskQueue()

	task := types.Task{Offset: 0, Length: 1000}
	q.Push(task)

	if q.Len() != 1 {
		t.Errorf("Len = %d, want 1", q.Len())
	}

	got, ok := q.Pop()
	if !ok {
		t.Error("Pop returned false, expected true")
	}
	if got.Offset != task.Offset || got.Length != task.Length {
		t.Errorf("Pop = %+v, want %+v", got, task)
	}
}

func TestTaskQueue_PushMultiple(t *testing.T) {
	q := NewTaskQueue()

	tasks := []types.Task{
		{Offset: 0, Length: 100},
		{Offset: 100, Length: 100},
		{Offset: 200, Length: 100},
	}
	q.PushMultiple(tasks)

	if q.Len() != 3 {
		t.Errorf("Len = %d, want 3", q.Len())
	}
}

func TestTaskQueue_IdleWorkers(t *testing.T) {
	q := NewTaskQueue()

	// Initially 0 idle workers
	if q.IdleWorkers() != 0 {
		t.Errorf("IdleWorkers = %d, want 0", q.IdleWorkers())
	}
}

func TestTaskQueue_Close(t *testing.T) {
	q := NewTaskQueue()
	q.Push(types.Task{Offset: 0, Length: 100})
	q.Close()

	// After close, Pop should still return existing tasks
	if _, ok := q.Pop(); !ok {
		t.Error("Pop should return existing task after Close")
	}

	// Additional Pop should return false
	if _, ok := q.Pop(); ok {
		t.Error("Pop should return false after draining closed queue")
	}
}

func TestTaskQueue_DrainRemaining(t *testing.T) {
	q := NewTaskQueue()

	tasks := []types.Task{
		{Offset: 0, Length: 100},
		{Offset: 100, Length: 100},
		{Offset: 200, Length: 100},
	}
	q.PushMultiple(tasks)

	remaining := q.DrainRemaining()

	if len(remaining) != 3 {
		t.Errorf("DrainRemaining returned %d tasks, want 3", len(remaining))
	}
	if q.Len() != 0 {
		t.Errorf("Queue should be empty after drain, Len = %d", q.Len())
	}
}

func TestAlignedSplitSize(t *testing.T) {
	tests := []struct {
		remaining int64
		wantZero  bool
	}{
		{types.MinChunk, true},       // Too small to split (half < MinChunk)
		{2 * types.MinChunk, false},  // Half is MinChunk, valid split
		{4 * types.MinChunk, false},  // Should produce valid split
		{10 * types.MinChunk, false}, // Should produce valid split
	}

	for _, tt := range tests {
		got := alignedSplitSize(tt.remaining, types.MinChunk)
		if tt.wantZero && got != 0 {
			t.Errorf("alignedSplitSize(%d) = %d, want 0", tt.remaining, got)
		}
		if !tt.wantZero && got == 0 {
			t.Errorf("alignedSplitSize(%d) = 0, want non-zero", tt.remaining)
		}
		// Verify alignment
		if got != 0 && got%types.AlignSize != 0 {
			t.Errorf("alignedSplitSize(%d) = %d, not aligned to %d", tt.remaining, got, types.AlignSize)
		}
	}
}

func TestAlignedSplitSize_UsesRuntimeMinimum(t *testing.T) {
	minChunk := int64(64 * 1024)
	if got := alignedSplitSize(2*minChunk, minChunk); got != minChunk {
		t.Fatalf("alignedSplitSize(%d, %d) = %d, want %d", 2*minChunk, minChunk, got, minChunk)
	}
}

func TestStealWork_AdaptiveTailFloor(t *testing.T) {
	const (
		workerID  = 3
		current   = 2 * types.AlignSize
		remaining = 15 * types.AlignSize
		minChunk  = 16 * types.AlignSize
	)
	originalEnd := int64(current + remaining)

	newDownloader := func(adaptive bool) (*ConcurrentDownloader, *ActiveTask) {
		runtime := &types.RuntimeConfig{MinChunkSize: minChunk}
		if adaptive {
			runtime.AdaptiveConcurrencyInterval = time.Second
		}
		downloader := NewConcurrentDownloader("steal-tail", nil, nil, runtime)
		active := &ActiveTask{Task: types.Task{Offset: 0, Length: originalEnd}}
		active.CurrentOffset.Store(current)
		active.StopAt.Store(originalEnd)
		downloader.activeTasks[workerID] = active
		return downloader, active
	}

	disabled, disabledActive := newDownloader(false)
	disabledQueue := NewTaskQueue()
	if disabled.StealWork(disabledQueue) {
		t.Fatal("disabled adaptation stole a sub-MinChunkSize tail")
	}
	if disabledQueue.Len() != 0 || disabledActive.StopAt.Load() != originalEnd {
		t.Fatal("disabled adaptation changed the active range")
	}

	enabled, enabledActive := newDownloader(true)
	enabledQueue := NewTaskQueue()
	if !enabled.StealWork(enabledQueue) {
		t.Fatal("enabled adaptation did not steal an aligned tail range")
	}
	stolenTasks := enabledQueue.DrainRemaining()
	if len(stolenTasks) != 1 {
		t.Fatalf("stolen task count = %d, want 1", len(stolenTasks))
	}
	stolen := stolenTasks[0]
	newStopAt := enabledActive.StopAt.Load()
	if newStopAt%types.AlignSize != 0 || stolen.Offset%types.AlignSize != 0 || stolen.Length%types.AlignSize != 0 {
		t.Fatalf("unaligned split: stop=%d stolen=%+v align=%d", newStopAt, stolen, types.AlignSize)
	}
	if stolen.Offset != newStopAt {
		t.Fatalf("gap or overlap at split: original stops at %d, stolen starts at %d", newStopAt, stolen.Offset)
	}
	if got := (newStopAt - current) + stolen.Length; got != remaining {
		t.Fatalf("covered bytes = %d, want %d", got, remaining)
	}
	if got := stolen.Offset + stolen.Length; got != originalEnd {
		t.Fatalf("stolen range ends at %d, want %d", got, originalEnd)
	}

	throttled, throttledActive := newDownloader(true)
	throttled.concurrencyGate = newAdaptiveConcurrencyGate(2, time.Second)
	throttled.concurrencyGate.throttle(time.Now(), time.Now())
	throttledQueue := NewTaskQueue()
	if throttled.StealWork(throttledQueue) {
		t.Fatal("throttled adaptation stole a sub-MinChunkSize tail")
	}
	if throttledQueue.Len() != 0 || throttledActive.StopAt.Load() != originalEnd {
		t.Fatal("throttled adaptation changed the active range")
	}
}

func TestDetachRemainingTaskRemovesTaskFromStealSet(t *testing.T) {
	runtime := &types.RuntimeConfig{
		MinChunkSize:                4 * types.AlignSize,
		AdaptiveConcurrencyInterval: time.Second,
	}
	downloader := NewConcurrentDownloader("detach-before-steal", nil, nil, runtime)
	active := &ActiveTask{Task: types.Task{Offset: 0, Length: 16 * types.AlignSize}}
	active.CurrentOffset.Store(4 * types.AlignSize)
	active.StopAt.Store(16 * types.AlignSize)
	downloader.activeTasks[1] = active

	remaining := downloader.detachRemainingTask(1, active)
	if remaining == nil || remaining.Offset != 4*types.AlignSize || remaining.Length != 12*types.AlignSize {
		t.Fatalf("detached remaining task = %+v", remaining)
	}
	queue := NewTaskQueue()
	if downloader.StealWork(queue) {
		t.Fatal("detached task remained visible to StealWork")
	}
	if queue.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", queue.Len())
	}
}
