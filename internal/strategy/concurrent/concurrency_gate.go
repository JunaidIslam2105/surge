package concurrent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

// adaptiveConcurrencyGate keeps worker goroutines alive while limiting how many
// may enter the task queue at once.
//
// ponytail: This cap is deliberately per-download. Promote it to host-wide
// coordination only if benchmarks show multiple same-host downloads fighting.
type adaptiveConcurrencyGate struct {
	mu sync.Mutex

	max            int
	cap            int
	admitted       int
	enabled        bool
	recoveryWindow time.Duration
	parked         atomic.Int64
	changed        chan struct{}
	policyChanged  chan struct{}
	throttled      bool

	cooldownUntil time.Time
	lastThrottle  time.Time
	nextIncrease  time.Time
}

func newAdaptiveConcurrencyGate(workers int, enabled bool, recoveryWindow time.Duration) *adaptiveConcurrencyGate {
	if workers < 1 {
		workers = 1
	}
	if recoveryWindow <= 0 {
		recoveryWindow = types.DefaultAdaptiveConcurrencyRecoveryWindow
	}
	return &adaptiveConcurrencyGate{
		max:            workers,
		cap:            workers,
		enabled:        enabled,
		recoveryWindow: recoveryWindow,
		changed:        make(chan struct{}),
		policyChanged:  make(chan struct{}),
	}
}

func (g *adaptiveConcurrencyGate) acquire(ctx context.Context) bool {
	for {
		g.mu.Lock()
		if g.admitted < g.cap {
			g.admitted++
			g.mu.Unlock()
			return true
		}
		changed := g.changed
		g.parked.Add(1)
		g.mu.Unlock()

		select {
		case <-ctx.Done():
			g.parked.Add(-1)
			return false
		case <-changed:
			g.parked.Add(-1)
		}
	}
}

func (g *adaptiveConcurrencyGate) release() {
	g.mu.Lock()
	if g.admitted > 0 {
		g.admitted--
	}
	g.notifyLocked()
	g.mu.Unlock()
}

// throttle records one server response. Responses that arrive before the
// current cooldown expires extend the episode but do not repeatedly halve the
// cap. It returns the old and new cap and whether this was a new episode.
func (g *adaptiveConcurrencyGate) throttle(now, cooldownUntil time.Time) (int, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	oldCap := g.cap
	newEpisode := g.cooldownUntil.IsZero() || !now.Before(g.cooldownUntil)
	if newEpisode && g.enabled {
		g.cap = (g.cap + 1) / 2
		if g.cap < 1 {
			g.cap = 1
		}
	}

	g.throttled = true
	g.lastThrottle = now
	if cooldownUntil.After(g.cooldownUntil) {
		g.cooldownUntil = cooldownUntil
	}
	g.nextIncrease = g.lastThrottle.Add(g.recoveryWindow)
	if g.cooldownUntil.After(g.nextIncrease) {
		g.nextIncrease = g.cooldownUntil
	}
	if g.cap != oldCap {
		g.notifyLocked()
	}
	g.notifyPolicyLocked()
	return oldCap, g.cap, newEpisode
}

// recover raises the cap at most once per healthy window. The final return
// value reports that the complete throttle/recovery episode has ended.
func (g *adaptiveConcurrencyGate) recover(now time.Time) (int, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	oldCap := g.cap
	if !g.throttled || now.Before(g.nextIncrease) {
		return oldCap, oldCap, false
	}

	if g.enabled && g.cap < g.max {
		g.cap++
		g.nextIncrease = now.Add(g.recoveryWindow)
		g.notifyLocked()
	}
	if !g.enabled || g.cap == g.max {
		g.throttled = false
		g.cooldownUntil = time.Time{}
		g.lastThrottle = time.Time{}
		g.nextIncrease = time.Time{}
		return oldCap, g.cap, true
	}
	return oldCap, g.cap, false
}

func (g *adaptiveConcurrencyGate) parkedWorkers() int64 {
	return g.parked.Load()
}

func (g *adaptiveConcurrencyGate) runRecovery(ctx context.Context, onTransition func(oldCap, newCap int, recovered bool)) {
	for {
		g.mu.Lock()
		policyChanged := g.policyChanged
		deadline := g.nextIncrease
		throttled := g.throttled
		g.mu.Unlock()

		if !throttled || deadline.IsZero() {
			select {
			case <-ctx.Done():
				return
			case <-policyChanged:
				continue
			}
		}

		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-policyChanged:
			if !timer.Stop() {
				<-timer.C
			}
			continue
		case now := <-timer.C:
			oldCap, newCap, recovered := g.recover(now)
			if newCap > oldCap || recovered {
				onTransition(oldCap, newCap, recovered)
			}
		}
	}
}

func (g *adaptiveConcurrencyGate) currentCap() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cap
}

func (g *adaptiveConcurrencyGate) notifyLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}

func (g *adaptiveConcurrencyGate) notifyPolicyLocked() {
	close(g.policyChanged)
	g.policyChanged = make(chan struct{})
}
