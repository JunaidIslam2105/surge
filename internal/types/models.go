package types

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"
)

// Task represents a byte range to download.
type Task struct {
	Offset          int64         `json:"offset"`
	Length          int64         `json:"length"`
	SharedMaxOffset *atomic.Int64 `json:"-"`
}

func (t *Task) GobEncode() ([]byte, error) {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint64(b[0:8], uint64(t.Offset))
	binary.LittleEndian.PutUint64(b[8:16], uint64(t.Length))
	return b, nil
}

func (t *Task) GobDecode(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) < 16 {
		return fmt.Errorf("corrupt task data: expected 16 bytes, got %d", len(data))
	}
	t.Offset = int64(binary.LittleEndian.Uint64(data[0:8]))
	t.Length = int64(binary.LittleEndian.Uint64(data[8:16]))
	return nil
}

// DownloadRecord is the canonical representation of a download's static configuration,
// persistent state, and runtime options. It replaces DownloadState, DownloadEntry, and DownloadConfig.
type DownloadRecord struct {
	// Identity & Core Info
	ID         string `json:"id"`
	URLHash    string `json:"url_hash"`
	URL        string `json:"url"`
	Filename   string `json:"filename"`
	OutputPath string `json:"output_path"`
	DestPath   string `json:"dest_path"`
	TotalSize  int64  `json:"total_size"`
	Downloaded int64  `json:"downloaded"`

	// Status
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`

	// Lifecycle Timestamps & Stats
	CreatedAt   int64   `json:"created_at"`
	PausedAt    int64   `json:"paused_at,omitempty"`
	CompletedAt int64   `json:"completed_at,omitempty"`
	TimeTaken   int64   `json:"time_taken,omitempty"`
	Elapsed     int64   `json:"elapsed,omitempty"`
	AvgSpeed    float64 `json:"avg_speed,omitempty"`

	// Resume State (Persistent)
	Tasks           []Task `json:"tasks,omitempty"`
	ChunkBitmap     []byte `json:"chunk_bitmap,omitempty"`
	ActualChunkSize int64  `json:"actual_chunk_size,omitempty"`
	FileHash        string `json:"file_hash,omitempty"`

	// Configuration Options (Persistent)
	Mirrors      []string `json:"mirrors,omitempty"`
	RateLimit    int64    `json:"rate_limit,omitempty"`
	RateLimitSet bool     `json:"rate_limit_set,omitempty"`
	Workers      int      `json:"workers,omitempty"`
	MinChunkSize int64    `json:"min_chunk_size,omitempty"`
	TLSCAFile    string   `json:"tls_ca_file,omitempty"`
	TLSInsecure  bool     `json:"tls_insecure,omitempty"`

	// Runtime / Transient Configuration (Not persisted)
	IsResume           bool                 `json:"-" gob:"-"`
	ProgressCh         chan<- DownloadEvent `json:"-" gob:"-"`
	ProgressState      interface{}          `json:"-" gob:"-"` // typically *progress.DownloadProgress
	Runtime            *RuntimeConfig       `json:"-" gob:"-"`
	Headers            map[string]string    `json:"-" gob:"-"`
	Limiter            ByteLimiter          `json:"-" gob:"-"`
	IsExplicitCategory bool                 `json:"-" gob:"-"`
	SupportsRange      bool                 `json:"-" gob:"-"`
}

// MasterList holds all tracked downloads.
type MasterList struct {
	Downloads []DownloadRecord `json:"downloads"`
}

// DownloadStatus is the transient view returned to the TUI and API clients.
type DownloadStatus struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	Filename     string  `json:"filename"`
	DestPath     string  `json:"dest_path,omitempty"`
	TotalSize    int64   `json:"total_size"`
	Downloaded   int64   `json:"downloaded"`
	Progress     float64 `json:"progress"`
	Speed        float64 `json:"speed"`
	Status       string  `json:"status"`
	Error        string  `json:"error,omitempty"`
	ETA          int64   `json:"eta"`
	Connections  int     `json:"connections"`
	AddedAt      int64   `json:"added_at"`
	TimeTaken    int64   `json:"time_taken"`
	AvgSpeed     float64 `json:"avg_speed"`
	RateLimit    int64   `json:"rate_limit,omitempty"`
	RateLimitSet bool    `json:"rate_limit_set,omitempty"`
}

// CancelResult carries enough metadata for callers to emit lifecycle events
// without creating an import cycle back to the worker pool.
type CancelResult struct {
	Found     bool
	Filename  string
	DestPath  string
	Completed bool
	WasQueued bool
}

type MirrorStatus struct {
	URL    string
	Active bool
	Error  bool
}

// ChunkStatus represents the status of a visualization chunk
type ChunkStatus int

const (
	ChunkPending     ChunkStatus = 0 // 00
	ChunkDownloading ChunkStatus = 1 // 01
	ChunkCompleted   ChunkStatus = 2 // 10 (Bit 2 set)
)
