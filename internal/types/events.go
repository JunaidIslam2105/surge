package types

import (
	"encoding/json"
	"errors"
	"time"
)

type EventType int

const (
	EventStarted EventType = iota
	EventProgress
	EventComplete
	EventPaused
	EventResumed
	EventQueued
	EventRemoved
	EventError
	EventRequest
	EventBatchRequest
	EventBatchProgress
	EventSystem
)

type DownloadEvent struct {
	Type       EventType `json:"type"`
	DownloadID string    `json:"download_id,omitempty"`
	URL        string    `json:"url,omitempty"`
	Filename   string    `json:"filename,omitempty"`
	DestPath   string    `json:"dest_path,omitempty"`

	// Progress
	Downloaded    int64   `json:"downloaded,omitempty"`
	Total         int64   `json:"total,omitempty"`
	Speed         float64 `json:"speed,omitempty"`
	Connections   int     `json:"connections,omitempty"`
	RateLimited   bool    `json:"rate_limited,omitempty"`
	ChunkBitmap   []byte  `json:"chunk_bitmap,omitempty"`
	BitmapWidth   int     `json:"bitmap_width,omitempty"`
	ChunkSize     int64   `json:"chunk_size,omitempty"`
	ChunkProgress []int64 `json:"chunk_progress,omitempty"`

	// Completion
	Elapsed   time.Duration `json:"elapsed,omitempty"`
	AvgSpeed  float64       `json:"avg_speed,omitempty"`
	Completed bool          `json:"completed,omitempty"`

	// Error
	Err error `json:"-"`

	// Pause state
	State *DownloadRecord `json:"-"`

	// Config echo
	RateLimit    int64 `json:"rate_limit,omitempty"`
	RateLimitSet bool  `json:"rate_limit_set,omitempty"`
	// RateLimitSetPresent distinguishes omitted metadata from explicit RateLimitSet=false.
	RateLimitSetPresent bool     `json:"rate_limit_set_present,omitempty"`
	Workers             int      `json:"workers,omitempty"`
	MinChunkSize        int64    `json:"min_chunk_size,omitempty"`
	Mirrors             []string `json:"mirrors,omitempty"`

	// Request
	Headers map[string]string `json:"headers,omitempty"`
	Path    string            `json:"path,omitempty"`

	// Batch
	BatchEvents []DownloadEvent `json:"batch_events,omitempty"`

	// System
	Message string `json:"message,omitempty"`
}

func (m DownloadEvent) MarshalJSON() ([]byte, error) {
	type Alias DownloadEvent
	var errStr string
	if m.Err != nil {
		errStr = m.Err.Error()
	}
	return json.Marshal(&struct {
		Alias
		Err string `json:"error,omitempty"`
	}{
		Alias: (Alias)(m),
		Err:   errStr,
	})
}

func (m *DownloadEvent) UnmarshalJSON(data []byte) error {
	type Alias DownloadEvent
	aux := &struct {
		*Alias
		Err json.RawMessage `json:"error"`
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	m.Err = nil
	if len(aux.Err) > 0 {
		var errStr string
		if err := json.Unmarshal(aux.Err, &errStr); err == nil {
			if errStr != "" {
				m.Err = errors.New(errStr)
			}
		} else {
			raw := string(aux.Err)
			if raw != "" && raw != "null" {
				m.Err = errors.New(raw)
			}
		}
	}
	return nil
}

type BatchProgress []DownloadEvent

const (
	EventTypeProgress      = "progress"
	EventTypeStarted       = "started"
	EventTypeComplete      = "complete"
	EventTypeError         = "error"
	EventTypePaused        = "paused"
	EventTypeResumed       = "resumed"
	EventTypeQueued        = "queued"
	EventTypeRemoved       = "removed"
	EventTypeRequest       = "request"
	EventTypeBatchRequest  = "batch_request"
	EventTypeBatchProgress = "batch_progress"
	EventTypeSystem        = "system"
)

type SSEMessage struct {
	Event string
	Data  []byte
}

func EncodeSSEMessages(msg DownloadEvent) ([]SSEMessage, error) {
	eventType := EventTypeToString(msg.Type)
	if eventType == "" {
		return nil, nil
	}

	if msg.Type == EventBatchProgress {
		return EncodeBatchProgress(msg.BatchEvents)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return []SSEMessage{{
		Event: eventType,
		Data:  data,
	}}, nil
}

func EncodeBatchProgress(batch []DownloadEvent) ([]SSEMessage, error) {
	frames := make([]SSEMessage, 0, len(batch))
	for _, p := range batch {
		data, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		frames = append(frames, SSEMessage{
			Event: EventTypeProgress,
			Data:  data,
		})
	}
	return frames, nil
}

func EventTypeToString(t EventType) string {
	switch t {
	case EventStarted:
		return EventTypeStarted
	case EventProgress:
		return EventTypeProgress
	case EventComplete:
		return EventTypeComplete
	case EventPaused:
		return EventTypePaused
	case EventResumed:
		return EventTypeResumed
	case EventQueued:
		return EventTypeQueued
	case EventRemoved:
		return EventTypeRemoved
	case EventError:
		return EventTypeError
	case EventRequest:
		return EventTypeRequest
	case EventBatchRequest:
		return EventTypeBatchRequest
	case EventBatchProgress:
		return EventTypeBatchProgress
	case EventSystem:
		return EventTypeSystem
	default:
		return ""
	}
}

func DecodeSSEMessage(data []byte) (DownloadEvent, error) {
	var msg DownloadEvent
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, err
	}
	return msg, nil
}
