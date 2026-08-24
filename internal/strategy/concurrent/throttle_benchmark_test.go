package concurrent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

const (
	benchmarkWorkers   = 8
	benchmarkChunkSize = int64(utils.MiB)
	benchmarkFileSize  = benchmarkWorkers * benchmarkChunkSize
	// ponytail: Recovery is compressed for a five-second developer benchmark;
	// use a real-server soak before changing production timing defaults.
	benchmarkRecoveryWindow = 50 * time.Millisecond
)

type throttleWorkload struct {
	name             string
	accepted         int
	initialThrottles int64
	responseDelay    time.Duration
	slowRangeStart   int64
	slowBlockDelay   time.Duration
}

type throttleMetrics struct {
	requests  int64
	throttled int64
	peak      int
}

type throttleServer struct {
	*httptest.Server
	work throttleWorkload

	mu         sync.Mutex
	requests   int64
	throttled  int64
	active     int
	peakActive int
}

func BenchmarkThrottle(b *testing.B) {
	tmpDir := b.TempDir()
	store.CloseDB()
	store.Configure(filepath.Join(tmpDir, "surge.db"))
	b.Cleanup(store.CloseDB)

	workloads := []throttleWorkload{
		{name: "storm", accepted: 2, responseDelay: 50 * time.Millisecond},
		{name: "recovery", initialThrottles: benchmarkWorkers, responseDelay: 100 * time.Millisecond},
		{
			name:           "slow-tail",
			responseDelay:  5 * time.Millisecond,
			slowRangeStart: 7 * benchmarkChunkSize,
			slowBlockDelay: 20 * time.Millisecond,
		},
	}
	for _, work := range workloads {
		for _, adaptive := range []bool{false, true} {
			policy := "fixed"
			if adaptive {
				policy = "adaptive"
			}
			b.Run(work.name+"/"+policy, func(b *testing.B) {
				benchmarkThrottlePolicy(b, tmpDir, work, adaptive)
			})
		}
	}
}

func benchmarkThrottlePolicy(b *testing.B, tmpDir string, work throttleWorkload, adaptive bool) {
	var totals throttleMetrics
	b.SetBytes(benchmarkFileSize)
	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()

	for i := 0; i < b.N; i++ {
		server := newThrottleServer(work)
		destPath := filepath.Join(tmpDir, fmt.Sprintf("%s-%t-%d.bin", work.name, adaptive, i))
		file, err := os.Create(destPath + types.IncompleteSuffix)
		if err != nil {
			b.Fatal(err)
		}
		if err := file.Close(); err != nil {
			b.Fatal(err)
		}

		state := progress.New(fmt.Sprintf("benchmark-%s-%t-%d", work.name, adaptive, i), benchmarkFileSize)
		runtime := types.DefaultRuntimeConfig()
		runtime.Workers = benchmarkWorkers
		runtime.MaxConnectionsPerDownload = benchmarkWorkers
		runtime.MinChunkSize = benchmarkChunkSize
		runtime.DialHedgeCount = 0
		runtime.AdaptiveConcurrencyInterval = 0
		if adaptive {
			runtime.AdaptiveConcurrencyInterval = benchmarkRecoveryWindow
		}
		downloader := NewConcurrentDownloader(state.ID, nil, state, runtime)
		downloader.hostLimiter = transport.NewHostRateLimiter()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		b.StartTimer()
		err = downloader.Download(ctx, server.URL, nil, nil, destPath, benchmarkFileSize)
		b.StopTimer()
		cancel()
		server.Close()
		if err != nil {
			b.Fatal(err)
		}
		if got := state.Bytes.VerifiedProgress.Load(); got != benchmarkFileSize {
			b.Fatalf("verified %d bytes, want %d", got, benchmarkFileSize)
		}
		metrics := server.metrics()
		if metrics.peak > benchmarkWorkers {
			b.Fatalf("peak requests = %d, exceeds worker count %d", metrics.peak, benchmarkWorkers)
		}
		totals.requests += metrics.requests
		totals.throttled += metrics.throttled
		totals.peak += metrics.peak
	}

	b.ReportMetric(float64(totals.throttled)/float64(b.N), "throttles/op")
	b.ReportMetric(float64(totals.requests)/float64(b.N*benchmarkWorkers), "requests/chunk")
	b.ReportMetric(float64(totals.peak)/float64(b.N), "peak-requests")
}

func newThrottleServer(work throttleWorkload) *throttleServer {
	server := &throttleServer{work: work}
	server.Server = httptest.NewServer(http.HandlerFunc(server.handle))
	return server
}

func (s *throttleServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests++
	throttle := s.requests <= s.work.initialThrottles || s.work.accepted > 0 && s.active >= s.work.accepted
	if throttle {
		s.throttled++
		s.mu.Unlock()
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	s.active++
	s.peakActive = max(s.peakActive, s.active)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()

	start, end, ok := parseBenchmarkRange(r.Header.Get("Range"), benchmarkFileSize)
	if !ok {
		http.Error(w, "range required", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	time.Sleep(s.work.responseDelay)
	length := end - start + 1
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, benchmarkFileSize))
	w.WriteHeader(http.StatusPartialContent)

	buf := make([]byte, 64*utils.KiB)
	slowRange := s.work.slowBlockDelay > 0 && start >= s.work.slowRangeStart
	for remaining := length; remaining > 0; {
		if slowRange {
			timer := time.NewTimer(s.work.slowBlockDelay)
			select {
			case <-r.Context().Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
		}
		n, err := w.Write(buf[:min(int64(len(buf)), remaining)])
		remaining -= int64(n)
		if err != nil {
			return
		}
		if slowRange {
			w.(http.Flusher).Flush()
		}
	}
}

func (s *throttleServer) metrics() throttleMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return throttleMetrics{requests: s.requests, throttled: s.throttled, peak: s.peakActive}
}

func parseBenchmarkRange(value string, size int64) (int64, int64, bool) {
	value, ok := strings.CutPrefix(value, "bytes=")
	if !ok {
		return 0, 0, false
	}
	startText, endText, ok := strings.Cut(value, "-")
	if !ok {
		return 0, 0, false
	}
	start, startErr := strconv.ParseInt(startText, 10, 64)
	end, endErr := strconv.ParseInt(endText, 10, 64)
	return start, end, startErr == nil && endErr == nil && start >= 0 && start <= end && end < size
}
