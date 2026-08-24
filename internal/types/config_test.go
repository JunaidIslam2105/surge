package types

import (
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/utils"
)

func TestRuntimeConfig_Getters(t *testing.T) {
	t.Run("nil config returns defaults", func(t *testing.T) {
		var r *RuntimeConfig = nil

		if got := r.GetUserAgent(); got == "" {
			t.Error("GetUserAgent should return default, got empty")
		}
		if got := r.GetMaxConnectionsPerDownload(); got != PerDownloadMax {
			t.Errorf("GetMaxConnectionsPerDownload = %d, want %d", got, PerDownloadMax)
		}
		if got := r.GetMinChunkSize(); got != MinChunk {
			t.Errorf("GetMinChunkSize = %d, want %d", got, MinChunk)
		}
		if got := r.GetWorkerBufferSize(); got != WorkerBuffer {
			t.Errorf("GetWorkerBufferSize = %d, want %d", got, WorkerBuffer)
		}
		if got := r.GetMaxTaskRetries(); got != MaxTaskRetries {
			t.Errorf("GetMaxTaskRetries = %d, want %d", got, MaxTaskRetries)
		}
		if got := r.GetSlowWorkerThreshold(); got != SlowWorkerThreshold {
			t.Errorf("GetSlowWorkerThreshold = %f, want %f", got, SlowWorkerThreshold)
		}
		if got := r.GetSlowWorkerGracePeriod(); got != SlowWorkerGrace {
			t.Errorf("GetSlowWorkerGracePeriod = %v, want %v", got, SlowWorkerGrace)
		}
		if got := r.GetStallTimeout(); got != StallTimeout {
			t.Errorf("GetStallTimeout = %v, want %v", got, StallTimeout)
		}
		if got := r.GetSpeedEmaAlpha(); got != SpeedEMAAlpha {
			t.Errorf("GetSpeedEmaAlpha = %f, want %f", got, SpeedEMAAlpha)
		}
		if r.IsAdaptiveConcurrencyEnabled() {
			t.Error("adaptive concurrency should default to disabled")
		}
		if got := r.GetAdaptiveConcurrencyInterval(); got != DefaultAdaptiveConcurrencyInterval {
			t.Errorf("GetAdaptiveConcurrencyInterval = %v, want %v", got, DefaultAdaptiveConcurrencyInterval)
		}
	})

	t.Run("zero values return defaults", func(t *testing.T) {
		r := &RuntimeConfig{} // All zero values

		if got := r.GetMaxConnectionsPerDownload(); got != PerDownloadMax {
			t.Errorf("GetMaxConnectionsPerDownload = %d, want %d", got, PerDownloadMax)
		}
		if got := r.GetMinChunkSize(); got != MinChunk {
			t.Errorf("GetMinChunkSize = %d, want %d", got, MinChunk)
		}

		if got := r.GetWorkerBufferSize(); got != WorkerBuffer {
			t.Errorf("GetWorkerBufferSize = %d, want %d", got, WorkerBuffer)
		}
	})

	t.Run("explicit zero values are preserved where zero is valid", func(t *testing.T) {
		r := &RuntimeConfig{
			MaxTaskRetries:              0,
			SlowWorkerThreshold:         0,
			SlowWorkerGracePeriod:       0,
			StallTimeout:                0,
			SpeedEmaAlpha:               0,
			AdaptiveConcurrencyInterval: 0,
		}
		if got := r.GetMaxTaskRetries(); got != 0 {
			t.Errorf("GetMaxTaskRetries = %d, want 0", got)
		}

		if got := r.GetSlowWorkerThreshold(); got != 0 {
			t.Errorf("GetSlowWorkerThreshold = %f, want 0", got)
		}
		if got := r.GetSlowWorkerGracePeriod(); got != 0 {
			t.Errorf("GetSlowWorkerGracePeriod = %v, want 0", got)
		}
		if got := r.GetStallTimeout(); got != 0 {
			t.Errorf("GetStallTimeout = %v, want 0", got)
		}
		if got := r.GetSpeedEmaAlpha(); got != 0 {
			t.Errorf("GetSpeedEmaAlpha = %f, want 0", got)
		}
		if r.IsAdaptiveConcurrencyEnabled() {
			t.Error("adaptive concurrency should be disabled")
		}
	})

	t.Run("invalid values fall back to defaults", func(t *testing.T) {
		r := &RuntimeConfig{
			MaxTaskRetries:              -1,
			SlowWorkerThreshold:         1.5,
			SlowWorkerGracePeriod:       -1 * time.Second,
			StallTimeout:                -1 * time.Second,
			SpeedEmaAlpha:               -0.1,
			AdaptiveConcurrencyInterval: -1 * time.Second,
		}

		if got := r.GetMaxTaskRetries(); got != MaxTaskRetries {
			t.Errorf("GetMaxTaskRetries = %d, want %d", got, MaxTaskRetries)
		}
		if got := r.GetSlowWorkerThreshold(); got != SlowWorkerThreshold {
			t.Errorf("GetSlowWorkerThreshold = %f, want %f", got, SlowWorkerThreshold)
		}
		if got := r.GetSlowWorkerGracePeriod(); got != SlowWorkerGrace {
			t.Errorf("GetSlowWorkerGracePeriod = %v, want %v", got, SlowWorkerGrace)
		}
		if got := r.GetStallTimeout(); got != StallTimeout {
			t.Errorf("GetStallTimeout = %v, want %v", got, StallTimeout)
		}
		if got := r.GetSpeedEmaAlpha(); got != SpeedEMAAlpha {
			t.Errorf("GetSpeedEmaAlpha = %f, want %f", got, SpeedEMAAlpha)
		}
		if got := r.GetAdaptiveConcurrencyInterval(); got != DefaultAdaptiveConcurrencyInterval {
			t.Errorf("GetAdaptiveConcurrencyInterval = %v, want %v", got, DefaultAdaptiveConcurrencyInterval)
		}
	})

	t.Run("custom values are returned", func(t *testing.T) {
		r := &RuntimeConfig{
			MaxConnectionsPerDownload:   128,
			UserAgent:                   "CustomAgent/1.0",
			MinChunkSize:                4 * utils.MiB,
			WorkerBufferSize:            1 * utils.MiB,
			MaxTaskRetries:              5,
			SlowWorkerThreshold:         0.75,
			SlowWorkerGracePeriod:       10 * time.Second,
			StallTimeout:                15 * time.Second,
			SpeedEmaAlpha:               0.5,
			AdaptiveConcurrencyInterval: 5 * time.Second,
		}

		if got := r.GetMaxConnectionsPerDownload(); got != 128 {
			t.Errorf("GetMaxConnectionsPerDownload = %d, want 128", got)
		}
		if got := r.GetUserAgent(); got != "CustomAgent/1.0" {
			t.Errorf("GetUserAgent = %s, want CustomAgent/1.0", got)
		}
		if got := r.GetMinChunkSize(); got != 4*utils.MiB {
			t.Errorf("GetMinChunkSize = %d, want %d", got, 4*utils.MiB)
		}

		if got := r.GetWorkerBufferSize(); got != 1*utils.MiB {
			t.Errorf("GetWorkerBufferSize = %d, want %d", got, 1*utils.MiB)
		}
		if got := r.GetMaxTaskRetries(); got != 5 {
			t.Errorf("GetMaxTaskRetries = %d, want 5", got)
		}
		if got := r.GetSlowWorkerThreshold(); got != 0.75 {
			t.Errorf("GetSlowWorkerThreshold = %f, want 0.75", got)
		}
		if got := r.GetSlowWorkerGracePeriod(); got != 10*time.Second {
			t.Errorf("GetSlowWorkerGracePeriod = %v, want %v", got, 10*time.Second)
		}
		if got := r.GetStallTimeout(); got != 15*time.Second {
			t.Errorf("GetStallTimeout = %v, want %v", got, 15*time.Second)
		}
		if got := r.GetSpeedEmaAlpha(); got != 0.5 {
			t.Errorf("GetSpeedEmaAlpha = %f, want 0.5", got)
		}
		if got := r.GetAdaptiveConcurrencyInterval(); got != 5*time.Second {
			t.Errorf("GetAdaptiveConcurrencyInterval = %v, want 5s", got)
		}
	})
}

func TestDefaultRuntimeConfig_PopulatesDefaults(t *testing.T) {
	r := DefaultRuntimeConfig()
	if r == nil {
		t.Fatal("DefaultRuntimeConfig returned nil")
	}

	if r.MaxConnectionsPerDownload != PerDownloadMax {
		t.Errorf("MaxConnectionsPerDownload = %d, want %d", r.MaxConnectionsPerDownload, PerDownloadMax)
	}
	if r.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q, want %q", r.UserAgent, DefaultUserAgent)
	}
	if r.MinChunkSize != MinChunk {
		t.Errorf("MinChunkSize = %d, want %d", r.MinChunkSize, MinChunk)
	}
	if r.WorkerBufferSize != WorkerBuffer {
		t.Errorf("WorkerBufferSize = %d, want %d", r.WorkerBufferSize, WorkerBuffer)
	}
	if r.MaxTaskRetries != MaxTaskRetries {
		t.Errorf("MaxTaskRetries = %d, want %d", r.MaxTaskRetries, MaxTaskRetries)
	}
	if r.DialHedgeCount != DialHedgeCount {
		t.Errorf("DialHedgeCount = %d, want %d", r.DialHedgeCount, DialHedgeCount)
	}
	if r.SlowWorkerThreshold != SlowWorkerThreshold {
		t.Errorf("SlowWorkerThreshold = %f, want %f", r.SlowWorkerThreshold, SlowWorkerThreshold)
	}
	if r.AdaptiveConcurrencyInterval != DefaultAdaptiveConcurrencyInterval {
		t.Errorf("AdaptiveConcurrencyInterval = %v, want %v", r.AdaptiveConcurrencyInterval, DefaultAdaptiveConcurrencyInterval)
	}
	if r.SlowWorkerGracePeriod != SlowWorkerGrace {
		t.Errorf("SlowWorkerGracePeriod = %v, want %v", r.SlowWorkerGracePeriod, SlowWorkerGrace)
	}
	if r.StallTimeout != StallTimeout {
		t.Errorf("StallTimeout = %v, want %v", r.StallTimeout, StallTimeout)
	}
	if r.SpeedEmaAlpha != SpeedEMAAlpha {
		t.Errorf("SpeedEmaAlpha = %f, want %f", r.SpeedEmaAlpha, SpeedEMAAlpha)
	}
}

func TestSizeConstants(t *testing.T) {
	// Verify size constant relationships
	if utils.KiB != 1024 {
		t.Errorf("KB = %d, want 1024", utils.KiB)
	}
	if utils.MiB != 1024*utils.KiB {
		t.Errorf("MB = %d, want %d", utils.MiB, 1024*utils.KiB)
	}
	if utils.GiB != 1024*utils.MiB {
		t.Errorf("GB = %d, want %d", utils.GiB, 1024*utils.MiB)
	}

	// Verify alignment
	if AlignSize <= 0 {
		t.Errorf("AlignSize = %d, should be positive", AlignSize)
	}
	if AlignSize&(AlignSize-1) != 0 {
		t.Error("AlignSize should be a power of 2")
	}
}

func TestTimeoutConstants(t *testing.T) {
	// Verify timeouts are reasonable (not zero, not too long)
	timeouts := map[string]time.Duration{
		"DefaultIdleConnTimeout":       DefaultIdleConnTimeout,
		"DefaultTLSHandshakeTimeout":   DefaultTLSHandshakeTimeout,
		"DefaultResponseHeaderTimeout": DefaultResponseHeaderTimeout,
		"DefaultExpectContinueTimeout": DefaultExpectContinueTimeout,
		"DialTimeout":                  DialTimeout,
		"KeepAliveDuration":            KeepAliveDuration,
		"ProbeTimeout":                 ProbeTimeout,
		"HealthCheckInterval":          HealthCheckInterval,
		"SlowWorkerGrace":              SlowWorkerGrace,
		"StallTimeout":                 StallTimeout,
		"RetryBaseDelay":               RetryBaseDelay,
	}

	for name, timeout := range timeouts {
		if timeout <= 0 {
			t.Errorf("%s = %v, should be positive", name, timeout)
		}
		if timeout > 5*time.Minute {
			t.Errorf("%s = %v, seems too long", name, timeout)
		}
	}
}

func TestConnectionLimits(t *testing.T) {
	if PerDownloadMax <= 0 {
		t.Error("PerDownloadMax should be positive")
	}
	if PerDownloadMax > 256 {
		t.Error("PerDownloadMax seems too high")
	}
	// Check DefaultMaxIdleConns if available (int type)
	if DefaultMaxIdleConns <= 0 {
		t.Error("DefaultMaxIdleConns should be positive")
	}
}

func TestChannelBufferSizes(t *testing.T) {
	if ProgressChannelBuffer <= 0 {
		t.Error("ProgressChannelBuffer should be positive")
	}
}

func TestDownloadRecord_Fields(t *testing.T) {
	state := &struct{}{}
	runtime := &RuntimeConfig{MaxConnectionsPerDownload: 8}

	cfg := DownloadRecord{
		URL:           "https://example.com/file.zip",
		OutputPath:    "/tmp/file.zip",
		ID:            "download-123",
		Filename:      "file.zip",
		ProgressCh:    nil,
		ProgressState: state,
		Runtime:       runtime,
	}

	if cfg.URL != "https://example.com/file.zip" {
		t.Error("URL not set correctly")
	}
	if cfg.OutputPath != "/tmp/file.zip" {
		t.Error("OutputPath not set correctly")
	}
	if cfg.ID != "download-123" {
		t.Error("ID not set correctly")
	}
	if cfg.ProgressState != state {
		t.Error("State not set correctly")
	}
	if cfg.Runtime != runtime {
		t.Error("Runtime not set correctly")
	}
}
