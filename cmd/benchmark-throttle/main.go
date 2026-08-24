//go:build linux

// Command benchmark-throttle measures downloader behavior under deterministic
// HTTP throttling. It complements cmd/benchmark's healthy-path measurements.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

const (
	schemaVersion = 1
	workerCount   = 8
	chunkSize     = int64(4 * utils.MiB)
	retryAfter    = "1"
	sampleEvery   = 20 * time.Millisecond
	zeroThreshold = 500 * time.Millisecond
)

type workload struct {
	Name                 string `json:"name"`
	FileSizeBytes        int64  `json:"file_size_bytes"`
	Workers              int    `json:"workers"`
	ChunkSizeBytes       int64  `json:"chunk_size_bytes"`
	ServerDelayPerByteNS int64  `json:"server_delay_per_byte_ns"`
	AcceptedConcurrency  int    `json:"accepted_concurrency,omitempty"`
	InitialThrottleCount int    `json:"initial_throttle_count,omitempty"`
	RetryAfter           string `json:"retry_after"`
	AdaptiveConcurrency  bool   `json:"adaptive_concurrency"`
	RecoveryWindowNS     int64  `json:"recovery_window_ns"`
}

type environment struct {
	Timestamp   time.Time `json:"timestamp"`
	Commit      string    `json:"commit"`
	Dirty       bool      `json:"dirty"`
	GoVersion   string    `json:"go_version"`
	Kernel      string    `json:"kernel"`
	CPU         string    `json:"cpu"`
	LogicalCPUs int       `json:"logical_cpus"`
}

type serverMetrics struct {
	Requests                   int64   `json:"requests"`
	ThrottledRequests          int64   `json:"throttled_requests"`
	SuccessfulRequests         int64   `json:"successful_requests"`
	BytesServed                int64   `json:"bytes_served"`
	PeakAcceptedConcurrency    int     `json:"peak_accepted_concurrency"`
	RecoveryPeakConcurrency    int     `json:"recovery_peak_concurrency"`
	TCPConnections             int     `json:"tcp_connections"`
	RequestAmplification       float64 `json:"request_amplification"`
	LastThrottleToCompletionNS int64   `json:"last_throttle_to_completion_ns"`
}

type progressMetrics struct {
	ZeroProgressNS            int64 `json:"zero_progress_ns"`
	RateLimitedZeroProgressNS int64 `json:"rate_limited_zero_progress_ns"`
	LongestZeroProgressNS     int64 `json:"longest_zero_progress_ns"`
	FinalTenPercentNS         int64 `json:"final_ten_percent_ns"`
}

type runResult struct {
	Index              int             `json:"index"`
	ElapsedNS          int64           `json:"elapsed_ns"`
	ThroughputBytesSec float64         `json:"throughput_bytes_per_sec"`
	Server             serverMetrics   `json:"server"`
	Progress           progressMetrics `json:"progress"`
}

type summary struct {
	MedianElapsedNS                  int64   `json:"median_elapsed_ns"`
	MedianThroughputBytesSec         float64 `json:"median_throughput_bytes_per_sec"`
	MedianThrottledRequests          int64   `json:"median_throttled_requests"`
	MedianRequestAmplification       float64 `json:"median_request_amplification"`
	MedianRateLimitedZeroProgressNS  int64   `json:"median_rate_limited_zero_progress_ns"`
	MedianLongestZeroProgressNS      int64   `json:"median_longest_zero_progress_ns"`
	MedianFinalTenPercentNS          int64   `json:"median_final_ten_percent_ns"`
	MedianLastThrottleToCompletionNS int64   `json:"median_last_throttle_to_completion_ns"`
	PeakAcceptedConcurrency          int     `json:"peak_accepted_concurrency"`
	PeakRecoveryConcurrency          int     `json:"peak_recovery_concurrency"`
}

type scenarioReport struct {
	Workload workload    `json:"workload"`
	Runs     []runResult `json:"runs"`
	Summary  summary     `json:"summary"`
}

type comparison struct {
	ElapsedChangePercent                 float64 `json:"elapsed_change_percent"`
	ThroughputChangePercent              float64 `json:"throughput_change_percent"`
	ThrottledRequestChangePercent        float64 `json:"throttled_request_change_percent"`
	RequestAmplificationChangePercent    float64 `json:"request_amplification_change_percent"`
	RateLimitedZeroProgressChangePercent float64 `json:"rate_limited_zero_progress_change_percent"`
	FinalTenPercentChangePercent         float64 `json:"final_ten_percent_change_percent"`
}

type report struct {
	SchemaVersion int                   `json:"schema_version"`
	Environment   environment           `json:"environment"`
	WarmupRuns    int                   `json:"warmup_runs"`
	Scenarios     []scenarioReport      `json:"scenarios"`
	Baseline      string                `json:"baseline,omitempty"`
	Comparison    map[string]comparison `json:"comparison,omitempty"`
}

type throttleServer struct {
	server *httptest.Server
	work   workload

	mu           sync.Mutex
	requests     int64
	throttled    int64
	successful   int64
	bytesServed  int64
	accepted     int
	peakAccepted int
	recoveryPeak int
	lastThrottle time.Time
	connections  map[string]struct{}
}

func main() {
	var output, baseline, selected string
	var runs, warmups int
	var adaptiveConcurrency bool
	var recoveryWindow time.Duration
	flag.StringVar(&output, "output", "benchmark-results/throttle-latest", "artifact directory")
	flag.StringVar(&baseline, "baseline", "", "prior throttle-report.json to compare")
	flag.StringVar(&selected, "scenario", "all", "scenario: all, storm, or recovery")
	flag.IntVar(&runs, "runs", 3, "measured runs per scenario")
	flag.IntVar(&warmups, "warmup", 0, "warm-up runs per scenario")
	flag.BoolVar(&adaptiveConcurrency, "adaptive-concurrency", true, "enable adaptive per-download concurrency")
	flag.DurationVar(&recoveryWindow, "recovery-window", types.DefaultAdaptiveConcurrencyRecoveryWindow, "healthy window between concurrency recovery steps")
	flag.Parse()
	if runs < 1 || runs > 10 || warmups < 0 || warmups > 2 {
		fatalf("runs must be 1..10 and warmup 0..2")
	}
	if selected != "all" && selected != "storm" && selected != "recovery" {
		fatalf("scenario must be all, storm, or recovery")
	}
	if recoveryWindow < time.Second || recoveryWindow > time.Minute {
		fatalf("recovery-window must be between 1s and 60s")
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "surge-throttle-benchmark-*")
	if err != nil {
		fatalf("create work directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	store.Configure(filepath.Join(tmpDir, "surge.db"))
	defer store.CloseDB()

	workloads := selectedWorkloads(selected, adaptiveConcurrency, recoveryWindow)
	rep := report{SchemaVersion: schemaVersion, Environment: inspectEnvironment(), WarmupRuns: warmups}
	for _, work := range workloads {
		for i := 0; i < warmups; i++ {
			fmt.Fprintf(os.Stderr, "%s warm-up %d/%d\n", work.Name, i+1, warmups)
			if _, err := runOnce(tmpDir, work, -(i + 1)); err != nil {
				fatalf("%s warm-up: %v", work.Name, err)
			}
		}
		results := make([]runResult, 0, runs)
		for i := 0; i < runs; i++ {
			fmt.Fprintf(os.Stderr, "%s measured run %d/%d\n", work.Name, i+1, runs)
			result, err := runOnce(tmpDir, work, i+1)
			if err != nil {
				fatalf("%s run %d: %v", work.Name, i+1, err)
			}
			results = append(results, result)
		}
		rep.Scenarios = append(rep.Scenarios, scenarioReport{Workload: work, Runs: results, Summary: summarize(results)})
	}

	if baseline != "" {
		comparisons, err := compareBaseline(baseline, rep.Scenarios)
		if err != nil {
			fatalf("compare baseline: %v", err)
		}
		rep.Baseline = baseline
		rep.Comparison = comparisons
	}
	path := filepath.Join(output, "throttle-report.json")
	if err := writeJSONAtomic(path, rep); err != nil {
		fatalf("write report: %v", err)
	}
	printReport(path, rep)
}

func selectedWorkloads(selected string, adaptiveConcurrency bool, recoveryWindow time.Duration) []workload {
	storm := workload{Name: "persistent-overload-128m-8w-2cap", FileSizeBytes: 128 * utils.MiB, Workers: workerCount,
		ChunkSizeBytes: chunkSize, ServerDelayPerByteNS: int64(250 * time.Nanosecond), AcceptedConcurrency: 2, RetryAfter: retryAfter,
		AdaptiveConcurrency: adaptiveConcurrency, RecoveryWindowNS: recoveryWindow.Nanoseconds()}
	recovery := workload{Name: "burst-recovery-160m-8w", FileSizeBytes: 160 * utils.MiB, Workers: workerCount,
		ChunkSizeBytes: chunkSize, ServerDelayPerByteNS: int64(time.Microsecond), InitialThrottleCount: workerCount, RetryAfter: retryAfter,
		AdaptiveConcurrency: adaptiveConcurrency, RecoveryWindowNS: recoveryWindow.Nanoseconds()}
	switch selected {
	case "storm":
		return []workload{storm}
	case "recovery":
		return []workload{recovery}
	default:
		return []workload{storm, recovery}
	}
}

func runOnce(tmpDir string, work workload, index int) (runResult, error) {
	server := newThrottleServer(work)
	defer server.Close()

	name := fmt.Sprintf("%s-%d.bin", work.Name, index)
	dest := filepath.Join(tmpDir, name)
	f, err := os.Create(dest + types.IncompleteSuffix)
	if err != nil {
		return runResult{}, err
	}
	if err := f.Close(); err != nil {
		return runResult{}, err
	}
	defer os.Remove(dest + types.IncompleteSuffix)

	runtimeCfg := types.DefaultRuntimeConfig()
	runtimeCfg.Workers = work.Workers
	runtimeCfg.MaxConnectionsPerDownload = work.Workers
	runtimeCfg.MinChunkSize = work.ChunkSizeBytes
	runtimeCfg.SequentialDownload = true
	runtimeCfg.DialHedgeCount = 0
	runtimeCfg.DisableAdaptiveConcurrency = !work.AdaptiveConcurrency
	runtimeCfg.AdaptiveConcurrencyRecoveryWindow = time.Duration(work.RecoveryWindowNS)
	state := progress.New(fmt.Sprintf("throttle-benchmark-%s-%d", work.Name, index), work.FileSizeBytes)
	cfg := types.DownloadRecord{ID: fmt.Sprintf("throttle-benchmark-%s-%d", work.Name, index), URL: server.URL(), Filename: name,
		OutputPath: tmpDir, TotalSize: work.FileSizeBytes, SupportsRange: true, ProgressState: state, Runtime: runtimeCfg}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	progressDone := make(chan struct{})
	progressResult := make(chan progressMetrics, 1)
	start := time.Now()
	go sampleProgress(progressDone, progressResult, server, state, work.FileSizeBytes, start)
	err = scheduler.RunDownload(ctx, &cfg)
	elapsed := time.Since(start)
	close(progressDone)
	pm := <-progressResult
	if err != nil {
		return runResult{}, err
	}
	if got := state.Bytes.Downloaded.Load(); got != work.FileSizeBytes {
		return runResult{}, fmt.Errorf("downloaded %d bytes, want %d", got, work.FileSizeBytes)
	}

	stats := server.Stats(time.Now())
	expectedRequests := float64((work.FileSizeBytes + work.ChunkSizeBytes - 1) / work.ChunkSizeBytes)
	stats.RequestAmplification = float64(stats.Requests) / expectedRequests
	return runResult{Index: index, ElapsedNS: elapsed.Nanoseconds(), ThroughputBytesSec: float64(work.FileSizeBytes) / elapsed.Seconds(), Server: stats, Progress: pm}, nil
}

func newThrottleServer(work workload) *throttleServer {
	s := &throttleServer{work: work, connections: make(map[string]struct{})}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *throttleServer) URL() string { return s.server.URL }
func (s *throttleServer) Close()      { s.server.Close() }

func (s *throttleServer) handle(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.mu.Lock()
	s.requests++
	requestNumber := s.requests
	s.connections[r.RemoteAddr] = struct{}{}
	throttle := (s.work.InitialThrottleCount > 0 && requestNumber <= int64(s.work.InitialThrottleCount)) ||
		(s.work.AcceptedConcurrency > 0 && s.accepted >= s.work.AcceptedConcurrency)
	if throttle {
		s.throttled++
		s.lastThrottle = now
		s.mu.Unlock()
		w.Header().Set("Retry-After", s.work.RetryAfter)
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	s.successful++
	s.accepted++
	s.peakAccepted = max(s.peakAccepted, s.accepted)
	if s.work.InitialThrottleCount > 0 && !s.lastThrottle.IsZero() && now.Sub(s.lastThrottle) >= time.Duration(s.work.RecoveryWindowNS) {
		s.recoveryPeak = max(s.recoveryPeak, s.accepted)
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.accepted--
		s.mu.Unlock()
	}()

	start, end, ok := parseRange(r.Header.Get("Range"), s.work.FileSizeBytes)
	if !ok {
		http.Error(w, "range required", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	length := end - start + 1
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, s.work.FileSizeBytes))
	w.WriteHeader(http.StatusPartialContent)

	buf := make([]byte, 64*utils.KiB)
	for written := int64(0); written < length; {
		n := min(int64(len(buf)), length-written)
		count, err := w.Write(buf[:n])
		if count > 0 {
			s.mu.Lock()
			s.bytesServed += int64(count)
			s.mu.Unlock()
			written += int64(count)
		}
		if err != nil {
			return
		}
		delay := time.Duration(count) * time.Duration(s.work.ServerDelayPerByteNS)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
}

func parseRange(value string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(value, "bytes=") {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || start < 0 || end < start || end >= size {
		return 0, 0, false
	}
	return start, end, true
}

func (s *throttleServer) Stats(completed time.Time) serverMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	lastThrottleToCompletion := int64(0)
	if !s.lastThrottle.IsZero() {
		lastThrottleToCompletion = completed.Sub(s.lastThrottle).Nanoseconds()
	}
	return serverMetrics{Requests: s.requests, ThrottledRequests: s.throttled, SuccessfulRequests: s.successful,
		BytesServed: s.bytesServed, PeakAcceptedConcurrency: s.peakAccepted, RecoveryPeakConcurrency: s.recoveryPeak,
		TCPConnections: len(s.connections), LastThrottleToCompletionNS: lastThrottleToCompletion}
}

func (s *throttleServer) BytesServed() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytesServed
}

func sampleProgress(done <-chan struct{}, result chan<- progressMetrics, server *throttleServer, state *progress.DownloadProgress, total int64, started time.Time) {
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()
	lastBytes := server.BytesServed()
	lastSample := started
	var metrics progressMetrics
	var zeroRun time.Duration
	var rateLimitedZeroRun time.Duration
	var tailStart time.Time
	flushZeroRun := func() {
		if zeroRun >= zeroThreshold {
			metrics.ZeroProgressNS += zeroRun.Nanoseconds()
			metrics.RateLimitedZeroProgressNS += rateLimitedZeroRun.Nanoseconds()
			metrics.LongestZeroProgressNS = max(metrics.LongestZeroProgressNS, zeroRun.Nanoseconds())
		}
		zeroRun = 0
		rateLimitedZeroRun = 0
	}
	for {
		select {
		case now := <-ticker.C:
			current := server.BytesServed()
			if tailStart.IsZero() && current >= total*9/10 {
				tailStart = now
			}
			if current == lastBytes && current < total && (current > 0 || state.RateLimited.Load()) {
				d := now.Sub(lastSample)
				zeroRun += d
				if state.RateLimited.Load() {
					rateLimitedZeroRun += d
				}
			} else {
				flushZeroRun()
			}
			lastBytes = current
			lastSample = now
		case <-done:
			flushZeroRun()
			if !tailStart.IsZero() {
				metrics.FinalTenPercentNS = time.Since(tailStart).Nanoseconds()
			}
			result <- metrics
			return
		}
	}
}

func summarize(runs []runResult) summary {
	elapsed := make([]int64, len(runs))
	throughput := make([]float64, len(runs))
	throttles := make([]int64, len(runs))
	amplification := make([]float64, len(runs))
	rateLimitedZero := make([]int64, len(runs))
	longestZero := make([]int64, len(runs))
	tail := make([]int64, len(runs))
	lastThrottle := make([]int64, len(runs))
	var peakAccepted, peakRecovery int
	for i, run := range runs {
		elapsed[i], throughput[i] = run.ElapsedNS, run.ThroughputBytesSec
		throttles[i], amplification[i] = run.Server.ThrottledRequests, run.Server.RequestAmplification
		rateLimitedZero[i], longestZero[i] = run.Progress.RateLimitedZeroProgressNS, run.Progress.LongestZeroProgressNS
		tail[i], lastThrottle[i] = run.Progress.FinalTenPercentNS, run.Server.LastThrottleToCompletionNS
		peakAccepted = max(peakAccepted, run.Server.PeakAcceptedConcurrency)
		peakRecovery = max(peakRecovery, run.Server.RecoveryPeakConcurrency)
	}
	sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
	sort.Float64s(throughput)
	sort.Slice(throttles, func(i, j int) bool { return throttles[i] < throttles[j] })
	sort.Float64s(amplification)
	sort.Slice(rateLimitedZero, func(i, j int) bool { return rateLimitedZero[i] < rateLimitedZero[j] })
	sort.Slice(longestZero, func(i, j int) bool { return longestZero[i] < longestZero[j] })
	sort.Slice(tail, func(i, j int) bool { return tail[i] < tail[j] })
	sort.Slice(lastThrottle, func(i, j int) bool { return lastThrottle[i] < lastThrottle[j] })
	mid := len(runs) / 2
	return summary{MedianElapsedNS: elapsed[mid], MedianThroughputBytesSec: throughput[mid], MedianThrottledRequests: throttles[mid],
		MedianRequestAmplification: amplification[mid], MedianRateLimitedZeroProgressNS: rateLimitedZero[mid],
		MedianLongestZeroProgressNS: longestZero[mid], MedianFinalTenPercentNS: tail[mid], MedianLastThrottleToCompletionNS: lastThrottle[mid],
		PeakAcceptedConcurrency: peakAccepted, PeakRecoveryConcurrency: peakRecovery}
}

func compareBaseline(path string, current []scenarioReport) (map[string]comparison, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var old report
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, err
	}
	if old.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("baseline schema does not match")
	}
	oldByName := make(map[string]scenarioReport, len(old.Scenarios))
	for _, scenario := range old.Scenarios {
		oldByName[scenario.Workload.Name] = scenario
	}
	comparisons := make(map[string]comparison, len(current))
	for _, scenario := range current {
		prior, ok := oldByName[scenario.Workload.Name]
		if !ok || benchmarkShape(prior.Workload) != benchmarkShape(scenario.Workload) {
			return nil, fmt.Errorf("baseline workload %q does not match", scenario.Workload.Name)
		}
		a, b := prior.Summary, scenario.Summary
		comparisons[scenario.Workload.Name] = comparison{ElapsedChangePercent: percent(float64(a.MedianElapsedNS), float64(b.MedianElapsedNS)),
			ThroughputChangePercent:              percent(a.MedianThroughputBytesSec, b.MedianThroughputBytesSec),
			ThrottledRequestChangePercent:        percent(float64(a.MedianThrottledRequests), float64(b.MedianThrottledRequests)),
			RequestAmplificationChangePercent:    percent(a.MedianRequestAmplification, b.MedianRequestAmplification),
			RateLimitedZeroProgressChangePercent: percent(float64(a.MedianRateLimitedZeroProgressNS), float64(b.MedianRateLimitedZeroProgressNS)),
			FinalTenPercentChangePercent:         percent(float64(a.MedianFinalTenPercentNS), float64(b.MedianFinalTenPercentNS))}
	}
	return comparisons, nil
}

func benchmarkShape(work workload) workload {
	work.AdaptiveConcurrency = false
	work.RecoveryWindowNS = 0
	return work
}

func percent(old, current float64) float64 {
	if old == 0 {
		return 0
	}
	return (current - old) / old * 100
}

func inspectEnvironment() environment {
	commit, dirty := "unknown", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				commit = setting.Value
			case "vcs.modified":
				dirty = setting.Value == "true"
			}
		}
	}
	kernel, _ := exec.Command("uname", "-sr").Output()
	cpu := "unknown"
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
				cpu = strings.TrimSpace(value)
				break
			}
		}
	}
	return environment{Timestamp: time.Now().UTC(), Commit: commit, Dirty: dirty, GoVersion: runtime.Version(), Kernel: strings.TrimSpace(string(kernel)), CPU: cpu, LogicalCPUs: runtime.NumCPU()}
}

func writeJSONAtomic(path string, value any) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".throttle-report-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func printReport(path string, rep report) {
	fmt.Printf("report: %s\n", path)
	for _, scenario := range rep.Scenarios {
		s := scenario.Summary
		fmt.Printf("%s: %.2f MiB/s in %.2fs, throttles %d, amplification %.2fx, rate-limited zero %.2fs, tail %.2fs, recovery peak %d\n",
			scenario.Workload.Name, s.MedianThroughputBytesSec/float64(utils.MiB), time.Duration(s.MedianElapsedNS).Seconds(),
			s.MedianThrottledRequests, s.MedianRequestAmplification, time.Duration(s.MedianRateLimitedZeroProgressNS).Seconds(),
			time.Duration(s.MedianFinalTenPercentNS).Seconds(), s.PeakRecoveryConcurrency)
		if cmp, ok := rep.Comparison[scenario.Workload.Name]; ok {
			fmt.Printf("  vs baseline: elapsed %+.2f%%, throughput %+.2f%%, throttles %+.2f%%, amplification %+.2f%%, rate-limited zero %+.2f%%, tail %+.2f%%\n",
				cmp.ElapsedChangePercent, cmp.ThroughputChangePercent, cmp.ThrottledRequestChangePercent,
				cmp.RequestAmplificationChangePercent, cmp.RateLimitedZeroProgressChangePercent, cmp.FinalTenPercentChangePercent)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "benchmark-throttle: "+format+"\n", args...)
	os.Exit(1)
}
