package concurrent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func TestConcurrentDownloader_SwitchOn429(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * utils.KiB)

	server1 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}),
	)
	defer server1.Close()

	server2 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server2.Close()

	destPath := filepath.Join(tmpDir, "switch429_test.bin")
	state := progress.New("switch429-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            5,
		MinChunkSize:              64 * utils.KiB,
		DialHedgeCount:            0, // Disable hedging for deterministic failover test
	}

	downloader := NewConcurrentDownloader("switch429-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{server1.URL(), server2.URL()}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server1.URL(), mirrors, mirrors, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	stateMirrors := state.GetMirrors()
	var badMirrorSeen, badMirrorErrored bool
	for _, m := range stateMirrors {
		if m.URL == server1.URL() {
			badMirrorSeen = true
			badMirrorErrored = m.Error
			break
		}
	}
	if !badMirrorSeen {
		t.Fatalf("Expected to track bad mirror %s in state, got: %+v", server1.URL(), stateMirrors)
	}
	if !badMirrorErrored {
		t.Fatalf("Expected bad mirror %s to be marked errored after 429, got: %+v", server1.URL(), stateMirrors)
	}
}

func TestConcurrentDownloader_BackoffOnSingleMirror(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(1 * utils.MiB) // Use enough size so it doesn't just finish instantly on 1st byte

	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithFailOnNthRequest(1),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "backoff_test.bin")
	state := progress.New("backoff-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            5,
		MinChunkSize:              64 * utils.KiB,
		DialHedgeCount:            0, // Disable hedging for deterministic backoff timing
	}

	downloader := NewConcurrentDownloader("backoff-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), mirrors, nil, destPath, fileSize)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if elapsed < 200*time.Millisecond {
		t.Errorf("Download took %v, but expected backoff wait (should be > 200ms)", elapsed)
	}
}

func TestConcurrentDownloader_AllMirrors429ThenRecover(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(64 * utils.KiB)

	var s1Count, s2Count atomic.Int64

	makeHandler := func(counter *atomic.Int64) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			n := counter.Add(1)
			if n <= 2 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
			w.WriteHeader(http.StatusOK)
			buf := make([]byte, 32*utils.KiB)
			for written := int64(0); written < fileSize; {
				n := int64(len(buf))
				if written+n > fileSize {
					n = fileSize - written
				}
				_, _ = w.Write(buf[:n])
				written += n
			}
		}
	}

	server1 := testutil.NewMockServerT(t,
		testutil.WithHandler(makeHandler(&s1Count)),
	)
	defer server1.Close()

	server2 := testutil.NewMockServerT(t,
		testutil.WithHandler(makeHandler(&s2Count)),
	)
	defer server2.Close()

	destPath := filepath.Join(tmpDir, "all429_test.bin")
	state := progress.New("all429-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 2,
		MaxTaskRetries:            3,
		MinChunkSize:              fileSize,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("all429-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{server1.URL(), server2.URL()}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server1.URL(), mirrors, mirrors, destPath, fileSize)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if elapsed < 900*time.Millisecond {
		t.Errorf("Expected coordinated backoff of ~1s, but download completed in %v", elapsed)
	}
}

func TestConcurrentDownloader_429RespectsRetryAfterHeader(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(128 * utils.KiB)

	var requestTimes []time.Time
	var mu sync.Mutex

	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requestTimes = append(requestTimes, time.Now())
			mu.Unlock()

			if len(requestTimes) == 1 {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
			w.WriteHeader(http.StatusOK)
			buf := make([]byte, fileSize)
			_, _ = w.Write(buf)
		}),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "retryafter_test.bin")
	state := progress.New("retryafter-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            3,
		MinChunkSize:              fileSize,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("retryafter-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), mirrors, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	mu.Lock()
	times := requestTimes
	mu.Unlock()

	if len(times) < 2 {
		t.Fatal("expected at least 2 requests")
	}

	gap := times[1].Sub(times[0])
	if gap < 900*time.Millisecond {
		t.Errorf("gap between 429 and next request %v; expected >= ~1s", gap)
	}
	if gap > 35*time.Second {
		t.Errorf("gap between 429 and next request %v; expected <= ~30s cap", gap)
	}
}

func TestConcurrentDownloader_429DoesNotTearDownWithHealthyMirror(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(1 * utils.MiB)

	server1 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}),
	)
	defer server1.Close()

	server2 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server2.Close()

	destPath := filepath.Join(tmpDir, "429healthy_test.bin")
	state := progress.New("429healthy-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 4,
		MaxTaskRetries:            3,
		MinChunkSize:              128 * utils.KiB,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("429healthy-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{server1.URL(), server2.URL()}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server1.URL(), mirrors, mirrors, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath+types.IncompleteSuffix, fileSize); err != nil {
		t.Error(err)
	}

	stateMirrors := state.GetMirrors()
	var badMirrorErrored bool
	for _, m := range stateMirrors {
		if m.URL == server1.URL() {
			badMirrorErrored = m.Error
			break
		}
	}
	if !badMirrorErrored {
		t.Fatal("Expected server1 to be flagged errored")
	}
}

func TestConcurrentDownloader_403DoesNotCancelHealthyWorker(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	const fileSize = int64(128 * utils.KiB)
	const chunkSize = fileSize / 2
	payload := make([]byte, chunkSize)
	healthyDone := make(chan struct{})
	var healthyDoneOnce sync.Once
	var forbiddenRequests atomic.Int64

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-65535" {
			select {
			case <-time.After(100 * time.Millisecond):
				healthyDoneOnce.Do(func() { close(healthyDone) })
			case <-r.Context().Done():
				return
			}
		} else if forbiddenRequests.Add(1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		} else {
			select {
			case <-healthyDone:
			case <-r.Context().Done():
				return
			}
		}

		w.Header().Set("Content-Length", strconv.FormatInt(chunkSize, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "soft403_healthy.bin")
	if f, err := os.Create(destPath + types.IncompleteSuffix); err == nil {
		_ = f.Close()
	}

	state := progress.New("soft403-healthy", fileSize)
	downloader := NewConcurrentDownloader("soft403-healthy", nil, state, &types.RuntimeConfig{
		MaxConnectionsPerDownload: 2,
		Workers:                   2,
		MinChunkSize:              chunkSize,
		MaxTaskRetries:            1,
		DialHedgeCount:            0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := downloader.Download(ctx, server.URL, nil, nil, destPath, fileSize); err != nil {
		t.Fatalf("Download failed after transient 403: %v", err)
	}
	if forbiddenRequests.Load() < 2 {
		t.Fatal("expected the forbidden range to be retried after its soft limit")
	}
}

func TestSoft403NilStateWaitsForConfirmation(t *testing.T) {
	downloader := NewConcurrentDownloader("soft403-nil-state", nil, nil, nil)
	now := time.Unix(100, 0)

	for i := 0; i < soft403MaxExhaustions; i++ {
		if downloader.shouldEscalate403(now) {
			t.Fatalf("escalated on exhaustion %d before confirmation", i+1)
		}
	}
	if downloader.shouldEscalate403(now.Add(soft403ConfirmWindow - time.Nanosecond)) {
		t.Fatal("escalated before the confirmation window elapsed")
	}
	if !downloader.shouldEscalate403(now.Add(soft403ConfirmWindow)) {
		t.Fatal("did not escalate after the confirmation window elapsed")
	}
}

func TestConcurrentDownloader_503WithRetryAfterTreatedAsThrottle(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(128 * utils.KiB)

	var count atomic.Int64

	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			n := count.Add(1)
			if n == 1 {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
			w.WriteHeader(http.StatusPartialContent)
			buf := make([]byte, fileSize)
			_, _ = w.Write(buf)
		}),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "503_test.bin")
	state := progress.New("503-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            3,
		MinChunkSize:              fileSize,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("503-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), mirrors, nil, destPath, fileSize)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if elapsed < 1500*time.Millisecond {
		t.Errorf("Expected backoff after 503+Retry-After, but completed in %v", elapsed)
	}
}

func TestConcurrentDownloader_Persistent429ExhaustsBudget(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(64 * utils.KiB)

	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		}),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "persistent429_test.bin")
	state := progress.New("persistent429-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            3,
		MinChunkSize:              fileSize,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("persistent429-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), mirrors, nil, destPath, fileSize)
	if err == nil {
		t.Fatal("expected download to fail after exhausting rate-limit budget")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate-limit error, got: %v", err)
	}
}

func TestConcurrentDownloader_Bare503IsGeneric(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(128 * utils.KiB)

	var count atomic.Int64

	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			n := count.Add(1)
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
			w.WriteHeader(http.StatusPartialContent)
			buf := make([]byte, fileSize)
			_, _ = w.Write(buf)
		}),
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "bare503_test.bin")
	state := progress.New("bare503-test", fileSize)

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 1,
		MaxTaskRetries:            3,
		MinChunkSize:              fileSize,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("bare503-id", nil, state, runtime)
	downloader.hostLimiter = transport.NewHostRateLimiter()

	mirrors := []string{}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if f, err := os.Create(destPath + ".surge"); err == nil {
		_ = f.Close()
	}

	err := downloader.Download(ctx, server.URL(), mirrors, nil, destPath, fileSize)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
}
