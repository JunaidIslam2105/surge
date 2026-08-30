package transport

import (
	"context"
	"math/bits"
	"sync"
	"time"
)

const maxInt64 = int64(^uint64(0) >> 1)

// RateLimiter is a custom token-bucket rate limiter.
// We use a custom implementation instead of golang.org/x/time/rate because
// x/time/rate's SetLimit does not wake up already-blocked WaitN calls. For a
// download manager with interactive UX, we need WaitN to instantly react to
// a rate increase (via wakeCh) so the user doesn't experience "stuck"
// downloads after lifting a heavy throttle.
type RateLimiter struct {
	rate       int64
	tokens     int64
	bucketSize int64
	lastRefill time.Time
	mu         sync.Mutex
	wakeCh     chan struct{}
}

func NewRateLimiter(rate int64, bucketSize int64) *RateLimiter {
	if bucketSize < 0 {
		bucketSize = 0
	}
	return &RateLimiter{
		rate:       rate,
		bucketSize: bucketSize,
		tokens:     bucketSize,
		lastRefill: time.Now(),
		wakeCh:     make(chan struct{}),
	}
}

func (r *RateLimiter) WaitN(ctx context.Context, n int64) error {
	if n <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	firstLoop := true
	for {
		r.mu.Lock()

		if r.rate <= 0 {
			r.mu.Unlock()
			return nil
		}

		bucketCap := r.bucketSize
		// Note: Expanding the bucketCap to 'n' on subsequent iterations couples
		// the effective burst size to the caller's read buffer. For a single-file
		// downloader reading 32 KB at a time with a 10 KB/s limit, tokens can build
		// up to 32 KB during a slow disk write, producing a 3.2x instantaneous burst
		// on the next loop. This burst-then-pause pattern is an accepted design
		// trade-off to avoid penalising bursty readers while bounding average throughput.
		if !firstLoop && bucketCap < n {
			bucketCap = n
		}

		now := time.Now()

		if r.lastRefill.IsZero() {
			r.lastRefill = now
		} else {
			elapsed := now.Sub(r.lastRefill)
			if elapsed > 0 {
				hi, lo := bits.Mul64(uint64(elapsed.Nanoseconds()), uint64(r.rate))
				if hi >= uint64(time.Second) {
					r.tokens = bucketCap
					r.lastRefill = now
				} else {
					add, _ := bits.Div64(hi, lo, uint64(time.Second))
					if add > 0 {
						if add > uint64(maxInt64) {
							r.tokens = maxInt64
						} else {
							r.tokens += int64(add)
						}
						if r.tokens > bucketCap {
							r.tokens = bucketCap
						}
						r.lastRefill = now
					}
				}
			}
		}

		if r.tokens > bucketCap {
			r.tokens = bucketCap
		}

		if r.tokens >= n {
			r.tokens -= n
			r.mu.Unlock()
			return nil
		}

		firstLoop = false
		missing := n - r.tokens
		wakeCh := r.wakeCh

		hi, lo := bits.Mul64(uint64(missing), uint64(time.Second))
		waitNs, rem := bits.Div64(hi, lo, uint64(r.rate))
		if rem > 0 {
			waitNs++
		}

		r.mu.Unlock()

		if waitNs == 0 {
			continue
		}
		if waitNs > uint64(maxInt64) {
			waitNs = uint64(maxInt64)
		}

		timer := time.NewTimer(time.Duration(int64(waitNs)))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (r *RateLimiter) SetRate(rate int64, bucketSize int64) {
	if bucketSize < 0 {
		bucketSize = 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// settle refill using the old rate before changing config
	if !r.lastRefill.IsZero() && r.rate > 0 {
		elapsed := now.Sub(r.lastRefill)
		if elapsed > 0 {
			hi, lo := bits.Mul64(uint64(elapsed.Nanoseconds()), uint64(r.rate))
			if hi >= uint64(time.Second) {
				r.tokens = r.bucketSize
			} else {
				add, _ := bits.Div64(hi, lo, uint64(time.Second))
				if add > 0 {
					if add > uint64(maxInt64) {
						r.tokens = maxInt64
					} else {
						r.tokens += int64(add)
					}
				}
			}
		}
	}

	oldRate := r.rate
	r.rate = rate
	r.bucketSize = bucketSize

	if rate <= 0 {
		r.tokens = 0
	} else if oldRate <= 0 {
		// Disabling a limiter intentionally drains its bucket. Re-seed the
		// bucket when it is enabled again; otherwise the first read after
		// removing a throttle waits for a full refill despite having a fresh
		// burst budget.
		r.tokens = bucketSize
	} else if r.tokens > bucketSize {
		r.tokens = bucketSize
	}
	r.lastRefill = now
	r.wakeWaitersLocked()
}

func (r *RateLimiter) wakeWaitersLocked() {
	if r.wakeCh == nil {
		r.wakeCh = make(chan struct{})
		return
	}
	close(r.wakeCh)
	r.wakeCh = make(chan struct{})
}

func (r *RateLimiter) Refund(n int64) {
	if n <= 0 {
		return
	}
	r.mu.Lock()
	// If the limiter is unlimited (rate == 0), WaitN never deducted tokens,
	// so there is nothing to refund and no waiters to wake.
	if r.rate <= 0 {
		r.mu.Unlock()
		return
	}
	r.tokens += n
	if r.tokens > r.bucketSize {
		r.tokens = r.bucketSize
	}
	r.wakeWaitersLocked()
	r.mu.Unlock()
}
