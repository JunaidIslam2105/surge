package concurrent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
)

// ActiveTask tracks a task currently being processed by a worker
type ActiveTask struct {
	Task          types.Task
	CurrentOffset atomic.Int64
	StopAt        atomic.Int64

	// Health monitoring fields
	LastActivity atomic.Int64       // Unix nano timestamp of last data received
	Speed        float64            // EMA-smoothed speed in bytes/sec (protected by mutex)
	StartTime    time.Time          // When this task started
	Cancel       context.CancelFunc // Cancel function to abort this task
	SpeedMu      sync.Mutex         // Protects Speed field

	// Sliding window for recent speed tracking
	WindowStart time.Time    // When current measurement window started
	WindowBytes atomic.Int64 // Bytes downloaded in current window

	// Hedged request tracking
	Hedged atomic.Int32 // 1 if an idle worker is already racing this task
	// SharedMaxOffset points to the highest offset reached by any racing worker.
	// Protected by SharedMaxOffsetMu for safe concurrent initialization.
	SharedMaxOffsetMu sync.RWMutex
	SharedMaxOffset   *atomic.Int64
	// Set while blocked on rate limiter so health monitor doesn't treat it as stalled
	WaitingOnLimiter atomic.Bool
}

// currentOffset returns the effective offset, using the shared max if it is larger
func (at *ActiveTask) currentOffset(sharedMax *atomic.Int64) int64 {
	current := at.CurrentOffset.Load()
	if sharedMax != nil {
		if shared := sharedMax.Load(); shared > current {
			current = shared
		}
	}
	return current
}

// RemainingTask returns a Task representing the remaining work, or nil if complete
func (at *ActiveTask) RemainingTask() *types.Task {
	at.SharedMaxOffsetMu.RLock()
	sharedMax := at.SharedMaxOffset
	at.SharedMaxOffsetMu.RUnlock()

	current := at.currentOffset(sharedMax)
	stopAt := at.StopAt.Load()
	if current >= stopAt {
		return nil
	}

	return &types.Task{
		Offset:          current,
		Length:          stopAt - current,
		SharedMaxOffset: sharedMax,
	}
}

// RemainingBytes returns the number of bytes left in the current task
func (at *ActiveTask) RemainingBytes() int64 {
	at.SharedMaxOffsetMu.RLock()
	sharedMax := at.SharedMaxOffset
	at.SharedMaxOffsetMu.RUnlock()

	current := at.currentOffset(sharedMax)
	stopAt := at.StopAt.Load()
	if current >= stopAt {
		return 0
	}
	return stopAt - current
}

// GetSpeed returns the current EMA-smoothed speed, decaying if stalled
func (at *ActiveTask) GetSpeed() float64 {
	at.SpeedMu.Lock()
	speed := at.Speed
	at.SpeedMu.Unlock()

	// Check for stall
	lastActivity := at.LastActivity.Load()
	if lastActivity == 0 {
		return speed
	}

	since := time.Since(time.Unix(0, lastActivity))
	const decayThreshold = 2 * time.Second

	// If we haven't heard from the worker in > 2s, decay the speed
	// effectively: Speed = Speed * (Threshold / TimeSinceLastActivity)
	if since > decayThreshold {
		decayFactor := float64(decayThreshold) / float64(since)
		speed *= decayFactor
	}

	return speed
}

// alignedSplitSize calculates a split size that is half of remaining, aligned to AlignSize
// Returns 0 if the split would be smaller than MinChunk
func alignedSplitSize(remaining int64) int64 {
	half := (remaining / 2 / types.AlignSize) * types.AlignSize
	if half < types.MinChunk {
		return 0
	}
	return half
}
