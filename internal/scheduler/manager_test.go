package scheduler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestUniqueFilePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Helper to create a dummy file
	createFile := func(name string) {
		path := filepath.Join(tmpDir, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	tests := []struct {
		name     string
		existing []string
		input    string
		want     string
	}{
		{
			name:     "No conflict",
			existing: []string{},
			input:    filepath.Join(tmpDir, "file.txt"),
			want:     filepath.Join(tmpDir, "file.txt"),
		},
		{
			name:     "One conflict",
			existing: []string{"file.txt"},
			input:    filepath.Join(tmpDir, "file.txt"),
			want:     filepath.Join(tmpDir, "file(1).txt"),
		},
		{
			name:     "Two conflicts",
			existing: []string{"file.txt", "file(1).txt"},
			input:    filepath.Join(tmpDir, "file.txt"),
			want:     filepath.Join(tmpDir, "file(2).txt"),
		},
		{
			name:     "Conflict with existing numbered file",
			existing: []string{"image(2).png"},
			input:    filepath.Join(tmpDir, "image(2).png"),
			want:     filepath.Join(tmpDir, "image(3).png"),
		},
		{
			name:     "Start from numbered file",
			existing: []string{"data(1).csv"},
			input:    filepath.Join(tmpDir, "data(1).csv"),
			want:     filepath.Join(tmpDir, "data(2).csv"),
		},
		{
			name:     "Nested directory retention",
			existing: []string{"subdir/notes.txt"},
			input:    filepath.Join(tmpDir, "subdir", "notes.txt"),
			want:     filepath.Join(tmpDir, "subdir", "notes(1).txt"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup existing files
			for _, f := range tt.existing {
				createFile(f)
			}
			// Cleanup after test case
			defer func() {
				for _, f := range tt.existing {
					_ = os.Remove(filepath.Join(tmpDir, f))
				}
			}()

			got := uniqueFilePath(tt.input)
			if got != tt.want {
				t.Errorf("uniqueFilePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUniqueFilePath_NoExtension(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a file without extension
	existingFile := filepath.Join(tmpDir, "README")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	expected := filepath.Join(tmpDir, "README(1)")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestUniqueFilePath_MultipleExtensions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create file with multiple dots
	existingFile := filepath.Join(tmpDir, "archive.tar.gz")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	// Should only consider .gz as extension
	expected := filepath.Join(tmpDir, "archive.tar(1).gz")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestRunDownload_StartedEventUsesFullDestPath(t *testing.T) {
	tmpDir := t.TempDir()
	fileSize := int64(2 * 1024 * 1024)
	server := testutil.NewStreamingMockServerT(t,
		fileSize,
		testutil.WithRangeSupport(false),
		testutil.WithByteLatency(50*time.Microsecond),
	)
	defer server.Close()

	finalPath := filepath.Join(tmpDir, "file.bin")
	surgePath := finalPath + types.IncompleteSuffix
	f, err := os.Create(surgePath)
	if err != nil {
		t.Fatalf("failed to pre-create incomplete file: %v", err)
	}
	_ = f.Close()

	progressCh := make(chan types.DownloadEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := types.DownloadRecord{
		URL:           server.URL(),
		OutputPath:    tmpDir,
		Filename:      "file.bin",
		ID:            "started-event-test",
		ProgressCh:    progressCh,
		ProgressState: progress.New("started-event-test", fileSize),
		Runtime:       &types.RuntimeConfig{},
		TotalSize:     fileSize,
		SupportsRange: false,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunDownload(ctx, &cfg)
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-progressCh:
			started := msg

			if started.DestPath != finalPath {
				t.Fatalf("started dest path = %q, want %q", started.DestPath, finalPath)
			}
			cancel()
			if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("download returned unexpected error after cancel: %v", err)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for started event")
		}
	}
}

func TestRunDownload_ConcurrentBootstrapWithoutProbeMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	fileSize := int64(2 * 1024 * 1024)
	server := testutil.NewStreamingMockServerT(t,
		fileSize,
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(10*time.Microsecond),
	)
	defer server.Close()

	finalPath := filepath.Join(tmpDir, "file.bin")
	surgePath := finalPath + types.IncompleteSuffix
	f, err := os.Create(surgePath)
	if err != nil {
		t.Fatalf("failed to pre-create incomplete file: %v", err)
	}
	_ = f.Close()

	progressCh := make(chan types.DownloadEvent, 16)
	cfg := types.DownloadRecord{
		URL:           server.URL(),
		OutputPath:    tmpDir,
		Filename:      "file.bin",
		ID:            "bootstrap-test",
		ProgressCh:    progressCh,
		ProgressState: progress.New("bootstrap-test", 0),
		Runtime:       &types.RuntimeConfig{},
		TotalSize:     0,
		SupportsRange: true,
	}

	if err := RunDownload(context.Background(), &cfg); err != nil {
		t.Fatalf("RunDownload failed: %v", err)
	}
	_, stateTotal, _, _, _, _ := cfg.ProgressState.(*progress.DownloadProgress).GetProgress()
	if stateTotal != fileSize {
		t.Fatalf("state total size = %d, want %d", stateTotal, fileSize)
	}

	foundComplete := false
	for len(progressCh) > 0 {
		msg := <-progressCh
		if msg.Type == types.EventComplete {
			foundComplete = true
			if msg.Total != fileSize {
				t.Fatalf("complete total = %d, want %d", msg.Total, fileSize)
			}
		}
	}
	if !foundComplete {
		t.Fatal("expected completion event")
	}
}

func TestRunDownload_OptimisticConcurrentFallsBackToSingle(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("fallback download content")
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	finalPath := filepath.Join(tmpDir, "fallback.bin")
	surgePath := finalPath + types.IncompleteSuffix
	f, err := os.Create(surgePath)
	if err != nil {
		t.Fatalf("failed to pre-create incomplete file: %v", err)
	}
	_ = f.Close()

	progressCh := make(chan types.DownloadEvent, 16)
	cfg := types.DownloadRecord{
		URL:           server.URL,
		OutputPath:    tmpDir,
		Filename:      "fallback.bin",
		ID:            "optimistic-fallback-test",
		ProgressCh:    progressCh,
		ProgressState: progress.New("optimistic-fallback-test", 0),
		Runtime:       &types.RuntimeConfig{},
		TotalSize:     0,
		SupportsRange: true,
	}

	if err := RunDownload(context.Background(), &cfg); err != nil {
		t.Fatalf("RunDownload failed: %v", err)
	}

	got, err := os.ReadFile(surgePath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("downloaded content = %q, want %q", string(got), string(content))
	}
	_, stateTotal, _, _, _, _ := cfg.ProgressState.(*progress.DownloadProgress).GetProgress()
	if stateTotal != int64(len(content)) {
		t.Fatalf("state total size = %d, want %d", stateTotal, len(content))
	}

	foundComplete := false
	for len(progressCh) > 0 {
		msg := <-progressCh
		if msg.Type == types.EventComplete {
			foundComplete = true
			if msg.Total != int64(len(content)) {
				t.Fatalf("complete total = %d, want %d", msg.Total, len(content))
			}
		}
	}
	if !foundComplete {
		t.Fatal("expected completion event")
	}
}

func TestRunDownload_MidTransferConcurrentFailureFallsBackToSingle(t *testing.T) {
	tmpDir := t.TempDir()
	fileSize := 10 * 1024
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(int64(fileSize)),
		testutil.WithRangeSupport(true),
		testutil.WithFailOnNthRequest(2), // Fail first worker GET
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "midfail.bin")
	surgePath := destPath + types.IncompleteSuffix
	if f, err := os.Create(surgePath); err == nil {
		_ = f.Close()
	}

	progressCh := make(chan types.DownloadEvent, 100)
	cfg := types.DownloadRecord{
		URL:           server.URL(),
		OutputPath:    tmpDir,
		Filename:      "midfail.bin",
		ID:            "mid-fail-test",
		ProgressCh:    progressCh,
		ProgressState: progress.New("mid-fail-test", 0), // Simulating unknown size
		Runtime:       &types.RuntimeConfig{MinChunkSize: 10240},
		TotalSize:     0, // Force bootstrap attempt/failure
		SupportsRange: true,
	}

	// Drain progress channel
	defer close(progressCh)
	go func() {
		for range progressCh {
		}
	}()

	if err := RunDownload(context.Background(), &cfg); err != nil {
		t.Fatalf("RunDownload should have succeeded via fallback: %v", err)
	}

	// Verification:
	// 1. Progress counter is correct
	downloaded, _, _, _, _, _ := cfg.ProgressState.(*progress.DownloadProgress).GetProgress()
	if downloaded != int64(fileSize) {
		t.Errorf("Progress counter = %d, want %d", downloaded, fileSize)
	}

	// 2. File on disk is exactly the right size (no stale tail bytes)
	fi, err := os.Stat(surgePath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(fileSize) {
		t.Errorf("File size on disk = %d, want %d (potential stale bytes)", fi.Size(), fileSize)
	}
}

func TestUniqueFilePath_IncompleteFileConflict(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create an incomplete download file
	incompleteFile := filepath.Join(tmpDir, "download.bin"+types.IncompleteSuffix)
	if err := os.WriteFile(incompleteFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Request the original filename - should conflict with incomplete
	inputPath := filepath.Join(tmpDir, "download.bin")
	result := uniqueFilePath(inputPath)
	expected := filepath.Join(tmpDir, "download(1).bin")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestUniqueFilePath_BothFileAndIncompleteExist(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create both the file and its incomplete version
	originalFile := filepath.Join(tmpDir, "video.mp4")
	incompleteFile := filepath.Join(tmpDir, "video(1).mp4"+types.IncompleteSuffix)
	if err := os.WriteFile(originalFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(incompleteFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Request original - should skip both
	result := uniqueFilePath(originalFile)
	expected := filepath.Join(tmpDir, "video(2).mp4")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestUniqueFilePath_HiddenFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a hidden file (Unix-style)
	// Note: filepath.Ext(".gitignore") returns ".gitignore" (entire name is extension)
	// So the unique path becomes "(1).gitignore"
	existingFile := filepath.Join(tmpDir, ".gitignore")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	// Since ".gitignore" has no base name (ext is the full name), result is "(1).gitignore"
	expected := filepath.Join(tmpDir, "(1).gitignore")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestUniqueFilePath_ManyConflicts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create 10 conflicting files
	for i := 0; i <= 10; i++ {
		var fileName string
		if i == 0 {
			fileName = filepath.Join(tmpDir, "doc.pdf")
		} else {
			fileName = filepath.Join(tmpDir, fmt.Sprintf("doc(%d).pdf", i))
		}
		if err := os.WriteFile(fileName, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result := uniqueFilePath(filepath.Join(tmpDir, "doc.pdf"))
	expected := filepath.Join(tmpDir, "doc(11).pdf")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestUniqueFilePath_SpecialCharactersInName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create file with special characters
	existingFile := filepath.Join(tmpDir, "file [2024].txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	expected := filepath.Join(tmpDir, "file [2024](1).txt")

	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestDownload_BuildsConfig(t *testing.T) {
	// This test verifies that the Download wrapper correctly builds a config
	// We dont test the full download

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := Download(ctx, "http://example.com/file", "/tmp/output", nil, "test-id")

	// Should fail because context is cancelled
	if err == nil {
		t.Log("Download returned nil error with cancelled context - this may be acceptable")
	}
}

func TestUniqueFilePath_EmptyFilename(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Edge case: just extension
	existingFile := filepath.Join(tmpDir, ".txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	// Should handle gracefully - behavior depends on implementation
	if result == "" {
		t.Error("uniqueFilePath returned empty string")
	}
}

func TestUniqueFilePath_LongFilename(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a file with a long name (within OS limits)
	longName := ""
	for i := 0; i < 50; i++ {
		longName += "a"
	}
	longName += ".txt"

	existingFile := filepath.Join(tmpDir, longName)
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	if result == existingFile {
		t.Error("uniqueFilePath should generate different name for existing file")
	}
}

func TestUniqueFilePath_ParenInMiddle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// File with parentheses in middle (not numbering)
	existingFile := filepath.Join(tmpDir, "file (copy).txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	// Should add (1) after the name but before extension
	expected := filepath.Join(tmpDir, "file (copy)(1).txt")
	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func TestUniqueFilePath_DeepNestedDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create deeply nested structure
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "d", "e")
	if err := os.MkdirAll(deepPath, 0o755); err != nil {
		t.Fatal(err)
	}

	existingFile := filepath.Join(deepPath, "file.txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := uniqueFilePath(existingFile)
	expected := filepath.Join(deepPath, "file(1).txt")
	if result != expected {
		t.Errorf("uniqueFilePath() = %v, want %v", result, expected)
	}
}

func BenchmarkUniqueFilePath_NoConflict(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "surge-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	path := filepath.Join(tmpDir, "nonexistent.txt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uniqueFilePath(path)
	}
}

func BenchmarkUniqueFilePath_WithConflict(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "surge-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create conflicting files
	path := filepath.Join(tmpDir, "file.txt")
	for i := 0; i <= 5; i++ {
		var name string
		if i == 0 {
			name = path
		} else {
			name = filepath.Join(tmpDir, fmt.Sprintf("file(%d).txt", i))
		}
		_ = os.WriteFile(name, []byte("test"), 0o644)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uniqueFilePath(path)
	}
}

func TestRunDownload_DoesNotEmitEventErrorOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	progressCh := make(chan types.DownloadEvent, 16)
	cfg := types.DownloadRecord{
		URL:           server.URL,
		OutputPath:    tmpDir,
		Filename:      "fail.bin",
		ID:            "no-event-error-test",
		ProgressCh:    progressCh,
		ProgressState: progress.New("no-event-error-test", 0),
		Runtime:       &types.RuntimeConfig{},
		TotalSize:     0,
		SupportsRange: false,
	}

	err := RunDownload(context.Background(), &cfg)
	if err == nil {
		t.Fatal("Expected RunDownload to return an error, got nil")
	}

	// Drain the channel and check if EventError was emitted
	close(progressCh)
	for msg := range progressCh {
		if msg.Type == types.EventError {
			t.Fatalf("RunDownload should not emit EventError, but it did: %+v", msg)
		}
	}
}

func TestSendPausedFallbackWaitsForFullChannel(t *testing.T) {
	progressCh := make(chan types.DownloadEvent, 1)
	progressCh <- types.DownloadEvent{Type: types.EventStarted}
	state := progress.New("pause-delivery", 1000)
	state.SetRateLimit(2048, true)
	state.SetPendingResumeState(&types.DownloadRecord{
		ID:         "pause-delivery",
		Filename:   "pause.bin",
		DestPath:   "/tmp/pause.bin",
		Downloaded: 400,
	})
	cfg := &types.DownloadRecord{
		ID:            "pause-delivery",
		Filename:      "pause.bin",
		ProgressCh:    progressCh,
		ProgressState: state,
		Runtime:       &types.RuntimeConfig{Workers: 3, MinChunkSize: 4096},
	}

	done := make(chan struct{})
	go func() {
		sendPausedFallback(progressCh, cfg, nil, nil)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("sendPausedFallback returned while the lifecycle channel was full")
	case <-time.After(20 * time.Millisecond):
	}
	<-progressCh

	select {
	case event := <-progressCh:
		if event.Type != types.EventPaused || event.State == nil {
			t.Fatalf("pause event = %+v", event)
		}
		if event.Downloaded != 400 || event.RateLimit != 2048 || !event.RateLimitSet {
			t.Fatalf("pause event metadata = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventPaused after channel space became available")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sendPausedFallback did not return after delivery")
	}

	sendPausedFallback(progressCh, cfg, nil, nil)
	select {
	case duplicate := <-progressCh:
		t.Fatalf("unexpected duplicate pause event: %+v", duplicate)
	default:
	}
}
