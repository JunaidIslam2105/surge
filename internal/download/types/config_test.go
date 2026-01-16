package types

import (
	"testing"
	"time"
)

// =============================================================================
// Constants Tests
// =============================================================================

func TestSizeConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      int64
		expected int64
	}{
		{"KB", KB, 1024},
		{"MB", MB, 1024 * 1024},
		{"GB", GB, 1024 * 1024 * 1024},
		{"Megabyte", int64(Megabyte), 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestChunkSizeConstants(t *testing.T) {
	// Verify sensible defaults
	if MinChunk <= 0 {
		t.Error("MinChunk should be positive")
	}
	if MaxChunk <= MinChunk {
		t.Error("MaxChunk should be greater than MinChunk")
	}
	if TargetChunk < MinChunk || TargetChunk > MaxChunk {
		t.Error("TargetChunk should be between MinChunk and MaxChunk")
	}
	if AlignSize <= 0 {
		t.Error("AlignSize should be positive")
	}
	if AlignSize&(AlignSize-1) != 0 {
		t.Error("AlignSize should be a power of 2")
	}
	if WorkerBuffer <= 0 {
		t.Error("WorkerBuffer should be positive")
	}
	if TasksPerWorker <= 0 {
		t.Error("TasksPerWorker should be positive")
	}
}

func TestConnectionLimits(t *testing.T) {
	if PerHostMax <= 0 {
		t.Error("PerHostMax should be positive")
	}
	if PerHostMax > 256 {
		t.Error("PerHostMax seems too high")
	}
}

func TestHTTPClientTimeouts(t *testing.T) {
	timeouts := []struct {
		name    string
		timeout time.Duration
	}{
		{"DefaultMaxIdleConns", time.Duration(DefaultMaxIdleConns)},
		{"DefaultIdleConnTimeout", DefaultIdleConnTimeout},
		{"DefaultTLSHandshakeTimeout", DefaultTLSHandshakeTimeout},
		{"DefaultResponseHeaderTimeout", DefaultResponseHeaderTimeout},
		{"DefaultExpectContinueTimeout", DefaultExpectContinueTimeout},
		{"DialTimeout", DialTimeout},
		{"KeepAliveDuration", KeepAliveDuration},
		{"ProbeTimeout", ProbeTimeout},
	}

	for _, tt := range timeouts {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "DefaultMaxIdleConns" {
				if DefaultMaxIdleConns <= 0 {
					t.Errorf("%s should be positive", tt.name)
				}
			} else {
				if tt.timeout <= 0 {
					t.Errorf("%s should be positive", tt.name)
				}
			}
		})
	}
}

func TestChannelBufferSizes(t *testing.T) {
	if ProgressChannelBuffer <= 0 {
		t.Error("ProgressChannelBuffer should be positive")
	}
}

// =============================================================================
// DownloadConfig Tests
// =============================================================================

func TestDownloadConfig_Fields(t *testing.T) {
	state := NewProgressState("test", 1000)
	runtime := &RuntimeConfig{MaxConnectionsPerHost: 8}

	cfg := DownloadConfig{
		URL:        "https://example.com/file.zip",
		OutputPath: "/tmp/file.zip",
		ID:         "download-123",
		Filename:   "file.zip",
		Verbose:    true,
		ProgressCh: nil,
		State:      state,
		Runtime:    runtime,
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
	if !cfg.Verbose {
		t.Error("Verbose not set correctly")
	}
	if cfg.State != state {
		t.Error("State not set correctly")
	}
	if cfg.Runtime != runtime {
		t.Error("Runtime not set correctly")
	}
}

// =============================================================================
// RuntimeConfig Getter Tests
// =============================================================================

func TestRuntimeConfig_GetUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected string
	}{
		{"nil config", nil, ""}, // Check it doesn't panic, default UA
		{"empty UserAgent", &RuntimeConfig{}, ""},
		{"custom UserAgent", &RuntimeConfig{UserAgent: "MyAgent/1.0"}, "MyAgent/1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetUserAgent()
			if tt.expected == "" {
				// For nil/empty, should return non-empty default
				if got == "" {
					t.Error("GetUserAgent should return default UA, not empty")
				}
			} else {
				if got != tt.expected {
					t.Errorf("GetUserAgent() = %q, want %q", got, tt.expected)
				}
			}
		})
	}
}

func TestRuntimeConfig_GetMaxConnectionsPerHost(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected int
	}{
		{"nil config", nil, PerHostMax},
		{"zero value", &RuntimeConfig{MaxConnectionsPerHost: 0}, PerHostMax},
		{"negative value", &RuntimeConfig{MaxConnectionsPerHost: -1}, PerHostMax},
		{"custom value", &RuntimeConfig{MaxConnectionsPerHost: 16}, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetMaxConnectionsPerHost()
			if got != tt.expected {
				t.Errorf("GetMaxConnectionsPerHost() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetMinChunkSize(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected int64
	}{
		{"nil config", nil, MinChunk},
		{"zero value", &RuntimeConfig{MinChunkSize: 0}, MinChunk},
		{"custom value", &RuntimeConfig{MinChunkSize: 1 * MB}, 1 * MB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetMinChunkSize()
			if got != tt.expected {
				t.Errorf("GetMinChunkSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetMaxChunkSize(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected int64
	}{
		{"nil config", nil, MaxChunk},
		{"zero value", &RuntimeConfig{MaxChunkSize: 0}, MaxChunk},
		{"custom value", &RuntimeConfig{MaxChunkSize: 32 * MB}, 32 * MB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetMaxChunkSize()
			if got != tt.expected {
				t.Errorf("GetMaxChunkSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetTargetChunkSize(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected int64
	}{
		{"nil config", nil, TargetChunk},
		{"custom value", &RuntimeConfig{TargetChunkSize: 4 * MB}, 4 * MB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetTargetChunkSize()
			if got != tt.expected {
				t.Errorf("GetTargetChunkSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetWorkerBufferSize(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected int
	}{
		{"nil config", nil, WorkerBuffer},
		{"custom value", &RuntimeConfig{WorkerBufferSize: 256 * KB}, 256 * KB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetWorkerBufferSize()
			if got != tt.expected {
				t.Errorf("GetWorkerBufferSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetMaxTaskRetries(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected int
	}{
		{"nil config", nil, MaxTaskRetries}, // Fixed casing from maxTaskRetries
		{"custom value", &RuntimeConfig{MaxTaskRetries: 5}, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetMaxTaskRetries()
			if got != tt.expected {
				t.Errorf("GetMaxTaskRetries() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetSlowWorkerThreshold(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected float64
	}{
		{"nil config", nil, SlowWorkerThreshold}, // Fixed casing
		{"custom value", &RuntimeConfig{SlowWorkerThreshold: 0.25}, 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetSlowWorkerThreshold()
			if got != tt.expected {
				t.Errorf("GetSlowWorkerThreshold() = %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetSlowWorkerGracePeriod(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected time.Duration
	}{
		{"nil config", nil, SlowWorkerGrace}, // Fixed casing
		{"custom value", &RuntimeConfig{SlowWorkerGracePeriod: 10 * time.Second}, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetSlowWorkerGracePeriod()
			if got != tt.expected {
				t.Errorf("GetSlowWorkerGracePeriod() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetStallTimeout(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected time.Duration
	}{
		{"nil config", nil, StallTimeout}, // Fixed casing
		{"custom value", &RuntimeConfig{StallTimeout: 15 * time.Second}, 15 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetStallTimeout()
			if got != tt.expected {
				t.Errorf("GetStallTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRuntimeConfig_GetSpeedEmaAlpha(t *testing.T) {
	tests := []struct {
		name     string
		runtime  *RuntimeConfig
		expected float64
	}{
		{"nil config", nil, SpeedEMAAlpha}, // Fixed casing
		{"custom value", &RuntimeConfig{SpeedEmaAlpha: 0.5}, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.runtime.GetSpeedEmaAlpha()
			if got != tt.expected {
				t.Errorf("GetSpeedEmaAlpha() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// RuntimeConfig Complete Configuration Test
// =============================================================================

func TestRuntimeConfig_FullConfiguration(t *testing.T) {
	rc := &RuntimeConfig{
		MaxConnectionsPerHost: 32,
		MaxGlobalConnections:  100,
		UserAgent:             "TestAgent/2.0",
		MinChunkSize:          1 * MB,
		MaxChunkSize:          32 * MB,
		TargetChunkSize:       8 * MB,
		WorkerBufferSize:      512 * KB,
		MaxTaskRetries:        5,
		SlowWorkerThreshold:   0.3,
		SlowWorkerGracePeriod: 5 * time.Second,
		StallTimeout:          10 * time.Second,
		SpeedEmaAlpha:         0.4,
	}

	// Verify all getters return the configured values
	if rc.GetMaxConnectionsPerHost() != 32 {
		t.Error("MaxConnectionsPerHost mismatch")
	}
	if rc.GetUserAgent() != "TestAgent/2.0" {
		t.Error("UserAgent mismatch")
	}
	if rc.GetMinChunkSize() != 1*MB {
		t.Error("MinChunkSize mismatch")
	}
	if rc.GetMaxChunkSize() != 32*MB {
		t.Error("MaxChunkSize mismatch")
	}
	if rc.GetTargetChunkSize() != 8*MB {
		t.Error("TargetChunkSize mismatch")
	}
	if rc.GetWorkerBufferSize() != 512*KB {
		t.Error("WorkerBufferSize mismatch")
	}
	if rc.GetMaxTaskRetries() != 5 {
		t.Error("MaxTaskRetries mismatch")
	}
	if rc.GetSlowWorkerThreshold() != 0.3 {
		t.Error("SlowWorkerThreshold mismatch")
	}
	if rc.GetSlowWorkerGracePeriod() != 5*time.Second {
		t.Error("SlowWorkerGracePeriod mismatch")
	}
	if rc.GetStallTimeout() != 10*time.Second {
		t.Error("StallTimeout mismatch")
	}
	if rc.GetSpeedEmaAlpha() != 0.4 {
		t.Error("SpeedEmaAlpha mismatch")
	}
}
