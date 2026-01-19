package limiter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_Handle429_WithRetryAfterSeconds(t *testing.T) {
	rl := NewRateLimiter("test.com")

	resp := &http.Response{
		Header: http.Header{"Retry-After": []string{"5"}},
	}

	waitDuration := rl.Handle429(resp)

	// With jitter, should be approximately 5s (±10%)
	if waitDuration < 4*time.Second || waitDuration > 6*time.Second {
		t.Errorf("expected ~5s wait, got %v", waitDuration)
	}

	if !rl.IsBlocked() {
		t.Error("expected to be blocked after 429")
	}
}

func TestRateLimiter_Handle429_WithRetryAfterDate(t *testing.T) {
	rl := NewRateLimiter("test.com")

	// HTTP-date uses UTC
	futureTime := time.Now().UTC().Add(3 * time.Second)
	resp := &http.Response{
		Header: http.Header{"Retry-After": []string{futureTime.Format(http.TimeFormat)}},
	}

	waitDuration := rl.Handle429(resp)

	// With jitter, should be approximately 3 seconds (±10%)
	if waitDuration < 2*time.Second || waitDuration > 4*time.Second {
		t.Errorf("expected ~3s wait, got %v", waitDuration)
	}
}

func TestRateLimiter_Handle429_ExponentialBackoff(t *testing.T) {
	rl := NewRateLimiter("test.com")
	resp := &http.Response{Header: http.Header{}}

	// First 429: should be ~1s (with jitter)
	wait1 := rl.Handle429(resp)
	if wait1 < 800*time.Millisecond || wait1 > 1200*time.Millisecond {
		t.Errorf("first 429: expected ~1s (±20%% with jitter), got %v", wait1)
	}

	// Second 429: should be ~2s (with jitter)
	wait2 := rl.Handle429(resp)
	if wait2 < 1600*time.Millisecond || wait2 > 2400*time.Millisecond {
		t.Errorf("second 429: expected ~2s (±20%% with jitter), got %v", wait2)
	}
}

func TestRateLimiter_ReportSuccess_ResetsCounter(t *testing.T) {
	rl := NewRateLimiter("test.com")
	resp := &http.Response{Header: http.Header{}}

	// Build up counter
	rl.Handle429(resp)
	rl.Handle429(resp)

	// Report success
	rl.ReportSuccess()

	// Next 429 should start fresh at ~1s
	wait := rl.Handle429(resp)
	if wait < 800*time.Millisecond || wait > 1200*time.Millisecond {
		t.Errorf("after success: expected ~1s, got %v", wait)
	}
}

func TestRateLimiter_WaitIfBlocked_NotBlocked(t *testing.T) {
	rl := NewRateLimiter("test.com")

	start := time.Now()
	waited := rl.WaitIfBlocked()
	elapsed := time.Since(start)

	if waited {
		t.Error("expected not to wait when not blocked")
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("should return immediately, took %v", elapsed)
	}
}

func TestRateLimitError_Error(t *testing.T) {
	err := &RateLimitError{WaitDuration: 5 * time.Second}
	expected := "rate limited (429), retry after 5s"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGlobalLimitManager_SameHost(t *testing.T) {
	Reset() // Clean slate

	limiter1 := GetLimiter("example.com")
	limiter2 := GetLimiter("example.com")

	if limiter1 != limiter2 {
		t.Error("expected same limiter instance for same host")
	}

	if ActiveHosts() != 1 {
		t.Errorf("expected 1 active host, got %d", ActiveHosts())
	}
}

func TestGlobalLimitManager_DifferentHosts(t *testing.T) {
	Reset() // Clean slate

	limiter1 := GetLimiter("example.com")
	limiter2 := GetLimiter("other.com")

	if limiter1 == limiter2 {
		t.Error("expected different limiter instances for different hosts")
	}

	if ActiveHosts() != 2 {
		t.Errorf("expected 2 active hosts, got %d", ActiveHosts())
	}
}

func TestGlobalLimitManager_SharedState(t *testing.T) {
	Reset() // Clean slate

	// Simulate two downloads from same host
	limiter1 := GetLimiter("shared.com")
	limiter2 := GetLimiter("shared.com")

	// First download triggers 429
	resp := &http.Response{
		Header: http.Header{"Retry-After": []string{"2"}},
	}
	limiter1.Handle429(resp)

	// Second download should see the block
	if !limiter2.IsBlocked() {
		t.Error("second limiter should be blocked due to shared state")
	}
}

func TestAddJitter(t *testing.T) {
	// Test that jitter adds variation
	base := 10 * time.Second

	// Run multiple times to verify variation
	seen := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		result := addJitter(base, 0.10)
		seen[result] = true

		// Should be within ±10%
		if result < 9*time.Second || result > 11*time.Second {
			t.Errorf("jitter out of range: got %v, expected 9s-11s", result)
		}
	}

	// Should have some variation (not all same value)
	if len(seen) < 2 {
		t.Error("expected jitter to produce varying results")
	}
}

func TestRateLimiter_Integration(t *testing.T) {
	Reset()

	// Create test server that returns 429
	retryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		if retryCount <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	rl := GetLimiter("127.0.0.1")
	client := server.Client()

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		waitDuration := rl.Handle429(resp)
		// Should be ~1s with jitter
		if waitDuration < 800*time.Millisecond || waitDuration > 1200*time.Millisecond {
			t.Errorf("expected ~1s wait, got %v", waitDuration)
		}
	}
}
