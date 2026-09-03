package concurrent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestInitialRangesStayWithinWorkerRequestBudget(t *testing.T) {
	const workers = 3
	fileSize := int64(workers*types.AlignSize + 123)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: workers,
		Workers:                   workers,
		MinChunkSize:              types.AlignSize,
	}
	downloader := NewConcurrentDownloader("signed-range-budget", nil, nil, runtime)

	var (
		requests atomic.Int64
		mu       sync.Mutex
		ranges   [][2]int64
	)
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		if requestNumber > workers {
			http.Error(w, "request budget exceeded", http.StatusNotFound)
			return
		}

		start, end, err := parseTestByteRange(r.Header.Get("Range"))
		if err != nil || start < 0 || end < start || end >= fileSize {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		mu.Lock()
		ranges = append(ranges, [2]int64{start, end})
		mu.Unlock()

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, end-start+1))
	}))
	defer server.Close()

	outFile, err := os.CreateTemp(t.TempDir(), "signed-range-*.surge")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outFile.Close() }()

	numConns := downloader.getInitialConnections(fileSize)
	if numConns != workers {
		t.Fatalf("initial connections = %d, want %d", numConns, workers)
	}
	chunkSize := downloader.determineChunkSize(fileSize, numConns)
	tasks, err := downloader.setupTasks(outFile.Name(), fileSize, chunkSize, numConns, outFile, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		active := &ActiveTask{Task: task}
		active.CurrentOffset.Store(task.Offset)
		active.StopAt.Store(task.Offset + task.Length)

		if err := downloader.downloadTask(
			context.Background(),
			server.URL,
			outFile,
			active,
			make([]byte, types.WorkerBuffer),
			server.Client(),
			fileSize,
		); err != nil {
			t.Fatalf("initial range request %d of %d failed: %v", requests.Load(), workers, err)
		}
	}

	if got := requests.Load(); got != workers {
		t.Fatalf("initial range requests = %d, want %d", got, workers)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != len(tasks) {
		t.Fatalf("recorded ranges count %d != tasks count %d", len(ranges), len(tasks))
	}
	for i, task := range tasks {
		wantStart := task.Offset
		wantEnd := task.Offset + task.Length - 1
		if ranges[i][0] != wantStart || ranges[i][1] != wantEnd {
			t.Errorf("task %d: recorded range [%d-%d] doesn't match expected [%d-%d]",
				i, ranges[i][0], ranges[i][1], wantStart, wantEnd)
		}
	}
	if len(ranges) > 0 {
		finalEnd := ranges[len(ranges)-1][1]
		if finalEnd != fileSize-1 {
			t.Errorf("final range ends at %d, want %d", finalEnd, fileSize-1)
		}
	}
}

func TestDownloadTask_CompletesExactRangeWithoutFIN(t *testing.T) {
	const rangeSize = int64(32 * 1024)

	delivered := make(chan struct{})
	handlerDone := make(chan struct{})
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		if got := r.Header.Get("Range"); got != "bytes=0-32767" {
			http.Error(w, "unexpected range: "+got, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-32767/32768")
		w.WriteHeader(http.StatusPartialContent)
		if _, err := w.Write(make([]byte, rangeSize)); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		close(delivered)

		// Keep the response open. The downloader must finish from its requested
		// byte count instead of waiting for EOF or a connection FIN.
		<-r.Context().Done()
	}))
	defer server.Close()

	outFile, err := os.CreateTemp(t.TempDir(), "exact-range-*.surge")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outFile.Close() }()

	task := types.Task{Offset: 0, Length: rangeSize}
	active := &ActiveTask{Task: task}
	active.CurrentOffset.Store(task.Offset)
	active.StopAt.Store(task.Offset + task.Length)
	downloader := NewConcurrentDownloader("exact-range-no-fin", nil, nil, &types.RuntimeConfig{
		WorkerBufferSize: int(rangeSize),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- downloader.downloadTask(ctx, server.URL, outFile, active, make([]byte, rangeSize), server.Client(), rangeSize)
	}()

	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("server did not deliver the advertised range")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("downloadTask failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("downloadTask waited for FIN after receiving the exact range")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("response body close did not release the open server handler")
	}
	if got := active.CurrentOffset.Load(); got != rangeSize {
		t.Fatalf("current offset = %d, want %d", got, rangeSize)
	}
}

func parseTestByteRange(header string) (int64, int64, error) {
	value, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return 0, 0, fmt.Errorf("missing bytes prefix")
	}
	startText, endText, ok := strings.Cut(value, "-")
	if !ok {
		return 0, 0, fmt.Errorf("missing range separator")
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.ParseInt(endText, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}
