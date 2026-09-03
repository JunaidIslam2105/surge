package orchestrator

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestEventBus_BasicPubSub(t *testing.T) {
	eb := NewEventBus()
	defer eb.Shutdown()

	sub, cleanup := eb.Subscribe()
	defer cleanup()

	msg := types.DownloadEvent{Message: "test message"}
	err := eb.Publish(msg)
	if err != nil {
		t.Fatalf("expected nil error on publish, got %v", err)
	}

	select {
	case received := <-sub:
		if received.Message != msg.Message {
			t.Errorf("expected %v, got %v", msg, received)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	eb := NewEventBus()
	defer eb.Shutdown()

	sub1, cleanup1 := eb.Subscribe()
	defer cleanup1()

	sub2, cleanup2 := eb.Subscribe()
	defer cleanup2()

	msg := types.DownloadEvent{Message: "broadcast"}
	_ = eb.Publish(msg)

	for i, sub := range []<-chan types.DownloadEvent{sub1, sub2} {
		select {
		case received := <-sub:
			if received.Message != msg.Message {
				t.Errorf("subscriber %d expected %v, got %v", i+1, msg, received)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("subscriber %d timed out", i+1)
		}
	}
}

func TestEventBus_ProgressMsgDropBehavior(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eb := &EventBus{listeners: []chan types.DownloadEvent{make(chan types.DownloadEvent, 1)}}
		eb.listeners[0] <- types.DownloadEvent{}

		start := time.Now()
		eb.broadcastMsg(types.DownloadEvent{Type: types.EventProgress, DownloadID: "test"})
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("progress event wait = %v, want 0", elapsed)
		}
	})
}

func TestEventBus_CriticalMsgWaitBehavior(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eb := &EventBus{listeners: []chan types.DownloadEvent{make(chan types.DownloadEvent, 1)}}
		eb.listeners[0] <- types.DownloadEvent{}

		start := time.Now()
		eb.broadcastMsg(types.DownloadEvent{Message: "critical"})
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("critical event wait = %v, want 1s", elapsed)
		}
	})
}

func TestEventBus_ShutdownCleanly(t *testing.T) {
	eb := NewEventBus()

	sub, cleanup := eb.Subscribe()
	defer cleanup()

	eb.Shutdown()

	// Should not be able to publish after shutdown
	err := eb.Publish(types.DownloadEvent{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled on publish after shutdown, got %v", err)
	}

	// Channels should be closed
	select {
	case _, ok := <-sub:
		if ok {
			t.Error("expected channel to be closed after shutdown")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for subscriber channel to close")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus()
	defer eb.Shutdown()

	_, cleanup := eb.Subscribe()

	eb.listenerMu.Lock()
	count := len(eb.listeners)
	eb.listenerMu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 listener, got %d", count)
	}

	cleanup()

	// Wait for the asynchronous unsubscribe to be processed by broadcastLoop
	for i := 0; i < 10; i++ {
		eb.listenerMu.Lock()
		count = len(eb.listeners)
		eb.listenerMu.Unlock()
		if count == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if count != 0 {
		t.Fatalf("expected 0 listeners after cleanup, got %d", count)
	}

	// Should be safe to call cleanup multiple times (sync.Once)
	cleanup()
}
