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
	// RangeMu serializes writes with work-steal boundary changes.
	// ponytail: When both are needed, acquire ConcurrentDownloader.activeMu
	// before RangeMu; never acquire them in the reverse order.
	RangeMu sync.Mutex

	// Health monitoring fields
	LastActivity atomic.Int64       // Unix nano timestamp of last data received
	Speed        float64            // EMA-smoothed speed in bytes/sec (protected by mutex)
	StartTime    time.Time          // When this task started
	Cancel       context.CancelFunc // Cancel function to abort this task
	SpeedMu      sync.Mutex         // Protects Speed field

	// Sliding window for recent speed tracking
	WindowStart time.Time    // When current measurement window started
	WindowBytes atomic.Int64 // Bytes downloaded in current window

	// Set while blocked on rate limiter so health monitor doesn't treat it as stalled
	WaitingOnLimiter atomic.Bool
}

// RemainingTask returns a Task representing the remaining work, or nil if complete
func (at *ActiveTask) RemainingTask() *types.Task {
	at.RangeMu.Lock()
	defer at.RangeMu.Unlock()
	current := at.CurrentOffset.Load()
	stopAt := at.StopAt.Load()
	if current >= stopAt {
		return nil
	}

	return &types.Task{
		Offset: current,
		Length: stopAt - current,
	}
}

// RemainingBytes returns the number of bytes left in the current task
func (at *ActiveTask) RemainingBytes() int64 {
	at.RangeMu.Lock()
	defer at.RangeMu.Unlock()
	current := at.CurrentOffset.Load()
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

// alignedSplitSize calculates a split size that is half of remaining, aligned to AlignSize.
func alignedSplitSize(remaining, minChunk int64) int64 {
	half := (remaining / 2 / types.AlignSize) * types.AlignSize
	if half < minChunk {
		return 0
	}
	return half
}
