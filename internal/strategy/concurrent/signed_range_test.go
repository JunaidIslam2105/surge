package concurrent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

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

	var requests atomic.Int64
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
