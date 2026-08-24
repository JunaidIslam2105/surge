//go:build linux

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/utils"
)

func TestParseRange(t *testing.T) {
	start, end, ok := parseRange("bytes=4096-8191", 16*utils.KiB)
	if !ok || start != 4096 || end != 8191 {
		t.Fatalf("parseRange = (%d, %d, %v), want (4096, 8191, true)", start, end, ok)
	}
	for _, value := range []string{"", "bytes=8192-4096", "bytes=0-16384", "items=0-1"} {
		if _, _, ok := parseRange(value, 16*utils.KiB); ok {
			t.Fatalf("parseRange(%q) unexpectedly succeeded", value)
		}
	}
}

func TestCompareBaselineRequiresIdenticalWorkload(t *testing.T) {
	work := selectedWorkloads("storm", true, 3*time.Second)[0]
	prior := scenarioReport{Workload: work, Summary: summary{MedianElapsedNS: 100, MedianThroughputBytesSec: 200,
		MedianThrottledRequests: 10, MedianRequestAmplification: 2, MedianRateLimitedZeroProgressNS: 50, MedianFinalTenPercentNS: 25}}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeJSONAtomic(path, report{SchemaVersion: schemaVersion, Scenarios: []scenarioReport{prior}}); err != nil {
		t.Fatal(err)
	}

	current := prior
	current.Summary.MedianElapsedNS = 90
	comparisons, err := compareBaseline(path, []scenarioReport{current})
	if err != nil {
		t.Fatal(err)
	}
	if got := comparisons[work.Name].ElapsedChangePercent; got != -10 {
		t.Fatalf("elapsed change = %v, want -10", got)
	}

	current.Workload.AdaptiveConcurrency = false
	current.Workload.RecoveryWindowNS = int64(5 * time.Second)
	if _, err := compareBaseline(path, []scenarioReport{current}); err != nil {
		t.Fatalf("adaptive policy settings should remain comparable: %v", err)
	}

	current.Workload.FileSizeBytes++
	if _, err := compareBaseline(path, []scenarioReport{current}); err == nil {
		t.Fatal("expected mismatched workload to reject baseline")
	}
}
