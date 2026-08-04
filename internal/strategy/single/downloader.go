package single

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// SingleDownloader handles single-threaded downloads for servers that don't support range requests.
// NOTE: Pause/resume is NOT supported because this downloader is only used when
// the server doesn't support Range headers. If interrupted, the download must restart.
type SingleDownloader struct {
	ProgressChan chan<- types.DownloadEvent // Channel for events (start/complete/error)
	ID           string                     // Download ID
	State        *progress.DownloadProgress // Shared state for TUI polling
	Runtime      *types.RuntimeConfig
	Limiter      types.ByteLimiter
	TotalSize    int64
	Headers      map[string]string // Custom HTTP headers (cookies, auth, etc.)
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*utils.KiB)
		return &b
	},
}

// NewSingleDownloader creates a new single-threaded downloader with all required parameters
func NewSingleDownloader(id string, progressCh chan<- types.DownloadEvent, state *progress.DownloadProgress, runtime *types.RuntimeConfig) *SingleDownloader {
	if runtime == nil {
		runtime = types.DefaultRuntimeConfig()
	}

	return &SingleDownloader{
		ProgressChan: progressCh,
		ID:           id,
		State:        state,
		Runtime:      runtime,
	}
}

func (d *SingleDownloader) applyClientSettings(client *http.Client) {
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return types.ErrMaxRedirects
		}
		if len(via) > 0 {
			utils.CopyRedirectHeaders(req, via[0])
		}
		return nil
	}
}

// Download downloads a file using a single connection.
// This is used for servers that don't support Range requests.
// If interrupted, the download cannot be resumed and must restart from the beginning.
func (d *SingleDownloader) Download(ctx context.Context, rawurl, destPath string, fileSize int64, filename string) (err error) {
	httpTransport := transport.DefaultNetworkPool.AcquireTransport(d.Runtime.ProxyURL, d.Runtime.CustomDNS, types.PoolMaxConnsPerHost)
	defer transport.DefaultNetworkPool.ReleaseTransport(httpTransport)

	client := &http.Client{Transport: httpTransport}
	d.applyClientSettings(client)

	if d.State != nil {
		d.State.SetURL(rawurl)
		d.State.SetDestPath(destPath)
		d.State.ActiveWorkers.Store(1)
		defer d.State.ActiveWorkers.Store(0)
	}

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if d.State != nil {
		d.State.SetCancelFunc(cancel)
	}

	buildRequest := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, err
		}
		for key, val := range d.Headers {
			if strings.EqualFold(key, "Range") {
				continue
			}
			req.Header.Set(key, val)
		}
		req.Header.Set("User-Agent", d.Runtime.GetUserAgent())
		return req, nil
	}

	var resp *http.Response
	const maxRlRetries = types.RateLimitMaxRetries
	rlRetries := 0

	for {
		req, err := buildRequest()
		if err != nil {
			return err
		}

		resp, err = client.Do(req)
		if err != nil {
			utils.Debug("Single downloader: GET %s failed: %v", rawurl, err)
			return err
		}

		if resp.StatusCode == http.StatusOK {
			break
		}

		if resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("Retry-After") != "") {
			_ = resp.Body.Close()
			rlRetries++
			if rlRetries > maxRlRetries {
				utils.Debug("Single downloader: rate limited after %d retries for %s", maxRlRetries, rawurl)
				return fmt.Errorf("rate limited after %d retries: %d", maxRlRetries, resp.StatusCode)
			}

			ra, _ := transport.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			if ra <= 0 {
				ra = 5 * time.Second
			}
			utils.Debug("Single downloader: rate limited (%d), waiting %v (retry %d/%d)", resp.StatusCode, ra, rlRetries, maxRlRetries)
			if d.State != nil {
				d.State.RateLimited.Store(true)
			}
			select {
			case <-dlCtx.Done():
				return dlCtx.Err()
			case <-time.After(ra):
			}
			if d.State != nil {
				d.State.RateLimited.Store(false)
			}
			continue
		}

		_ = resp.Body.Close()
		utils.Debug("Single downloader: unexpected status %d for %s", resp.StatusCode, rawurl)
		if types.IsPermanentHTTPStatus(resp.StatusCode) {
			return fmt.Errorf("unexpected status code: %d: %w", resp.StatusCode, types.ErrPermanentHTTP)
		}
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			utils.Debug("Error closing response body: %v", cerr)
		}
	}()

	if fileSize <= 0 && resp.ContentLength > 0 {
		fileSize = resp.ContentLength
	}
	d.TotalSize = fileSize
	if d.State != nil && fileSize > 0 {
		d.State.SetTotalSize(fileSize)
	}

	// Use .surge extension for incomplete file (must be pre-created by processing layer)
	workingPath := destPath + types.IncompleteSuffix
	outFile, err := os.OpenFile(workingPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close error: %w", cerr)
		}
	}()

	preallocated := false
	if fileSize > 0 {
		if err := preallocateFile(outFile, fileSize); err != nil {
			return fmt.Errorf("failed to preallocate file: %w", err)
		}
		preallocated = true
	}

	start := time.Now()
	var written int64

	bufPtr := bufPool.Get().(*[]byte)
	buf := *bufPtr
	defer bufPool.Put(bufPtr)

	reader := io.Reader(resp.Body)
	if d.Limiter != nil {
		reader = &throttledReader{reader: resp.Body, limiter: d.Limiter, ctx: ctx}
	}

	if d.State == nil {
		written, err = io.CopyBuffer(outFile, reader, buf)
	} else {
		progressReader := newProgressReader(reader, d.State, types.WorkerBatchSize, types.WorkerBatchInterval)
		written, err = io.CopyBuffer(outFile, progressReader, buf)
		progressReader.Flush()
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		utils.Debug("Single downloader: copy error for %s: %v", rawurl, err)
		return fmt.Errorf("copy error: %w", err)
	}

	if preallocated && written != fileSize {
		if err := outFile.Truncate(written); err != nil {
			return fmt.Errorf("truncate error: %w", err)
		}
	}

	if err := outFile.Sync(); err != nil {
		return fmt.Errorf("sync error: %w", err)
	}

	if d.State != nil {
		d.State.Bytes.Downloaded.Store(written)
		d.State.Bytes.VerifiedProgress.Store(written)
	}

	elapsed := time.Since(start)
	speed := 0.0
	if elapsed > 0 {
		speed = float64(written) / elapsed.Seconds()
	}
	utils.Debug("\nDownloaded %s in %s (%s/s)\n",
		destPath,
		elapsed.Round(time.Second),
		utils.FormatBytes(int64(speed)),
	)

	return nil
}

type throttledReader struct {
	reader  io.Reader
	limiter types.ByteLimiter
	ctx     context.Context
}

func (t *throttledReader) Read(p []byte) (int, error) {
	n, err := t.reader.Read(p)
	if n > 0 && t.limiter != nil {
		if waitErr := t.limiter.WaitN(t.ctx, int64(n)); waitErr != nil {
			// Preserve an underlying transport/read failure from the same Read call.
			// io.Reader permits returning both n > 0 and err != nil.
			if err != nil && err != io.EOF {
				return n, err
			}
			return n, waitErr
		}
	}
	return n, err
}

type progressReader struct {
	reader        io.Reader
	state         *progress.DownloadProgress
	batchSize     int64
	batchInterval time.Duration
	written       int64
	pending       int64
	pendingStart  int64
	lastFlush     time.Time
	readChecks    uint8
}

func newProgressReader(reader io.Reader, state *progress.DownloadProgress, batchSize int64, batchInterval time.Duration) *progressReader {
	if batchSize <= 0 {
		batchSize = types.WorkerBatchSize
	}
	return &progressReader{
		reader:        reader,
		state:         state,
		batchSize:     batchSize,
		batchInterval: batchInterval,
		lastFlush:     time.Now(),
	}
}

func (w *progressReader) Read(p []byte) (int, error) {
	n, err := w.reader.Read(p)
	if n <= 0 || w.state == nil {
		return n, err
	}

	written := int64(n)
	w.written += written
	if w.pending == 0 {
		w.pendingStart = w.written - written
	}
	w.pending += written
	if w.pending >= w.batchSize {
		w.flushWithTime(time.Now())
		return n, err
	}

	if w.batchInterval > 0 {
		// Check wall-clock interval periodically to avoid calling time.Now on every read.
		w.readChecks++
		if w.readChecks >= 8 {
			now := time.Now()
			if now.Sub(w.lastFlush) >= w.batchInterval {
				w.flushWithTime(now)
			}
			w.readChecks = 0
		}
	}

	return n, err
}

func (w *progressReader) Flush() {
	w.flushWithTime(time.Now())
}

func (w *progressReader) flushWithTime(now time.Time) {
	if w.state == nil {
		w.pending = 0
		w.lastFlush = now
		w.readChecks = 0
		return
	}

	if w.pending == 0 && w.written == 0 {
		return
	}

	if w.pending > 0 {
		w.state.UpdateChunkStatus(w.pendingStart, w.pending, types.ChunkCompleted)
	}
	w.state.Bytes.Downloaded.Store(w.written)
	w.state.Bytes.VerifiedProgress.Store(w.written)
	w.pending = 0
	w.lastFlush = now
	w.readChecks = 0
}
