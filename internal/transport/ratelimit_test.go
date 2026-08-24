package transport

import (
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestParseRetryAfter_Seconds(t *testing.T) {
	now := time.Now()
	d, ok := ParseRetryAfter("120", now)
	if !ok {
		t.Fatal("expected ok=true for seconds form")
	}
	if d != 120*time.Second {
		t.Fatalf("expected 120s, got %v", d)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	now := time.Now()
	future := now.Add(5*time.Second).UTC().Format("Mon, 02 Jan 2006 15:04:05") + " GMT"
	d, ok := ParseRetryAfter(future, now)
	if !ok {
		t.Fatalf("expected ok=true for HTTP-date form: %q", future)
	}
	if d < 4*time.Second || d > 6*time.Second {
		t.Fatalf("expected ~5s, got %v", d)
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	_, ok := ParseRetryAfter("", time.Now())
	if ok {
		t.Fatal("expected ok=false for empty header")
	}
}

func TestParseRetryAfter_Garbage(t *testing.T) {
	_, ok := ParseRetryAfter("not-valid", time.Now())
	if ok {
		t.Fatal("expected ok=false for garbage")
	}
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	now := time.Now()
	past := now.Add(-10*time.Second).UTC().Format("Mon, 02 Jan 2006 15:04:05") + " GMT"
	d, ok := ParseRetryAfter(past, now)
	if !ok {
		t.Fatalf("expected ok=true for past HTTP-date: %q", past)
	}
	if d >= 0 {
		t.Fatalf("expected negative duration for past date, got %v", d)
	}
}

func TestHostRateLimiter_PenalizeExpBackoff(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	penalize := func(host string) time.Duration {
		deadline := h.Penalize(host, 0, false, now)
		return deadline.Sub(now)
	}

	d1 := penalize("a.example.com")
	d2 := penalize("a.example.com")
	d3 := penalize("a.example.com")

	if d2 < d1 {
		t.Fatalf("expected backoff to grow: d1=%v d2=%v", d1, d2)
	}
	if d3 < d2 {
		t.Fatalf("expected backoff to keep growing: d2=%v d3=%v", d2, d3)
	}
}

func TestHostRateLimiter_PenalizeRetryAfterClamp(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	deadline := h.Penalize("example.com", 3600*time.Second, true, now)
	backoff := deadline.Sub(now)

	if backoff > types.RateLimitMaxBackoff+time.Second {
		t.Fatalf("expected backoff clamped to max %v, got %v", types.RateLimitMaxBackoff, backoff)
	}
}

func TestHostRateLimiter_PenalizeMinClamp(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	deadline := h.Penalize("example.com", 0, true, now)
	backoff := deadline.Sub(now)

	if backoff < types.RateLimitMinBackoff {
		t.Fatalf("expected backoff at least %v, got %v", types.RateLimitMinBackoff, backoff)
	}
}

func TestHostRateLimiter_BlockedUntil(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	if bu := h.BlockedUntil("unknown.example.com", now); !bu.IsZero() {
		t.Fatal("expected zero time for unknown host")
	}

	h.Penalize("example.com", 5*time.Second, true, now)
	bu := h.BlockedUntil("example.com", now)
	if bu.IsZero() {
		t.Fatal("expected non-zero blocked until")
	}
	if !now.Before(bu) {
		t.Fatalf("blocked until %v should be after now %v", bu, now)
	}

	free := h.BlockedUntil("example.com", now.Add(6*time.Second))
	if !free.IsZero() {
		t.Fatal("expected free after penalty expires")
	}
}

func TestHostRateLimiter_RecordSuccess(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	h.Penalize("example.com", 1*time.Second, true, now)
	h.recordSuccess("example.com", now.Add(100*time.Millisecond))

	bu := h.BlockedUntil("example.com", now.Add(100*time.Millisecond))
	if bu.IsZero() {
		t.Fatal("expected overlapping success to preserve the future cooldown")
	}

	h.recordSuccess("example.com", now.Add(2*time.Second))
	h.mu.Lock()
	_, retained := h.hosts["example.com"]
	h.mu.Unlock()
	if retained {
		t.Fatal("expected success after cooldown to remove the host penalty")
	}
}

func TestHostRateLimiter_LaterPenaltyCannotShortenCooldown(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	first := h.Penalize("example.com", 10*time.Second, true, now)
	second := h.Penalize("example.com", time.Second, true, now.Add(time.Second))
	if second.Before(first) {
		t.Fatalf("later penalty shortened deadline from %v to %v", first, second)
	}

	h.recordSuccess("example.com", now.Add(2*time.Second))
	if got := h.BlockedUntil("example.com", now.Add(2*time.Second)); got.Before(first) {
		t.Fatalf("overlapping success shortened deadline from %v to %v", first, got)
	}
}

func TestHostRateLimiter_PickMirror_FreeChosen(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	hosts := []string{"a.example.com", "b.example.com"}
	h.Penalize("b.example.com", 10*time.Second, true, now)

	idx, wait := h.PickMirror(hosts, 1, now)
	if idx != 0 {
		t.Fatalf("expected free mirror a (idx 0), got %d", idx)
	}
	if wait != 0 {
		t.Fatalf("expected no wait, got %v", wait)
	}
}

func TestHostRateLimiter_PickMirror_AllPenalized(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	hosts := []string{"a.example.com", "b.example.com"}
	h.Penalize("a.example.com", 10*time.Second, true, now)
	h.Penalize("b.example.com", 5*time.Second, true, now)

	idx, wait := h.PickMirror(hosts, 0, now)
	if wait <= 0 {
		t.Fatal("expected positive wait when all penalized")
	}
	if idx != 1 {
		t.Fatalf("expected soonest mirror b (idx 1), got %d", idx)
	}
}

func TestHostRateLimiter_PickMirror_StartIdxRotation(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	hosts := []string{"a.example.com", "b.example.com", "c.example.com"}

	idx, wait := h.PickMirror(hosts, 1, now)
	if idx != 1 {
		t.Fatalf("expected to start at index 1, got %d", idx)
	}
	if wait != 0 {
		t.Fatalf("expected no wait, got %v", wait)
	}
}

func TestHostRateLimiter_PenaltyDecay(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	h.Penalize("example.com", 1*time.Second, true, now)

	h.Penalize("example.com", 1*time.Second, true, now.Add(types.RateLimitPenaltyDecay+time.Second))

	bu := h.BlockedUntil("example.com", now.Add(types.RateLimitPenaltyDecay+time.Second))
	if bu.IsZero() {
		t.Fatal("expected host to still be penalized after decay")
	}
}

func TestMirrorHost(t *testing.T) {
	h := MirrorHost("https://cdn.example.com:443/path/file.bin")
	if h != "cdn.example.com:443" {
		t.Fatalf("expected cdn.example.com:443, got %s", h)
	}
}

func TestMirrorHost_ParseError(t *testing.T) {
	raw := "://invalid"
	h := MirrorHost(raw)
	if h != raw {
		t.Fatalf("expected fallback to raw URL on parse error, got %s", h)
	}
}

func TestHostRateLimiter_PenalizeNegativeRetryAfter(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	deadline := h.Penalize("example.com", -10*time.Second, true, now)
	backoff := deadline.Sub(now)

	if backoff < types.RateLimitMinBackoff {
		t.Fatalf("expected backoff at least %v for negative Retry-After, got %v", types.RateLimitMinBackoff, backoff)
	}
	if backoff > types.RateLimitMinBackoff+time.Second {
		t.Fatalf("expected backoff near min %v for negative Retry-After, got %v", types.RateLimitMinBackoff, backoff)
	}
}

func TestHostRateLimiter_CleanupRemovesExpired(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	h.Penalize("old.example.com", 1*time.Second, true, now)

	h.Penalize("new.example.com", 10*time.Second, true, now.Add(types.RateLimitPenaltyDecay+2*time.Second))

	bu := h.BlockedUntil("old.example.com", now.Add(types.RateLimitPenaltyDecay+3*time.Second))
	if !bu.IsZero() {
		t.Fatal("expected old host to be cleaned up after decay window + expiry")
	}

	bu2 := h.BlockedUntil("new.example.com", now.Add(types.RateLimitPenaltyDecay+3*time.Second))
	if bu2.IsZero() {
		t.Fatal("expected new host to still exist")
	}
}

func TestHostRateLimiter_PenaltyDecayResetsConsecutive(t *testing.T) {
	h := NewHostRateLimiter()
	now := time.Now()

	penalizeAt := func(t time.Time) time.Duration {
		deadline := h.Penalize("example.com", 0, false, t)
		return deadline.Sub(t)
	}

	d1 := penalizeAt(now)
	d2 := penalizeAt(now)

	d3 := penalizeAt(now.Add(types.RateLimitPenaltyDecay + time.Second))

	if d3 >= d2 {
		t.Fatalf("expected decay-reset backoff (d3=%v) to be less than exponential (d2=%v)", d3, d2)
	}
	_ = d1
}
