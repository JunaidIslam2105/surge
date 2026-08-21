package types

import (
	"context"
	"time"

	"github.com/SurgeDM/Surge/internal/utils"
)

const (
	IncompleteSuffix = ".surge"

	MinChunk     = 2 * utils.MiB
	AlignSize    = 4 * utils.KiB
	WorkerBuffer = 512 * utils.KiB

	DefaultPrecheckSafetyBuffer = 500 * utils.MiB

	WorkerBatchSize     = 1 * utils.MiB
	WorkerBatchInterval = 200 * time.Millisecond

	PerDownloadMax = 32
	DialHedgeCount = 4

	DefaultMaxIdleConns          = 100
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 15 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second
	DialTimeout                  = 10 * time.Second
	KeepAliveDuration            = 30 * time.Second
	ProbeTimeout                 = 30 * time.Second

	PoolMaxIdleConns        = 512
	PoolMaxIdleConnsPerHost = 128
	PoolMaxConnsPerHost     = 512

	MaxTaskRetries = 3
	RetryBaseDelay = 200 * time.Millisecond

	HealthCheckInterval = 1 * time.Second
	SlowWorkerThreshold = 0.30
	SlowWorkerGrace     = 5 * time.Second
	StallTimeout        = 3 * time.Second
	SpeedEMAAlpha       = 0.3

	ProgressChannelBuffer = 100

	RateLimitBaseBackoff    = 1 * time.Second
	RateLimitMaxBackoff     = 30 * time.Second
	RateLimitMinBackoff     = 500 * time.Millisecond
	RateLimitJitterFraction = 0.2
	RateLimitPenaltyDecay   = 60 * time.Second
	RateLimitMaxRetries     = 6
)

// ByteLimiter abstracts byte-based throttling for downloads.
type ByteLimiter interface {
	WaitN(ctx context.Context, n int64) error
}

// RuntimeConfig carries network and downloader tuning knobs.
// Fields used by the downloader getters fall into two groups:
// zero means "use package default" for capacity-style settings such as
// connections, chunk size, buffer size, and retries; zero is preserved for
// opt-out settings where disabling a behavior is meaningful.
type RuntimeConfig struct {
	MaxConnectionsPerDownload   int
	Workers                     int
	UserAgent                   string
	ProxyURL                    string
	CustomDNS                   string
	SequentialDownload          bool
	MinChunkSize                int64
	PrecheckSafetyBuffer        int64
	GlobalRateLimitBps          int64
	DefaultDownloadRateLimitBps int64

	WorkerBufferSize      int
	MaxTaskRetries        int
	DialHedgeCount        int
	SlowWorkerThreshold   float64
	SlowWorkerGracePeriod time.Duration
	StallTimeout          time.Duration
	SpeedEmaAlpha         float64
}

const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func (r *RuntimeConfig) GetUserAgent() string {
	if r == nil || r.UserAgent == "" {
		return DefaultUserAgent
	}
	return r.UserAgent
}

func (r *RuntimeConfig) GetMaxConnectionsPerDownload() int {
	if r == nil || r.MaxConnectionsPerDownload <= 0 {
		return PerDownloadMax
	}
	return r.MaxConnectionsPerDownload
}

// GetWorkers returns the explicit worker count if set (>0), or 0 to indicate
// "use heuristic" (caller must fall back to √size calculation).
func (r *RuntimeConfig) GetWorkers() int {
	if r == nil || r.Workers <= 0 {
		return 0
	}
	return r.Workers
}

func (r *RuntimeConfig) GetMinChunkSize() int64 {
	if r == nil || r.MinChunkSize <= 0 {
		return MinChunk
	}
	return r.MinChunkSize
}

// GetPrecheckSafetyBuffer returns the configured free-space cushion for
// enqueue precheck. Zero means use the default (500 MiB).
func (r *RuntimeConfig) GetPrecheckSafetyBuffer() int64 {
	if r == nil || r.PrecheckSafetyBuffer <= 0 {
		return DefaultPrecheckSafetyBuffer
	}
	return r.PrecheckSafetyBuffer
}

func (r *RuntimeConfig) GetWorkerBufferSize() int {
	if r == nil || r.WorkerBufferSize <= 0 {
		return WorkerBuffer
	}
	return r.WorkerBufferSize
}

func (r *RuntimeConfig) GetMaxTaskRetries() int {
	if r == nil || r.MaxTaskRetries < 0 {
		return MaxTaskRetries
	}
	return r.MaxTaskRetries
}

// GetDialHedgeCount returns the extra prewarmed connection count. Zero disables prewarming.
func (r *RuntimeConfig) GetDialHedgeCount() int {
	if r == nil || r.DialHedgeCount < 0 {
		return DialHedgeCount
	}
	return r.DialHedgeCount
}

func (r *RuntimeConfig) GetSlowWorkerThreshold() float64 {
	if r == nil || r.SlowWorkerThreshold < 0 || r.SlowWorkerThreshold > 1 {
		return SlowWorkerThreshold
	}
	return r.SlowWorkerThreshold
}

func (r *RuntimeConfig) GetSlowWorkerGracePeriod() time.Duration {
	if r == nil || r.SlowWorkerGracePeriod < 0 {
		return SlowWorkerGrace
	}
	return r.SlowWorkerGracePeriod
}

func (r *RuntimeConfig) GetStallTimeout() time.Duration {
	if r == nil || r.StallTimeout < 0 {
		return StallTimeout
	}
	return r.StallTimeout
}

func (r *RuntimeConfig) GetSpeedEmaAlpha() float64 {
	if r == nil || r.SpeedEmaAlpha <= 0 || r.SpeedEmaAlpha > 1 {
		return SpeedEMAAlpha
	}
	return r.SpeedEmaAlpha
}

// DefaultRuntimeConfig returns a fully-populated runtime config for callers
// that want engine defaults rather than relying on zero-value semantics.
func DefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		MaxConnectionsPerDownload:   PerDownloadMax,
		Workers:                     0,
		UserAgent:                   DefaultUserAgent,
		ProxyURL:                    "",
		CustomDNS:                   "",
		SequentialDownload:          false,
		MinChunkSize:                MinChunk,
		PrecheckSafetyBuffer:        DefaultPrecheckSafetyBuffer,
		GlobalRateLimitBps:          0,
		DefaultDownloadRateLimitBps: 0,
		WorkerBufferSize:            WorkerBuffer,
		MaxTaskRetries:              MaxTaskRetries,
		DialHedgeCount:              DialHedgeCount,
		SlowWorkerThreshold:         SlowWorkerThreshold,
		SlowWorkerGracePeriod:       SlowWorkerGrace,
		StallTimeout:                StallTimeout,
		SpeedEmaAlpha:               SpeedEMAAlpha,
	}
}
