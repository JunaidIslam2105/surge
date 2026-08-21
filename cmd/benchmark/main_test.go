//go:build linux

package main

import "testing"

func TestSummarizeUsesMedianAndPeaks(t *testing.T) {
	runs := []runResult{
		{ElapsedNS: 30, ThroughputBytesSec: 10, Process: processMetrics{MaxRSSBytes: 100, PeakGoroutines: 4, TotalAllocBytes: 30}},
		{ElapsedNS: 10, ThroughputBytesSec: 30, Process: processMetrics{MaxRSSBytes: 300, PeakGoroutines: 8, TotalAllocBytes: 10}},
		{ElapsedNS: 20, ThroughputBytesSec: 20, Process: processMetrics{MaxRSSBytes: 200, PeakGoroutines: 6, TotalAllocBytes: 20}},
	}
	got := summarize(runs)
	if got.MedianElapsedNS != 20 || got.MedianThroughputBytesSec != 20 || got.MedianTotalAllocBytes != 20 {
		t.Fatalf("unexpected medians: %+v", got)
	}
	if got.PeakRSSBytes != 300 || got.PeakGoroutines != 8 {
		t.Fatalf("unexpected peaks: %+v", got)
	}
}

func TestCounterDeltaDropsUnchangedAndResetCounters(t *testing.T) {
	got := counterDelta(map[string]uint64{"up": 2, "same": 4, "reset": 9}, map[string]uint64{"up": 5, "same": 4, "reset": 1})
	if len(got) != 1 || got["up"] != 3 {
		t.Fatalf("unexpected delta: %#v", got)
	}
}
