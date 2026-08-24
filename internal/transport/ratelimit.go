package transport

import (
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

var DefaultHostRateLimiter = NewHostRateLimiter()

type hostPenalty struct {
	until       time.Time
	consecutive int
	lastHit     time.Time
}

type HostRateLimiter struct {
	mu    sync.Mutex
	hosts map[string]*hostPenalty
}

func NewHostRateLimiter() *HostRateLimiter {
	return &HostRateLimiter{
		hosts: make(map[string]*hostPenalty),
	}
}

func (h *HostRateLimiter) Penalize(host string, retryAfter time.Duration, explicit bool, now time.Time) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()

	p, ok := h.hosts[host]
	if !ok {
		p = &hostPenalty{}
		h.hosts[host] = p
	}

	if now.Sub(p.lastHit) > types.RateLimitPenaltyDecay {
		p.consecutive = 0
	}
	p.consecutive++
	p.lastHit = now

	var d time.Duration
	if explicit {
		d = retryAfter
	} else {
		d = types.RateLimitBaseBackoff * time.Duration(int64(1)<<(p.consecutive-1))
	}

	if d < types.RateLimitMinBackoff {
		d = types.RateLimitMinBackoff
	}
	if d > types.RateLimitMaxBackoff {
		d = types.RateLimitMaxBackoff
	}

	jitterRange := int64(float64(d) * types.RateLimitJitterFraction)
	if jitterRange > 0 {
		delta := rand.Int64N(2*jitterRange) - jitterRange
		d += time.Duration(delta)
	}
	if d < types.RateLimitMinBackoff {
		d = types.RateLimitMinBackoff
	}
	if d > types.RateLimitMaxBackoff {
		d = types.RateLimitMaxBackoff
	}

	deadline := now.Add(d)
	if deadline.After(p.until) {
		p.until = deadline
	}

	h.cleanupLocked()
	return p.until
}

func (h *HostRateLimiter) BlockedUntil(host string, now time.Time) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()

	p, ok := h.hosts[host]
	if !ok {
		return time.Time{}
	}
	if now.Before(p.until) {
		return p.until
	}
	return time.Time{}
}

func (h *HostRateLimiter) RecordSuccess(host string) {
	h.recordSuccess(host, time.Now())
}

func (h *HostRateLimiter) recordSuccess(host string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	p, ok := h.hosts[host]
	if !ok || !now.Before(p.until) {
		delete(h.hosts, host)
	}
}

func (h *HostRateLimiter) PickMirror(hosts []string, startIdx int, now time.Time) (int, time.Duration) {
	if len(hosts) == 0 {
		return 0, 0
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	firstFree := -1
	earliestIdx := -1
	var earliestDeadline time.Time

	n := len(hosts)
	for i := 0; i < n; i++ {
		idx := (startIdx + i) % n
		host := hosts[idx]
		p, ok := h.hosts[host]
		if !ok || now.After(p.until) {
			firstFree = idx
			break
		}
		if earliestIdx == -1 || p.until.Before(earliestDeadline) {
			earliestIdx = idx
			earliestDeadline = p.until
		}
	}

	if firstFree >= 0 {
		return firstFree, 0
	}

	wait := earliestDeadline.Sub(now)
	if wait < 0 {
		wait = 0
	}
	return earliestIdx, wait
}

func (h *HostRateLimiter) cleanupLocked() {
	now := time.Now()
	for host, p := range h.hosts {
		if now.After(p.until) && now.Sub(p.lastHit) > types.RateLimitPenaltyDecay {
			delete(h.hosts, host)
		}
	}
}

func ParseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	if header == "" {
		return 0, false
	}

	if n, err := strconv.Atoi(header); err == nil {
		return time.Duration(n) * time.Second, true
	}

	t, err := http.ParseTime(header)
	if err != nil {
		return 0, false
	}
	d := t.Sub(now)
	return d, true
}

func MirrorHost(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return rawurl
	}
	return u.Host
}
