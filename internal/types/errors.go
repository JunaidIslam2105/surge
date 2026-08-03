package types

import (
	"errors"
	"net/http"
)

// Common errors
var (
	ErrPaused             = errors.New("download paused")
	ErrNotFound           = errors.New("download not found")
	ErrCompleted          = errors.New("download already completed")
	ErrPausing            = errors.New("download is still pausing, try again in a moment")
	ErrEngineNotInit      = errors.New("engine not initialized")
	ErrPoolNotInit        = errors.New("worker pool not initialized")
	ErrIDExists           = errors.New("download id already exists")
	ErrURLRequired        = errors.New("URL is required")
	ErrDestRequired       = errors.New("destination path is required")
	ErrServiceUnavailable = errors.New("service unavailable")
	ErrQueuedUpdate       = errors.New("cannot update URL for a queued download, please cancel or wait for it to start")
	ErrActiveUpdate       = errors.New("download is currently active, please pause it before updating the URL")
	ErrMaxRedirects       = errors.New("stopped after 10 redirects")
	ErrAlreadyActive      = errors.New("download is already active or queued")

	// ErrPermanentHTTP indicates the server returned a 4xx status (other than 429)
	// that makes retrying pointless (e.g. 403 Forbidden, 404 Not Found, 401 Unauthorized).
	ErrPermanentHTTP = errors.New("permanent HTTP error")

	// ErrInsufficientDiskSpace indicates the destination volume is full or
	// the user's quota has been exhausted. Retry is pointless.
	ErrInsufficientDiskSpace = errors.New("insufficient disk space")
)

// IsPermanentHTTPError reports whether err is a permanent HTTP error that
// should not be retried by the scheduler.
func IsPermanentHTTPError(err error) bool {
	return errors.Is(err, ErrPermanentHTTP)
}

// IsPermanentHTTPStatus reports whether the given HTTP status code represents
// a permanent failure that should not be retried.
// 429 Too Many Requests is explicitly excluded because it is transient.
func IsPermanentHTTPStatus(code int) bool {
	return code >= http.StatusBadRequest &&
		code < http.StatusInternalServerError &&
		code != http.StatusTooManyRequests
}
