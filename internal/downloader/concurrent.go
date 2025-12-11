package downloader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"surge/internal/utils"
)

const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB

	// Each connection downloads chunks of this size
	MinChunk     = 2 * MB  // Minimum chunk size
	MaxChunk     = 16 * MB // Maximum chunk size
	TargetChunk  = 8 * MB  // Target chunk size
	AlignSize    = 4 * KB  // Align chunks to 4KB for filesystem
	WorkerBuffer = 512 * KB

	TasksPerWorker = 4 // Target tasks per connection

	// Connection limits
	PerHostMax = 8 // Max concurrent connections per host
)

// Buffer pool to reduce GC pressure
var bufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, WorkerBuffer)
		return &buf
	},
}

// Task represents a byte range to download
type Task struct {
	Offset int64
	Length int64
}

// TaskQueue is a thread-safe work-stealing queue
type TaskQueue struct {
	tasks       []Task
	head        int
	mu          sync.Mutex
	cond        *sync.Cond
	done        bool
	idleWorkers int64 // Atomic counter for idle workers
}

func NewTaskQueue() *TaskQueue {
	tq := &TaskQueue{}
	tq.cond = sync.NewCond(&tq.mu)
	return tq
}

func (q *TaskQueue) Push(t Task) {
	q.mu.Lock()
	q.tasks = append(q.tasks, t)
	q.cond.Signal()
	q.mu.Unlock()
}

func (q *TaskQueue) PushMultiple(tasks []Task) {
	q.mu.Lock()
	q.tasks = append(q.tasks, tasks...)
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *TaskQueue) Pop() (Task, bool) {
	// Mark as idle while waiting
	atomic.AddInt64(&q.idleWorkers, 1)

	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.tasks) == 0 && !q.done {
		q.cond.Wait()
	}

	// No longer idle once we have work (or are done)
	atomic.AddInt64(&q.idleWorkers, -1)

	if len(q.tasks) == 0 {
		return Task{}, false
	}

	t := q.tasks[q.head]
	q.head++
	if q.head > len(q.tasks)/2 {
		q.tasks = append([]Task(nil), q.tasks[q.head:]...)
		q.head = 0
	}
	return t, true
}

func (q *TaskQueue) Close() {
	q.mu.Lock()
	q.done = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *TaskQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

func (q *TaskQueue) IdleWorkers() int64 {
	return atomic.LoadInt64(&q.idleWorkers)
}

// SplitLargestIfNeeded finds the largest queued task and splits it if > 2*MinChunk
// Returns true if a split occurred
func (q *TaskQueue) SplitLargestIfNeeded() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Find largest queued task
	idx := -1
	var maxLen int64 = 0
	for i, t := range q.tasks {
		if t.Length > maxLen && t.Length > 2*MinChunk {
			maxLen = t.Length
			idx = i
		}
	}

	if idx == -1 {
		return false // No task large enough to split
	}

	t := q.tasks[idx]

	// Split in half, aligned to AlignSize
	half := (t.Length / 2 / AlignSize) * AlignSize
	if half < MinChunk {
		return false // Halves would be too small
	}

	left := Task{Offset: t.Offset, Length: half}
	right := Task{Offset: t.Offset + half, Length: t.Length - half}

	// Replace original with right half, append left half
	q.tasks[idx] = right
	q.tasks = append(q.tasks, left)

	// Wake up idle workers
	q.cond.Broadcast()
	return true
}

// getInitialConnections returns the starting number of connections based on file size
func getInitialConnections(fileSize int64) int {
	switch {
	case fileSize < 10*MB:
		return 1
	case fileSize < 100*MB:
		return 4
	case fileSize < 1*GB:
		return 6
	default:
		return 8
	}
}

// calculateChunkSize determines optimal chunk size
func calculateChunkSize(fileSize int64, numConns int) int64 {
	targetChunks := int64(numConns * TasksPerWorker)
	chunkSize := fileSize / targetChunks

	// Clamp to min/max
	if chunkSize < MinChunk {
		chunkSize = MinChunk
	}
	if chunkSize > MaxChunk {
		chunkSize = MaxChunk
	}

	// Align to 4KB
	chunkSize = (chunkSize / AlignSize) * AlignSize
	if chunkSize == 0 {
		chunkSize = AlignSize
	}

	return chunkSize
}

// createTasks generates initial task queue from file size and chunk size
func createTasks(fileSize, chunkSize int64) []Task {
	var tasks []Task
	for offset := int64(0); offset < fileSize; offset += chunkSize {
		length := chunkSize
		if offset+length > fileSize {
			length = fileSize - offset
		}
		tasks = append(tasks, Task{Offset: offset, Length: length})
	}
	return tasks
}

// newConcurrentClient creates an http.Client tuned for concurrent downloads
func newConcurrentClient() *http.Client {
	transport := &http.Transport{
		// Connection pooling
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: PerHostMax + 2, // Slightly more than max to handle bursts
		MaxConnsPerHost:     PerHostMax,

		// Timeouts to prevent hung connections
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		// Performance tuning
		DisableCompression: true, // Files are usually already compressed
		ForceAttemptHTTP2:  true, // HTTP/2 multiplexing if available

		// Dial settings for TCP reliability
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	return &http.Client{
		Transport: transport,
	}
}

func (d *Downloader) concurrentDownload(ctx context.Context, rawurl, outPath string, verbose bool, md5sum, sha256sum string) (err error) {
	// Create tuned HTTP client for concurrent downloads
	client := newConcurrentClient()

	// 1. HEAD request to get file size
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawurl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) "+
		"Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// 2. Get file size
	contentLength := resp.Header.Get("Content-Length")
	if contentLength == "" {
		if verbose {
			fmt.Println("Content-Length unknown, falling back to single download")
		}
		return d.singleDownload(ctx, rawurl, outPath, verbose)
	}

	fileSize, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Content-Length: %w", err)
	}

	// 3. Determine connections and chunk size
	numConns := getInitialConnections(fileSize)
	chunkSize := calculateChunkSize(fileSize, numConns)

	if verbose {
		fmt.Printf("File size: %s, connections: %d, chunk size: %s\n",
			utils.ConvertBytesToHumanReadable(fileSize),
			numConns,
			utils.ConvertBytesToHumanReadable(chunkSize))
	}

	// 4. Create and preallocate output file
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	// Preallocate file to avoid fragmentation
	if err := outFile.Truncate(fileSize); err != nil {
		return fmt.Errorf("failed to preallocate file: %w", err)
	}

	// 5. Create task queue
	tasks := createTasks(fileSize, chunkSize)
	queue := NewTaskQueue()
	queue.PushMultiple(tasks)

	// 6. Progress tracking
	var totalDownloaded int64
	startTime := time.Now()

	// 7. Start balancer goroutine for dynamic chunk splitting
	balancerCtx, cancelBalancer := context.WithCancel(ctx)
	defer cancelBalancer()

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		maxSplits := 50 // Limit total splits to avoid over-splitting
		splitCount := 0

		for {
			select {
			case <-balancerCtx.Done():
				return
			case <-ticker.C:
				// Only split if there are idle workers and we haven't hit the limit
				if queue.IdleWorkers() > 0 && splitCount < maxSplits {
					if queue.SplitLargestIfNeeded() {
						splitCount++
						if verbose {
							fmt.Fprintf(os.Stderr, "\n[Balancer] Split task (total splits: %d)\n", splitCount)
						}
					}
				}
			}
		}
	}()

	// 8. Start workers
	var wg sync.WaitGroup
	workerErrors := make(chan error, numConns)

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			err := d.worker(ctx, workerID, rawurl, outFile, queue, &totalDownloaded, fileSize, startTime, verbose, client)
			if err != nil {
				workerErrors <- err
			}
		}(i)
	}

	// 9. Wait for all workers to complete
	go func() {
		wg.Wait()
		close(workerErrors)
		queue.Close()
	}()

	// 10. Check for errors
	for err := range workerErrors {
		if err != nil {
			return err
		}
	}

	// 11. Final sync
	if err := outFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// 12. Print final stats
	elapsed := time.Since(startTime)
	speed := float64(fileSize) / elapsed.Seconds()
	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n",
		utils.ConvertBytesToHumanReadable(fileSize),
		elapsed.Round(time.Millisecond),
		utils.ConvertBytesToHumanReadable(int64(speed)))

	return nil
}

// worker downloads tasks from the queue
func (d *Downloader) worker(ctx context.Context, id int, rawurl string, file *os.File, queue *TaskQueue, downloaded *int64, totalSize int64, startTime time.Time, verbose bool, client *http.Client) error {
	// Get pooled buffer
	bufPtr := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufPtr)
	buf := *bufPtr

	for {
		// Get next task
		task, ok := queue.Pop()
		if !ok {
			return nil // Queue closed, no more work
		}

		// Download this task
		err := d.downloadTask(ctx, rawurl, file, task, buf, downloaded, totalSize, startTime, verbose, client)
		if err != nil {
			// On error, push task back for retry (could add retry limit)
			queue.Push(task)
			return fmt.Errorf("worker %d failed: %w", id, err)
		}
	}
}

// downloadTask downloads a single byte range and writes to file at offset
func (d *Downloader) downloadTask(ctx context.Context, rawurl string, file *os.File, task Task, buf []byte, downloaded *int64, totalSize int64, startTime time.Time, verbose bool, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) "+
		"Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Offset, task.Offset+task.Length-1))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Read and write at offset
	offset := task.Offset
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := file.WriteAt(buf[:n], offset)
			if writeErr != nil {
				return fmt.Errorf("write error: %w", writeErr)
			}
			offset += int64(n)

			// Update progress
			newTotal := atomic.AddInt64(downloaded, int64(n))
			d.printProgress(newTotal, totalSize, startTime, verbose, 0)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read error: %w", readErr)
		}
	}

	return nil
}
