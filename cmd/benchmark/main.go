//go:build linux

// Command benchmark runs Surge's repeatable, Linux-only research benchmark.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

const (
	schemaVersion = 1
	defaultSize   = int64(512 << 20)
	workerCount   = 8
	byteLatency   = 60 * time.Nanosecond
)

type scenario struct {
	Name                 string `json:"name"`
	FileSizeBytes        int64  `json:"file_size_bytes"`
	Workers              int    `json:"workers"`
	ServerDelayPerByteNS int64  `json:"server_delay_per_byte_ns"`
	Transport            string `json:"transport"`
	RequestHedging       bool   `json:"request_hedging"`
}

type environment struct {
	Timestamp       time.Time `json:"timestamp"`
	Commit          string    `json:"commit"`
	Dirty           bool      `json:"dirty"`
	GoVersion       string    `json:"go_version"`
	GOOS            string    `json:"goos"`
	GOARCH          string    `json:"goarch"`
	Kernel          string    `json:"kernel"`
	CPU             string    `json:"cpu"`
	LogicalCPUs     int       `json:"logical_cpus"`
	GOMAXPROCS      int       `json:"gomaxprocs"`
	Instrumentation string    `json:"instrumentation"`
}

type processMetrics struct {
	CPUUserNS              int64  `json:"cpu_user_ns"`
	CPUSystemNS            int64  `json:"cpu_system_ns"`
	MaxRSSBytes            uint64 `json:"max_rss_bytes"`
	PeakHeapAllocBytes     uint64 `json:"peak_heap_alloc_bytes"`
	PeakHeapInuseBytes     uint64 `json:"peak_heap_inuse_bytes"`
	PeakSysBytes           uint64 `json:"peak_go_sys_bytes"`
	PeakStackInuseBytes    uint64 `json:"peak_stack_inuse_bytes"`
	PeakGoroutines         int    `json:"peak_goroutines"`
	PeakThreads            int64  `json:"peak_threads"`
	PeakFDs                int    `json:"peak_open_fds"`
	TotalAllocBytes        uint64 `json:"total_alloc_bytes"`
	Mallocs                uint64 `json:"mallocs"`
	Frees                  uint64 `json:"frees"`
	GCs                    uint32 `json:"gc_cycles"`
	GCPauseNS              uint64 `json:"gc_pause_ns"`
	VoluntaryContextSwitch int64  `json:"voluntary_context_switches"`
	ForcedContextSwitch    int64  `json:"forced_context_switches"`
	ReadBytes              uint64 `json:"storage_read_bytes"`
	WriteBytes             uint64 `json:"storage_write_bytes"`
}

type serverMetrics struct {
	Requests       int64 `json:"requests"`
	RangeRequests  int64 `json:"range_requests"`
	FullRequests   int64 `json:"full_requests"`
	FailedRequests int64 `json:"failed_requests"`
	BytesServed    int64 `json:"bytes_served"`
	PeakRequests   int64 `json:"peak_concurrent_requests"`
	TCPConnections int64 `json:"tcp_handshakes"`
}

type runResult struct {
	Index              int            `json:"index"`
	ElapsedNS          int64          `json:"elapsed_ns"`
	ThroughputBytesSec float64        `json:"throughput_bytes_per_sec"`
	Process            processMetrics `json:"process"`
	Server             serverMetrics  `json:"server"`
}

type summary struct {
	MedianElapsedNS          int64   `json:"median_elapsed_ns"`
	MedianThroughputBytesSec float64 `json:"median_throughput_bytes_per_sec"`
	ThroughputCVPercent      float64 `json:"throughput_cv_percent"`
	PeakRSSBytes             uint64  `json:"peak_rss_bytes"`
	PeakHeapAllocBytes       uint64  `json:"peak_heap_alloc_bytes"`
	PeakGoroutines           int     `json:"peak_goroutines"`
	MedianTotalAllocBytes    uint64  `json:"median_total_alloc_bytes"`
}

type comparison struct {
	Baseline                string  `json:"baseline"`
	ThroughputChangePercent float64 `json:"throughput_change_percent"`
	ElapsedChangePercent    float64 `json:"elapsed_change_percent"`
	PeakRSSChangePercent    float64 `json:"peak_rss_change_percent"`
	AllocationChangePercent float64 `json:"allocation_change_percent"`
}

type report struct {
	SchemaVersion int               `json:"schema_version"`
	Environment   environment       `json:"environment"`
	Scenario      scenario          `json:"scenario"`
	WarmupRuns    int               `json:"warmup_runs"`
	Runs          []runResult       `json:"runs"`
	Summary       summary           `json:"summary"`
	NetworkScope  string            `json:"network_counter_scope"`
	NetworkDelta  map[string]uint64 `json:"linux_network_counter_delta"`
	Comparison    *comparison       `json:"comparison,omitempty"`
	Artifacts     []string          `json:"artifacts,omitempty"`
}

type sample struct {
	heapAlloc, heapInuse, sys, stack, rss uint64
	goRoutines, fds                       int
	threads                               int64
}

func main() {
	var output, baseline, reportName string
	var runs, warmups, sizeMiB int
	var profiles, disableRequestHedging bool
	flag.StringVar(&output, "output", "benchmark-results/latest", "artifact directory")
	flag.StringVar(&baseline, "baseline", "", "prior report.json to compare")
	flag.StringVar(&reportName, "report", "", "report filename (defaults to report.json or diagnostic.json)")
	flag.IntVar(&runs, "runs", 5, "measured runs")
	flag.IntVar(&warmups, "warmup", 1, "warm-up runs")
	flag.IntVar(&sizeMiB, "size-mib", int(defaultSize>>20), "transfer size in MiB")
	flag.BoolVar(&profiles, "profiles", false, "capture diagnostic Go profiles")
	flag.BoolVar(&disableRequestHedging, "disable-request-hedging", false, "disable late duplicate range requests")
	flag.Parse()
	if runs < 1 || warmups < 0 || runs > 20 || warmups > 5 || sizeMiB < 16 || sizeMiB > 2048 {
		fatalf("runs must be 1..20, warmup 0..5, and size-mib 16..2048")
	}
	if reportName != "" && (filepath.Base(reportName) != reportName || !strings.HasSuffix(reportName, ".json")) {
		fatalf("report must be a .json filename without a directory")
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "surge-benchmark-*")
	if err != nil {
		fatalf("create work directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	store.Configure(filepath.Join(tmpDir, "surge.db"))
	defer store.CloseDB()

	transferSize := int64(sizeMiB) << 20
	s := scenario{Name: fmt.Sprintf("loopback-range-%dm-8w", sizeMiB), FileSizeBytes: transferSize, Workers: workerCount, ServerDelayPerByteNS: int64(byteLatency), Transport: "HTTP/1.1 loopback", RequestHedging: !disableRequestHedging}
	for i := 0; i < warmups; i++ {
		fmt.Fprintf(os.Stderr, "warm-up %d/%d\n", i+1, warmups)
		if _, err := runOnce(tmpDir, -1, transferSize, disableRequestHedging); err != nil {
			fatalf("warm-up: %v", err)
		}
	}

	artifacts := []string(nil)
	var stopProfile func() error
	if profiles {
		stopProfile, artifacts, err = startProfiles(output)
		if err != nil {
			fatalf("start profiles: %v", err)
		}
	}

	netBefore := readNetworkCounters()
	results := make([]runResult, 0, runs)
	for i := 0; i < runs; i++ {
		fmt.Fprintf(os.Stderr, "measured run %d/%d\n", i+1, runs)
		result, err := runOnce(tmpDir, i+1, transferSize, disableRequestHedging)
		if err != nil {
			fatalf("run %d: %v", i+1, err)
		}
		results = append(results, result)
	}
	netAfter := readNetworkCounters()
	if stopProfile != nil {
		if err := stopProfile(); err != nil {
			fatalf("write profiles: %v", err)
		}
	}

	rep := report{
		SchemaVersion: schemaVersion,
		Environment:   inspectEnvironment(profiles),
		Scenario:      s,
		WarmupRuns:    warmups,
		Runs:          results,
		Summary:       summarize(results),
		NetworkScope:  "system-wide /proc counters sampled around measured runs; unrelated host traffic may contribute",
		NetworkDelta:  counterDelta(netBefore, netAfter),
		Artifacts:     artifacts,
	}
	if baseline != "" {
		cmp, err := compareBaseline(baseline, s, rep.Summary)
		if err != nil {
			fatalf("compare baseline: %v", err)
		}
		rep.Comparison = cmp
	}

	if reportName == "" {
		reportName = "report.json"
		if profiles {
			reportName = "diagnostic.json"
		}
	}
	path := filepath.Join(output, reportName)
	if err := writeJSONAtomic(path, rep); err != nil {
		fatalf("write report: %v", err)
	}
	printSummary(path, rep)
}

func runOnce(tmpDir string, index int, transferSize int64, disableRequestHedging bool) (runResult, error) {
	server := testutil.NewStreamingMockServer(transferSize,
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(byteLatency),
	)
	defer server.Close()

	name := fmt.Sprintf("run-%d.bin", index)
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
	runtimeCfg.Workers = workerCount
	runtimeCfg.MaxConnectionsPerDownload = workerCount
	if disableRequestHedging {
		runtimeCfg.DialHedgeCount = 0
	}
	state := progress.New(fmt.Sprintf("benchmark-%d", index), transferSize)
	cfg := types.DownloadRecord{
		ID: fmt.Sprintf("benchmark-%d", index), URL: server.URL(), Filename: name,
		OutputPath: tmpDir, TotalSize: transferSize, SupportsRange: true,
		ProgressState: state, Runtime: runtimeCfg,
	}

	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	usageBefore := getRusage()
	ioBefore := readProcIO()
	peak := currentSample()
	done := make(chan struct{})
	samples := make(chan sample, 1)
	go sampleProcess(done, samples, peak)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	start := time.Now()
	err = scheduler.RunDownload(ctx, &cfg)
	elapsed := time.Since(start)
	cancel()
	close(done)
	peak = <-samples
	runtime.ReadMemStats(&memAfter)
	usageAfter := getRusage()
	ioAfter := readProcIO()
	if err != nil {
		return runResult{}, err
	}
	if got := state.Bytes.Downloaded.Load(); got != transferSize {
		return runResult{}, fmt.Errorf("downloaded %d bytes, want %d", got, transferSize)
	}

	stats := server.Stats()
	return runResult{
		Index: index, ElapsedNS: elapsed.Nanoseconds(), ThroughputBytesSec: float64(transferSize) / elapsed.Seconds(),
		Process: processMetrics{
			CPUUserNS: usageAfter.userNS - usageBefore.userNS, CPUSystemNS: usageAfter.systemNS - usageBefore.systemNS,
			MaxRSSBytes: peak.rss, PeakHeapAllocBytes: peak.heapAlloc, PeakHeapInuseBytes: peak.heapInuse,
			PeakSysBytes: peak.sys, PeakStackInuseBytes: peak.stack, PeakGoroutines: peak.goRoutines,
			PeakThreads: peak.threads, PeakFDs: peak.fds, TotalAllocBytes: memAfter.TotalAlloc - memBefore.TotalAlloc,
			Mallocs: memAfter.Mallocs - memBefore.Mallocs, Frees: memAfter.Frees - memBefore.Frees,
			GCs: memAfter.NumGC - memBefore.NumGC, GCPauseNS: memAfter.PauseTotalNs - memBefore.PauseTotalNs,
			VoluntaryContextSwitch: usageAfter.voluntary - usageBefore.voluntary,
			ForcedContextSwitch:    usageAfter.forced - usageBefore.forced,
			ReadBytes:              delta(ioBefore["read_bytes"], ioAfter["read_bytes"]),
			WriteBytes:             delta(ioBefore["write_bytes"], ioAfter["write_bytes"]),
		},
		Server: serverMetrics{Requests: stats.TotalRequests, RangeRequests: stats.RangeRequests, FullRequests: stats.FullRequests,
			FailedRequests: stats.FailedRequests, BytesServed: stats.BytesServed, PeakRequests: stats.PeakRequests, TCPConnections: stats.TCPConnections},
	}, nil
}

func sampleProcess(done <-chan struct{}, result chan<- sample, peak sample) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			peak = mergePeak(peak, currentSample())
		case <-done:
			result <- mergePeak(peak, currentSample())
			return
		}
	}
}

func currentSample() sample {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	status := readProcStatus()
	fds, _ := os.ReadDir("/proc/self/fd")
	return sample{heapAlloc: m.HeapAlloc, heapInuse: m.HeapInuse, sys: m.Sys, stack: m.StackInuse,
		rss: status["VmRSS"] * 1024, goRoutines: runtime.NumGoroutine(), threads: int64(status["Threads"]), fds: len(fds)}
}

func mergePeak(a, b sample) sample {
	a.heapAlloc = max(a.heapAlloc, b.heapAlloc)
	a.heapInuse = max(a.heapInuse, b.heapInuse)
	a.sys = max(a.sys, b.sys)
	a.stack = max(a.stack, b.stack)
	a.rss = max(a.rss, b.rss)
	a.goRoutines = max(a.goRoutines, b.goRoutines)
	a.threads = max(a.threads, b.threads)
	a.fds = max(a.fds, b.fds)
	return a
}

type usage struct{ userNS, systemNS, voluntary, forced int64 }

func getRusage() usage {
	var r syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &r) != nil {
		return usage{}
	}
	return usage{timevalNS(r.Utime), timevalNS(r.Stime), r.Nvcsw, r.Nivcsw}
}

func timevalNS(t syscall.Timeval) int64 {
	return t.Sec*int64(time.Second) + t.Usec*int64(time.Microsecond)
}

func readProcStatus() map[string]uint64 {
	return readKeyValues("/proc/self/status", func(line string) (string, uint64, bool) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", 0, false
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		return strings.TrimSuffix(fields[0], ":"), v, err == nil
	})
}

func readProcIO() map[string]uint64 {
	return readKeyValues("/proc/self/io", func(line string) (string, uint64, bool) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", 0, false
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		return strings.TrimSuffix(fields[0], ":"), v, err == nil
	})
}

func readKeyValues(path string, parse func(string) (string, uint64, bool)) map[string]uint64 {
	values := make(map[string]uint64)
	f, err := os.Open(path)
	if err != nil {
		return values
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if k, v, ok := parse(scanner.Text()); ok {
			values[k] = v
		}
	}
	return values
}

func readNetworkCounters() map[string]uint64 {
	result := make(map[string]uint64)
	for _, path := range []string{"/proc/net/snmp", "/proc/net/netstat"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for i := 0; i+1 < len(lines); i += 2 {
			h, v := strings.Fields(lines[i]), strings.Fields(lines[i+1])
			if len(h) != len(v) || len(h) < 2 || h[0] != v[0] {
				continue
			}
			section := strings.TrimSuffix(h[0], ":")
			for j := 1; j < len(h); j++ {
				n, err := strconv.ParseUint(v[j], 10, 64)
				if err == nil {
					result[section+"."+h[j]] = n
				}
			}
		}
	}
	return result
}

func counterDelta(before, after map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64)
	for key, end := range after {
		if start, ok := before[key]; ok && end >= start && end != start {
			out[key] = end - start
		}
	}
	return out
}

func delta(start, end uint64) uint64 {
	if end < start {
		return 0
	}
	return end - start
}

func summarize(runs []runResult) summary {
	elapsed := make([]int64, len(runs))
	throughput := make([]float64, len(runs))
	alloc := make([]uint64, len(runs))
	var peakRSS, peakHeap uint64
	var peakGo int
	var mean float64
	for i, r := range runs {
		elapsed[i], throughput[i], alloc[i] = r.ElapsedNS, r.ThroughputBytesSec, r.Process.TotalAllocBytes
		peakRSS = max(peakRSS, r.Process.MaxRSSBytes)
		peakHeap = max(peakHeap, r.Process.PeakHeapAllocBytes)
		peakGo = max(peakGo, r.Process.PeakGoroutines)
		mean += r.ThroughputBytesSec
	}
	sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
	sort.Float64s(throughput)
	sort.Slice(alloc, func(i, j int) bool { return alloc[i] < alloc[j] })
	mean /= float64(len(runs))
	var variance float64
	for _, r := range runs {
		variance += math.Pow(r.ThroughputBytesSec-mean, 2)
	}
	variance /= float64(len(runs))
	return summary{MedianElapsedNS: elapsed[len(elapsed)/2], MedianThroughputBytesSec: throughput[len(throughput)/2],
		ThroughputCVPercent: math.Sqrt(variance) / mean * 100, PeakRSSBytes: peakRSS, PeakHeapAllocBytes: peakHeap,
		PeakGoroutines: peakGo, MedianTotalAllocBytes: alloc[len(alloc)/2]}
}

func startProfiles(dir string) (func() error, []string, error) {
	cpuPath := filepath.Join(dir, "cpu.pprof")
	cpu, err := os.Create(cpuPath)
	if err != nil {
		return nil, nil, err
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		cpu.Close()
		return nil, nil, err
	}
	tracePath := filepath.Join(dir, "runtime.trace")
	traceFile, err := os.Create(tracePath)
	if err != nil {
		pprof.StopCPUProfile()
		cpu.Close()
		return nil, nil, err
	}
	if err := trace.Start(traceFile); err != nil {
		pprof.StopCPUProfile()
		cpu.Close()
		traceFile.Close()
		return nil, nil, err
	}
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
	names := []string{"cpu.pprof", "runtime.trace", "heap.pprof", "allocs.pprof", "goroutine.pprof", "block.pprof", "mutex.pprof"}
	return func() error {
		trace.Stop()
		traceErr := traceFile.Close()
		pprof.StopCPUProfile()
		cpuErr := cpu.Close()
		for _, name := range names[2:] {
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				return err
			}
			if err := pprof.Lookup(strings.TrimSuffix(name, ".pprof")).WriteTo(f, 0); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
		return errors.Join(traceErr, cpuErr)
	}, names, nil
}

func inspectEnvironment(profiles bool) environment {
	commit, dirty := "unknown", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				commit = s.Value
			}
			if s.Key == "vcs.modified" {
				dirty = s.Value == "true"
			}
		}
	}
	kernel, _ := exec.Command("uname", "-sr").Output()
	instrumentation := "measurement"
	if profiles {
		instrumentation = "diagnostic (CPU, trace, heap, alloc, goroutine, block, mutex profiling enabled)"
	}
	return environment{Timestamp: time.Now().UTC(), Commit: commit, Dirty: dirty, GoVersion: runtime.Version(), GOOS: runtime.GOOS,
		GOARCH: runtime.GOARCH, Kernel: strings.TrimSpace(string(kernel)), CPU: cpuModel(), LogicalCPUs: runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0), Instrumentation: instrumentation}
}

func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if k, v, ok := strings.Cut(s.Text(), ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return "unknown"
}

func compareBaseline(path string, currentScenario scenario, current summary) (*comparison, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var old report
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, err
	}
	if old.SchemaVersion != schemaVersion || old.Scenario != currentScenario {
		return nil, fmt.Errorf("baseline schema or scenario does not match")
	}
	return &comparison{Baseline: path,
		ThroughputChangePercent: percent(old.Summary.MedianThroughputBytesSec, current.MedianThroughputBytesSec),
		ElapsedChangePercent:    percent(float64(old.Summary.MedianElapsedNS), float64(current.MedianElapsedNS)),
		PeakRSSChangePercent:    percent(float64(old.Summary.PeakRSSBytes), float64(current.PeakRSSBytes)),
		AllocationChangePercent: percent(float64(old.Summary.MedianTotalAllocBytes), float64(current.MedianTotalAllocBytes))}, nil
}

func percent(old, current float64) float64 {
	if old == 0 {
		return 0
	}
	return (current - old) / old * 100
}

func writeJSONAtomic(path string, value any) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".report-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func printSummary(path string, rep report) {
	fmt.Printf("report: %s\n", path)
	fmt.Printf("median: %.2f MiB/s in %.3fs (CV %.2f%%)\n", rep.Summary.MedianThroughputBytesSec/(1<<20), float64(rep.Summary.MedianElapsedNS)/float64(time.Second), rep.Summary.ThroughputCVPercent)
	fmt.Printf("peaks: RSS %.1f MiB, heap %.1f MiB, goroutines %d\n", float64(rep.Summary.PeakRSSBytes)/(1<<20), float64(rep.Summary.PeakHeapAllocBytes)/(1<<20), rep.Summary.PeakGoroutines)
	if rep.Comparison != nil {
		fmt.Printf("vs baseline: throughput %+.2f%%, elapsed %+.2f%%, RSS %+.2f%%, allocations %+.2f%%\n", rep.Comparison.ThroughputChangePercent, rep.Comparison.ElapsedChangePercent, rep.Comparison.PeakRSSChangePercent, rep.Comparison.AllocationChangePercent)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "benchmark: "+format+"\n", args...)
	os.Exit(1)
}
