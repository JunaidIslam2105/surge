package transport

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_SetRateDisableWakesWaiter(t *testing.T) {
	limiter := NewRateLimiter(1, 0)
	done := make(chan error, 1)

	go func() {
		done <- limiter.WaitN(context.Background(), 10)
	}()

	select {
	case err := <-done:
		t.Fatalf("WaitN returned before rate change: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	limiter.SetRate(0, 0)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitN returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitN did not wake after disabling rate limit")
	}
}

func TestRateLimiter_NegativeRateIsDisabled(t *testing.T) {
	limiter := NewRateLimiter(-1, 100)
	if err := limiter.WaitN(context.Background(), 100); err != nil {
		t.Fatalf("negative rate should be disabled: %v", err)
	}

	limiter.SetRate(-2, 100)
	if err := limiter.WaitN(context.Background(), 100); err != nil {
		t.Fatalf("negative SetRate should disable limiter: %v", err)
	}
}

func TestRateLimiter_ReenableSeedsFreshBurst(t *testing.T) {
	limiter := NewRateLimiter(100, 100)
	if err := limiter.WaitN(context.Background(), 100); err != nil {
		t.Fatalf("initial WaitN returned error: %v", err)
	}

	limiter.SetRate(0, 0)
	limiter.SetRate(1000, 1000)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := limiter.WaitN(ctx, 1000); err != nil {
		t.Fatalf("re-enabled limiter did not provide its fresh burst: %v", err)
	}
}

func TestRateLimiter_SetRateIncreaseWakesWaiter(t *testing.T) {
	limiter := NewRateLimiter(1, 0)
	done := make(chan error, 1)

	go func() {
		done <- limiter.WaitN(context.Background(), 10)
	}()

	select {
	case err := <-done:
		t.Fatalf("WaitN returned before rate change: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	limiter.SetRate(10*1024*1024, 10*1024*1024)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitN returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitN did not wake after increasing rate limit")
	}
}

func TestRateLimiter_SetRateDecreaseWakesWaiter(t *testing.T) {
	// Start with enough rate to be useful but a request that exceeds the bucket
	// so the waiter actually blocks.
	limiter := NewRateLimiter(10000, 10000)
	done := make(chan error, 1)

	go func() {
		done <- limiter.WaitN(context.Background(), 20000)
	}()

	select {
	case err := <-done:
		t.Fatalf("WaitN returned before rate change: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	// Decrease the rate: the waiter has 10000 tokens and needs 20000.
	// SetRate wakes waiters so it re-checks. With the much slower rate it still
	// won't have enough tokens and must remain blocked.
	limiter.SetRate(100, 100)

	select {
	case err := <-done:
		t.Fatalf("WaitN returned unexpectedly after rate decrease: %v", err)
	case <-time.After(200 * time.Millisecond):
		// waiter is still blocked - expected since it still lacks tokens
	}

	// Disable to unblock and avoid goroutine leak
	limiter.SetRate(0, 0)
	select {
	case err := <-done:
		if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
			t.Fatalf("WaitN returned error after disable: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("WaitN did not return after disabling rate limit")
	}
}
