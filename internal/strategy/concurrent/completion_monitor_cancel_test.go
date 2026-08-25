package concurrent

import (
	"context"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
)

func TestRunCompletionMonitor_DownloadedCancelsActives(t *testing.T) {
	const fileSize int64 = 1024
	queue := NewTaskQueue()
	state := progress.New("monitor-downloaded-cancel", fileSize)
	state.Bytes.Downloaded.Store(fileSize)

	taskCtx1, cancel1 := context.WithCancel(context.Background())
	taskCtx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	d := &ConcurrentDownloader{
		State:       state,
		activeTasks: map[int]*ActiveTask{0: {Cancel: cancel1}, 1: {Cancel: cancel2}},
	}

	monCtx, monCancel := context.WithCancel(context.Background())
	defer monCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runCompletionMonitor(monCtx, queue, fileSize, 4)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("completion monitor did not return after Downloaded reached file size")
	}

	select {
	case <-taskCtx1.Done():
	default:
		t.Fatal("active task 0 was not cancelled")
	}
	select {
	case <-taskCtx2.Done():
	default:
		t.Fatal("active task 1 was not cancelled")
	}
}

func TestRunCompletionMonitor_IdleCompletionStillCloses(t *testing.T) {
	queue := NewTaskQueue()
	d := &ConcurrentDownloader{activeTasks: map[int]*ActiveTask{}}

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_, _ = queue.Pop()
	}()
	deadline := time.Now().Add(time.Second)
	for queue.IdleWorkers() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if queue.IdleWorkers() != 1 {
		t.Fatal("queue worker did not become idle")
	}

	monCtx, monCancel := context.WithCancel(context.Background())
	defer monCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runCompletionMonitor(monCtx, queue, 1, 1)
	}()

	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("idle completion did not close the queue")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("completion monitor did not exit on idle completion")
	}
}

func TestRunCompletionMonitor_VerifiedProgressIsNotCompletionKey(t *testing.T) {
	const fileSize int64 = 1024
	queue := NewTaskQueue()
	state := progress.New("monitor-vp-not-done", fileSize)
	state.Bytes.VerifiedProgress.Store(fileSize)
	state.Bytes.Downloaded.Store(0)

	d := &ConcurrentDownloader{
		State:       state,
		activeTasks: map[int]*ActiveTask{},
	}

	monCtx, monCancel := context.WithCancel(context.Background())
	defer monCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runCompletionMonitor(monCtx, queue, fileSize, 2)
	}()

	select {
	case <-done:
		t.Fatal("completion monitor returned on VerifiedProgress without Downloaded")
	case <-time.After(300 * time.Millisecond):
	}

	monCancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("completion monitor did not exit after parent cancel")
	}
}

func TestRunCompletionMonitor_NilCancelDoesNotPanic(t *testing.T) {
	const fileSize int64 = 1024
	queue := NewTaskQueue()
	state := progress.New("monitor-nil-cancel", fileSize)
	state.Bytes.Downloaded.Store(fileSize)

	taskCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := &ConcurrentDownloader{
		State: state,
		activeTasks: map[int]*ActiveTask{
			0: {},
			1: {Cancel: nil},
			2: {Cancel: cancel},
		},
	}

	monCtx, monCancel := context.WithCancel(context.Background())
	defer monCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runCompletionMonitor(monCtx, queue, fileSize, 4)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("completion monitor panicked or hung on nil Cancel")
	}
	select {
	case <-taskCtx.Done():
	default:
		t.Fatal("non-nil Cancel was not invoked")
	}
}
