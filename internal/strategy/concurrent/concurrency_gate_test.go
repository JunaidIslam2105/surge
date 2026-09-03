package concurrent

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveConcurrencyGateThrottleEpisodes(t *testing.T) {
	g := newAdaptiveConcurrencyGate(8, 15*time.Second)
	now := time.Unix(100, 0)

	oldCap, newCap, episode := g.throttle(now, now.Add(5*time.Second))
	if oldCap != 8 || newCap != 4 || !episode {
		t.Fatalf("first throttle = (%d, %d, %v), want (8, 4, true)", oldCap, newCap, episode)
	}

	oldCap, newCap, episode = g.throttle(now.Add(time.Second), now.Add(8*time.Second))
	if oldCap != 4 || newCap != 4 || episode {
		t.Fatalf("same burst = (%d, %d, %v), want (4, 4, false)", oldCap, newCap, episode)
	}

	oldCap, newCap, episode = g.throttle(now.Add(8*time.Second), now.Add(10*time.Second))
	if oldCap != 4 || newCap != 2 || !episode {
		t.Fatalf("second episode = (%d, %d, %v), want (4, 2, true)", oldCap, newCap, episode)
	}

	oldCap, newCap, episode = g.throttle(now.Add(11*time.Second), now.Add(12*time.Second))
	if oldCap != 2 || newCap != 1 || !episode {
		t.Fatalf("third episode = (%d, %d, %v), want (2, 1, true)", oldCap, newCap, episode)
	}
}

func TestAdaptiveConcurrencyGateRecoversOnePerHealthyWindow(t *testing.T) {
	recoveryWindow := 15 * time.Second
	g := newAdaptiveConcurrencyGate(8, recoveryWindow)
	now := time.Unix(200, 0)
	g.throttle(now, now.Add(2*time.Second))

	if oldCap, newCap, recovered := g.recover(now.Add(recoveryWindow - time.Nanosecond)); oldCap != 4 || newCap != 4 || recovered {
		t.Fatalf("early success = (%d, %d, %v), want (4, 4, false)", oldCap, newCap, recovered)
	}

	for want := 5; want <= 8; want++ {
		successAt := now.Add(time.Duration(want-4) * recoveryWindow)
		oldCap, newCap, recovered := g.recover(successAt)
		if oldCap != want-1 || newCap != want {
			t.Fatalf("success at %v = (%d, %d), want (%d, %d)", successAt, oldCap, newCap, want-1, want)
		}
		if recovered != (want == 8) {
			t.Fatalf("recovered at cap %d = %v", want, recovered)
		}
		if want < 8 {
			_, earlyCap, _ := g.recover(successAt.Add(recoveryWindow - time.Nanosecond))
			if earlyCap != want {
				t.Fatalf("cap increased early from %d to %d", want, earlyCap)
			}
		}
	}

	if _, capAfterRecovery, recovered := g.recover(now.Add(10 * recoveryWindow)); capAfterRecovery != 8 || recovered {
		t.Fatalf("post-recovery success = cap %d, recovered %v; want cap 8 and no transition", capAfterRecovery, recovered)
	}
}

func TestAdaptiveConcurrencyGateCanBeDisabled(t *testing.T) {
	g := newAdaptiveConcurrencyGate(8, 0)
	now := time.Unix(250, 0)

	oldCap, newCap, _ := g.throttle(now, now.Add(time.Second))
	if oldCap != 8 || newCap != 8 {
		t.Fatalf("disabled throttle changed cap from %d to %d", oldCap, newCap)
	}
	if _, capAfterWindow, recovered := g.recover(now.Add(time.Second)); capAfterWindow != 8 || !recovered {
		t.Fatalf("disabled recovery = cap %d, recovered %v; want cap 8 and recovered", capAfterWindow, recovered)
	}
}

func TestAdaptiveConcurrencyGateRecoversOnTimerWithoutTaskCompletion(t *testing.T) {
	const window = 20 * time.Millisecond
	g := newAdaptiveConcurrencyGate(4, window)
	now := time.Now()
	g.throttle(now, now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type transition struct {
		oldCap, newCap int
		recovered      bool
	}
	transitions := make(chan transition, 2)
	go g.runRecovery(ctx, func(oldCap, newCap int, recovered bool) {
		transitions <- transition{oldCap: oldCap, newCap: newCap, recovered: recovered}
	})

	for _, want := range []transition{{oldCap: 2, newCap: 3}, {oldCap: 3, newCap: 4, recovered: true}} {
		select {
		case got := <-transitions:
			if got != want {
				t.Fatalf("transition = %+v, want %+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for transition %+v", want)
		}
	}
}

func TestAdaptiveConcurrencyGateParksUntilBelowCapAndCancels(t *testing.T) {
	g := newAdaptiveConcurrencyGate(3, 15*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 3; i++ {
		if !g.acquire(ctx) {
			t.Fatal("initial acquire failed")
		}
	}
	now := time.Unix(300, 0)
	g.throttle(now, now.Add(time.Second)) // 3 -> 2 while three permits are admitted.

	acquired := make(chan bool, 1)
	go func() { acquired <- g.acquire(ctx) }()
	waitForParkedWorkers(t, g, 1)

	g.release() // admitted falls from 3 to 2, so the parked worker must stay parked.
	select {
	case <-acquired:
		t.Fatal("worker acquired while admitted work still equaled the reduced cap")
	case <-time.After(20 * time.Millisecond):
	}

	g.release() // admitted is now below the cap.
	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("worker did not acquire after capacity became available")
		}
	case <-time.After(time.Second):
		t.Fatal("parked worker was not released")
	}

	blockedCtx, blockedCancel := context.WithCancel(context.Background())
	blocked := make(chan bool, 1)
	go func() { blocked <- g.acquire(blockedCtx) }()
	waitForParkedWorkers(t, g, 1)
	blockedCancel()
	select {
	case ok := <-blocked:
		if ok {
			t.Fatal("cancelled parked worker acquired a permit")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled parked worker did not exit")
	}

	// Release the original remaining permit and the permit acquired above.
	g.release()
	g.release()
}

func TestAdaptiveConcurrencyGateRejectsCanceledContext(t *testing.T) {
	g := newAdaptiveConcurrencyGate(1, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if g.acquire(ctx) {
		t.Fatal("acquire succeeded with canceled context")
	}
	if g.admitted != 0 {
		t.Fatalf("admitted = %d, want 0", g.admitted)
	}
}

func TestCompletionMonitorCountsParkedAndQueueIdleWorkers(t *testing.T) {
	queue := NewTaskQueue()
	g := newAdaptiveConcurrencyGate(3, 15*time.Second)
	g.parked.Store(2)
	d := &ConcurrentDownloader{concurrencyGate: g}

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		d.runCompletionMonitor(ctx, queue, 1, 3)
	}()

	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("completion monitor did not count parked workers")
	}
	select {
	case <-monitorDone:
	case <-time.After(time.Second):
		t.Fatal("completion monitor did not exit")
	}
}

func waitForParkedWorkers(t *testing.T, g *adaptiveConcurrencyGate, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if g.parkedWorkers() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("parked workers = %d, want %d", g.parkedWorkers(), want)
}
