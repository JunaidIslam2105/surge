package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestHandleDownload_PathResolution(t *testing.T) {
	// Setup temporary directory for mocking XDG_CONFIG_HOME
	tempDir, err := os.MkdirTemp("", "surge-test-home")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	origLifecycle := GlobalLifecycle
	origLifecycleCleanup := GlobalLifecycleCleanup
	origService := GlobalService
	t.Cleanup(func() {
		GlobalLifecycle = origLifecycle
		GlobalLifecycleCleanup = origLifecycleCleanup
		GlobalService = origService
	})
	GlobalLifecycle = nil
	GlobalLifecycleCleanup = nil
	GlobalService = nil

	origSettings := globalSettings
	globalSettings = nil
	t.Cleanup(func() { globalSettings = origSettings })

	// Ensure a clean state DB for the test scope.
	store.CloseDB()
	store.Configure(filepath.Join(tempDir, "surge.db"))
	defer store.CloseDB()

	// Mock XDG_CONFIG_HOME to affect GetSurgeDir() on Linux
	originalConfigHome := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tempDir)
	originalAppData := os.Getenv("APPDATA")
	_ = os.Setenv("APPDATA", tempDir)
	defer func() {
		if originalConfigHome == "" {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			_ = os.Setenv("XDG_CONFIG_HOME", originalConfigHome)
		}
		if originalAppData == "" {
			_ = os.Unsetenv("APPDATA")
		} else {
			_ = os.Setenv("APPDATA", originalAppData)
		}
	}()

	// Create surge config directory
	surgeConfigDir := filepath.Join(tempDir, "surge")
	if err := os.MkdirAll(surgeConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Setup default download directory
	defaultDownloadDir := filepath.Join(tempDir, "Downloads")
	if err := os.MkdirAll(defaultDownloadDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a temporary settings file
	settings := config.DefaultSettings()
	settings.General.DefaultDownloadDir.Value = defaultDownloadDir
	settings.Extension.ExtensionPrompt.Value = false

	if err := config.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Initialize GlobalPool (required by handleDownload)
	if GlobalPool != nil {
		GlobalPool.GracefulShutdown()
	}
	tmpPool := scheduler.New(nil, 1)
	t.Cleanup(func() {
		if tmpPool != nil {
			tmpPool.GracefulShutdown()
		}
	})
	GlobalPool = tmpPool

	tests := []struct {
		name               string
		request            DownloadRequest
		expectedOutputPath string
		skipOnWindows      bool // simulation tests that don't apply when the daemon IS on Windows
	}{
		{
			name: "Absolute Path (Explicit)",
			request: DownloadRequest{
				URL:  "http://example.com/file1",
				Path: filepath.Join(tempDir, "absolute"),
			},
			expectedOutputPath: filepath.Join(tempDir, "absolute"),
		},
		{
			name: "Relative Path (No Flag)",
			request: DownloadRequest{
				URL:  "http://example.com/file2",
				Path: "relative",
			},
			expectedOutputPath: filepath.Join(mustGetwd(t), "relative"),
		},
		{
			name: "Relative to Default Dir",
			request: DownloadRequest{
				URL:                  "http://example.com/file3",
				Path:                 "subdir",
				RelativeToDefaultDir: true,
			},
			expectedOutputPath: filepath.Join(defaultDownloadDir, "subdir"),
		},
		{
			name: "Nested Relative to Default Dir",
			request: DownloadRequest{
				URL:                  "http://example.com/file4",
				Path:                 "nested/deep",
				RelativeToDefaultDir: true,
			},
			expectedOutputPath: filepath.Join(defaultDownloadDir, "nested", "deep"),
		},
		{
			name: "Empty Path (Default)",
			request: DownloadRequest{
				URL:  "http://example.com/file5",
				Path: "",
			},
			expectedOutputPath: defaultDownloadDir,
		},
		{
			// On a non-Windows daemon, a Windows absolute path from the extension
			// is remapped to the daemon's default download directory.
			// On Windows, C:/ is a real local path — no remapping occurs.
			name: "Windows Download Root Maps To Default Dir",
			request: DownloadRequest{
				URL:  "http://example.com/file6",
				Path: "C:/Users/me/Downloads",
			},
			expectedOutputPath: defaultDownloadDir,
			skipOnWindows:      true,
		},
		{
			// Same as above: the "/surge-repro" suffix is preserved on non-Windows daemons.
			// On Windows, this is just a literal local path.
			name: "Windows Nested Path Maps Under Default Dir",
			request: DownloadRequest{
				URL:  "http://example.com/file7",
				Path: "C:/Users/me/Downloads/surge-repro",
			},
			expectedOutputPath: filepath.Join(defaultDownloadDir, "surge-repro"),
			skipOnWindows:      true,
		},
		{
			name: "Windows Nested Path Relative Flag Maps Under Default Dir",
			request: DownloadRequest{
				URL:                  "http://example.com/file8",
				Path:                 "C:/Users/me/Downloads/surge-repro",
				RelativeToDefaultDir: true,
			},
			expectedOutputPath: filepath.Join(defaultDownloadDir, "surge-repro"),
			skipOnWindows:      true,
		},
		{
			name: "Unmatched Windows Path Falls Back To Default Dir",
			request: DownloadRequest{
				URL:                  "http://example.com/file9",
				Path:                 "E:/Torrents/complete",
				RelativeToDefaultDir: true,
			},
			expectedOutputPath: defaultDownloadDir,
			skipOnWindows:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("path-remapping simulation does not apply on Windows daemons")
			}
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest("POST", "/download", bytes.NewBuffer(body))
			w := httptest.NewRecorder()
			eventBus := orchestrator.NewEventBus()
			t.Cleanup(func() { eventBus.Shutdown() })
			getAll := func() []types.DownloadRecord { return GlobalPool.GetAll() }
			tmpLifecycle := orchestrator.NewLifecycleManager(GlobalPool, eventBus, nil, buildActiveDownloadChecker(getAll))
			t.Cleanup(func() { tmpLifecycle.Shutdown() })
			GlobalLifecycle = tmpLifecycle
			svc := service.NewLocalDownloadService(GlobalLifecycle)

			// We pass defaultDownloadDir as a fallback to handleDownload, but since we mocked settings,
			// it should prioritize settings.General.DefaultDownloadDir
			handleDownload(w, req, defaultDownloadDir, svc)

			if w.Code != http.StatusOK && w.Code != http.StatusConflict {
				t.Errorf("Expected OK, got %d. Body: %s", w.Code, w.Body.String())
			}

			// GlobalPool access
			configs := GlobalPool.GetAll()
			found := false
			for _, cfg := range configs {
				if cfg.URL == tt.request.URL {
					found = true
					t.Logf("OutputPath for %s: %s", tt.name, cfg.OutputPath)

					expectedAbs := tt.expectedOutputPath
					if abs, err := filepath.Abs(expectedAbs); err == nil {
						expectedAbs = abs
					}

					if cfg.OutputPath != expectedAbs {
						if runtime.GOOS == "windows" && strings.EqualFold(cfg.OutputPath, expectedAbs) {
							// Windows paths are case-insensitive
						} else {
							t.Errorf("Expected path %s, got %s", expectedAbs, cfg.OutputPath)
						}
					}
					break
				}
			}
			if !found {
				t.Errorf("Download was not queued")
			}
		})
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(mustGetwd(t), "relative"))
	})
}

func TestShouldFallbackUnmappedWindowsPath(t *testing.T) {
	tests := []struct {
		name                 string
		relativeToDefaultDir bool
		hostOS               string
		want                 bool
	}{
		{
			name:                 "relative request falls back on windows",
			relativeToDefaultDir: true,
			hostOS:               "windows",
			want:                 true,
		},
		{
			name:                 "relative request falls back on linux",
			relativeToDefaultDir: true,
			hostOS:               "linux",
			want:                 true,
		},
		{
			name:                 "explicit request does not fall back on windows",
			relativeToDefaultDir: false,
			hostOS:               "windows",
			want:                 false,
		},
		{
			name:                 "explicit request falls back on linux",
			relativeToDefaultDir: false,
			hostOS:               "linux",
			want:                 true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFallbackUnmappedWindowsPath(tt.relativeToDefaultDir, tt.hostOS); got != tt.want {
				t.Fatalf("shouldFallbackUnmappedWindowsPath(%v, %q) = %v, want %v", tt.relativeToDefaultDir, tt.hostOS, got, tt.want)
			}
		})
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	return wd
}

func TestHandleDownload_SkipApprovalUsesLifecycleEnqueue(t *testing.T) {
	setupIsolatedCmdState(t)

	progressCh := make(chan types.DownloadEvent, 10)
	GlobalProgressCh = progressCh
	if GlobalPool != nil {
		GlobalPool.GracefulShutdown()
	}
	tmpPool := scheduler.New(progressCh, 1)
	t.Cleanup(func() {
		if tmpPool != nil {
			tmpPool.GracefulShutdown()
		}
	})
	GlobalPool = tmpPool

	origLifecycle := GlobalLifecycle
	origService := GlobalService
	t.Cleanup(func() {
		GlobalLifecycle = origLifecycle
		GlobalService = origService
		GlobalPool = nil
		GlobalProgressCh = nil
	})

	firstRange := make(chan string, 1)
	probeServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		select {
		case firstRange <- rangeHeader:
		default:
		}
		if rangeHeader == "bytes=0-0" {
			w.Header().Set("Content-Range", "bytes 0-0/7")
			w.Header().Set("Content-Length", "1")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
			return
		}
		w.Header().Set("Content-Range", "bytes 0-6/7")
		w.Header().Set("Content-Length", "7")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("content"))
	}))
	defer probeServer.Close()

	tempDir := t.TempDir()
	expectedFile := "from-extension.bin"

	eventBus := orchestrator.NewEventBus()
	t.Cleanup(func() { eventBus.Shutdown() })
	getAll := func() []types.DownloadRecord { return GlobalPool.GetAll() }
	tmpLifecycle := orchestrator.NewLifecycleManager(GlobalPool, eventBus, nil, buildActiveDownloadChecker(getAll))
	t.Cleanup(func() { tmpLifecycle.Shutdown() })
	GlobalLifecycle = tmpLifecycle
	svc := service.NewLocalDownloadService(GlobalLifecycle)
	GlobalService = svc
	t.Cleanup(func() {
		_ = svc.Shutdown()
	})

	body := fmt.Sprintf(`{
		"url": %q,
		"filename": %q,
		"path": %q,
		"skip_approval": true,
		"is_explicit_category": true,
		"headers": {"Authorization": "Bearer test"}
	}`, probeServer.URL, expectedFile, tempDir)

	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case got := <-firstRange:
		if got != "bytes=0-0" {
			t.Fatalf("first Range header = %q, want bytes=0-0", got)
		}
	default:
		t.Fatal("expected lifecycle enqueue to probe the server")
	}
	configs := GlobalPool.GetAll()
	if len(configs) != 1 {
		t.Fatalf("expected 1 download queued, got %d", len(configs))
	}
	cfg := configs[0]
	if cfg.URL != probeServer.URL {
		t.Fatalf("url = %q, want %q", cfg.URL, probeServer.URL)
	}
	expectedPath := tempDir
	if abs, err := filepath.Abs(tempDir); err == nil {
		expectedPath = abs
	}
	if cfg.OutputPath != expectedPath {
		t.Fatalf("path = %q, want %q", cfg.OutputPath, expectedPath)
	}
	if cfg.Filename != expectedFile {
		t.Fatalf("filename = %q, want %q", cfg.Filename, expectedFile)
	}
	if cfg.Headers["Authorization"] != "Bearer test" {
		t.Fatalf("headers were not forwarded to lifecycle addFunc")
	}
}

func TestHandleDownload_EnqueueError_RecordsPreflightError(t *testing.T) {
	setupIsolatedCmdState(t)

	progressCh := make(chan types.DownloadEvent, 10)
	GlobalProgressCh = progressCh
	if GlobalPool != nil {
		GlobalPool.GracefulShutdown()
	}
	tmpPool := scheduler.New(progressCh, 1)
	t.Cleanup(func() {
		if tmpPool != nil {
			tmpPool.GracefulShutdown()
		}
	})
	GlobalPool = tmpPool

	origLifecycle := GlobalLifecycle
	origService := GlobalService
	t.Cleanup(func() {
		GlobalLifecycle = origLifecycle
		GlobalService = origService
		GlobalPool = nil
		GlobalProgressCh = nil
	})

	eventBus := orchestrator.NewEventBus()
	t.Cleanup(func() { eventBus.Shutdown() })
	getAll := func() []types.DownloadRecord { return GlobalPool.GetAll() }
	tmpLifecycle := orchestrator.NewLifecycleManager(GlobalPool, eventBus, nil, buildActiveDownloadChecker(getAll))
	t.Cleanup(func() { tmpLifecycle.Shutdown() })
	GlobalLifecycle = tmpLifecycle
	svc := service.NewLocalDownloadService(GlobalLifecycle)
	GlobalService = svc
	t.Cleanup(func() {
		_ = svc.Shutdown()
	})

	// Use a URL with an invalid scheme so ProbeServer fails immediately with a
	// terminal error. Since probing now runs at enqueue time, the request is
	// synchronously rejected with 500 — no background worker is involved.
	body := `{"url": "badscheme://example.com/file.bin", "path": "/tmp", "skip_approval": true}`
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for terminal probe error, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDownload_ForwardsPerTaskOverridesToLifecycle(t *testing.T) {
	setupIsolatedCmdState(t)

	progressCh := make(chan types.DownloadEvent, 10)
	GlobalProgressCh = progressCh
	if GlobalPool != nil {
		GlobalPool.GracefulShutdown()
	}
	tmpPool := scheduler.New(progressCh, 1)
	t.Cleanup(func() {
		if tmpPool != nil {
			tmpPool.GracefulShutdown()
		}
	})
	GlobalPool = tmpPool

	origLifecycle := GlobalLifecycle
	origService := GlobalService
	t.Cleanup(func() {
		GlobalLifecycle = origLifecycle
		GlobalService = origService
		GlobalPool = nil
		GlobalProgressCh = nil
	})

	probeServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-0/1024")
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer probeServer.Close()

	tempDir := t.TempDir()

	eventBus := orchestrator.NewEventBus()
	t.Cleanup(func() { eventBus.Shutdown() })
	getAll := func() []types.DownloadRecord { return GlobalPool.GetAll() }
	tmpLifecycle := orchestrator.NewLifecycleManager(GlobalPool, eventBus, nil, buildActiveDownloadChecker(getAll))
	t.Cleanup(func() { tmpLifecycle.Shutdown() })
	GlobalLifecycle = tmpLifecycle
	svc := service.NewLocalDownloadService(GlobalLifecycle)
	GlobalService = svc
	t.Cleanup(func() {
		_ = svc.Shutdown()
	})

	minChunk := int64(8 * 1024 * 1024)
	body := fmt.Sprintf(`{
		"url": %q,
		"filename": "override-test.bin",
		"path": %q,
		"skip_approval": true,
		"workers": 12,
		"min_chunk_size": %d
	}`, probeServer.URL, tempDir, minChunk)

	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	configs := GlobalPool.GetAll()
	if len(configs) != 1 {
		t.Fatalf("expected 1 download queued, got %d", len(configs))
	}
	if configs[0].Runtime.Workers != 12 {
		t.Fatalf("expected lifecycle addFunc to receive workers=12, got %d", configs[0].Runtime.Workers)
	}
	if configs[0].Runtime.MinChunkSize != minChunk {
		t.Fatalf("expected lifecycle addFunc to receive minChunkSize=%d, got %d", minChunk, configs[0].Runtime.MinChunkSize)
	}
}

type failingPublishService struct {
	fakeRemoteDownloadService
	publishErr error
}

func (f *failingPublishService) Publish(msg types.DownloadEvent) error {
	return f.publishErr
}

func TestHandleDownload_PublishError_RecordsPreflightError(t *testing.T) {
	setupIsolatedCmdState(t)

	origPool := GlobalPool
	origProgress := GlobalProgressCh
	origService := GlobalService
	origLifecycle := GlobalLifecycle
	t.Cleanup(func() {
		GlobalPool = origPool
		GlobalProgressCh = origProgress
		GlobalService = origService
		GlobalLifecycle = origLifecycle
	})

	if GlobalPool != nil {
		GlobalPool.GracefulShutdown()
	}
	tmpPool := scheduler.New(nil, 1)
	t.Cleanup(func() {
		if tmpPool != nil {
			tmpPool.GracefulShutdown()
		}
	})
	GlobalPool = tmpPool

	origServerProgram := serverProgram
	serverProgram = &tea.Program{}
	t.Cleanup(func() { serverProgram = origServerProgram })

	settings := config.DefaultSettings()
	settings.Extension.ExtensionPrompt.Value = true
	settings.General.WarnOnDuplicate.Value = false
	if err := config.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}

	svc := &failingPublishService{publishErr: errors.New("publish failed")}
	GlobalService = svc

	outDir := t.TempDir()
	body := fmt.Sprintf(`{"url": %q, "path": %q}`, "http://example.com/file.bin", outDir)
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := store.LoadMasterList()
	if err != nil {
		t.Fatalf("LoadMasterList failed: %v", err)
	}

	found := false
	for _, entry := range list.Downloads {
		if strings.Contains(entry.URL, "http://example.com/file.bin") && entry.Status == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected errored download entry in master list after publish failure via HTTP API")
	}
}

// TestHandleDownload_DoesNotUseCancelledHTTPRequestContext verifies that the
// enqueue path uses the application-level enqueue context instead of the HTTP
// request context. When the HTTP client disconnects or the handler returns
// before the probe finishes, r.Context() is cancelled — but the download
// should still be enqueued successfully.
func TestHandleDownload_DoesNotUseCancelledHTTPRequestContext(t *testing.T) {
	setupIsolatedCmdState(t)

	progressCh := make(chan types.DownloadEvent, 10)
	GlobalProgressCh = progressCh
	if GlobalPool != nil {
		GlobalPool.GracefulShutdown()
	}
	tmpPool := scheduler.New(progressCh, 1)
	t.Cleanup(func() {
		if tmpPool != nil {
			tmpPool.GracefulShutdown()
		}
	})
	GlobalPool = tmpPool

	origLifecycle := GlobalLifecycle
	origService := GlobalService
	t.Cleanup(func() {
		GlobalLifecycle = origLifecycle
		GlobalService = origService
		GlobalPool = nil
		GlobalProgressCh = nil
	})

	probeServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-0/7")
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer probeServer.Close()

	tempDir := t.TempDir()

	eventBus := orchestrator.NewEventBus()
	t.Cleanup(func() { eventBus.Shutdown() })
	getAll := func() []types.DownloadRecord { return GlobalPool.GetAll() }
	tmpLifecycle := orchestrator.NewLifecycleManager(GlobalPool, eventBus, nil, buildActiveDownloadChecker(getAll))
	t.Cleanup(func() { tmpLifecycle.Shutdown() })
	GlobalLifecycle = tmpLifecycle
	svc := service.NewLocalDownloadService(GlobalLifecycle)
	GlobalService = svc
	t.Cleanup(func() {
		_ = svc.Shutdown()
	})

	body := fmt.Sprintf(`{
		"url": %q,
		"filename": "ctx-test.bin",
		"path": %q,
		"skip_approval": true
	}`, probeServer.URL, tempDir)

	// Create an HTTP request with an already-cancelled context to simulate
	// a client that disconnects before the probe finishes.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body)).WithContext(ctx)
	rec := httptest.NewRecorder()

	handleDownload(rec, req, "", svc)

	// Before the fix, this would return 500 with "context canceled".
	// After the fix, the download should be queued successfully because
	// enqueueDownloadRequest uses currentEnqueueContext() instead of r.Context().
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (download queued despite cancelled HTTP context), got %d: %s", rec.Code, rec.Body.String())
	}
	configs := GlobalPool.GetAll()
	if len(configs) != 1 {
		t.Fatalf("expected 1 download queued, got %d", len(configs))
	}
	if configs[0].Filename != "ctx-test.bin" {
		t.Fatalf("filename = %q, want %q", configs[0].Filename, "ctx-test.bin")
	}
}
