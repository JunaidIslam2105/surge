package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

// TestTUI_Startup_HandlesResume verifies that TUI initialization handles resume logic correctly
// including "queued" items and AutoResume settings.
func TestTUI_Startup_HandlesResume(t *testing.T) {
	// 1. Setup Environment
	tmpDir, err := os.MkdirTemp("", "surge-tui-startup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	setupTestEnv(t, tmpDir)

	// 2. Seed DB with a 'queued' download (as set by 'surge resume' offline)
	testID := "tui-resume-id"
	testURL := "http://example.com/tui-resume.zip"
	testDest := filepath.Join(tmpDir, "tui-resume.zip")
	seedDownload(t, testID, testURL, testDest, "queued")

	// 3. Initialize TUI Model (Simulate StartTUI)
	bus := orchestrator.NewEventBus()
	mgr := orchestrator.NewLifecycleManager(nil, bus, nil)

	// PASSING noResume=false (default)
	m := InitialRootModel(1700, "test-version", service.NewLocalDownloadService(mgr), mgr, nil, false)

	// 4. Verify Download is Active in Model
	// InitialRootModel loads downloads and should set paused=false for "queued" items
	found := false
	for _, d := range m.downloads { // Access unexported field
		if d.ID == testID {
			found = true
			if !d.resuming {
				t.Error("TUI Model initialized queued download without resuming=true")
			}
			// Note: d.paused will be true initially until async resume completes
			// Verify Filename and Destination are preserved (critical to avoid uniqueFilePath generation)
			if d.Filename != "tui-resume.zip" {
				t.Errorf("Expected filename tui-resume.zip, got %s", d.Filename)
			}
			if d.Destination != testDest {
				t.Errorf("Expected destination %s, got %s", d.Destination, d.Destination)
			}
		}
	}
	if !found {
		t.Error("TUI Model failed to load queued download")
	}

	// 5. Verify it was added to Pool
	// We can't rely on pool immediate state as worker is async, but Model state reflects intent
}

func TestTUI_Startup_LoadsCompletedTiming(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-tui-completed-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	setupTestEnv(t, tmpDir)

	testID := "tui-completed-id"
	testURL := "http://example.com/completed.zip"
	testDest := filepath.Join(tmpDir, "completed.zip")
	const totalSize = int64(5 * 1024 * 1024)
	const timeTakenMs = int64(2500)
	const avgSpeed = float64(2 * 1024 * 1024) // 2 MB/s

	if err := store.AddToMasterList(types.DownloadRecord{
		ID:         testID,
		URL:        testURL,
		URLHash:    "dummy-hash",
		DestPath:   testDest,
		Filename:   filepath.Base(testDest),
		Status:     "completed",
		TotalSize:  totalSize,
		Downloaded: totalSize,
		TimeTaken:  timeTakenMs,
		AvgSpeed:   avgSpeed,
	}); err != nil {
		t.Fatal(err)
	}

	bus := orchestrator.NewEventBus()
	mgr := orchestrator.NewLifecycleManager(nil, bus, nil)
	m := InitialRootModel(1700, "test-version", service.NewLocalDownloadService(mgr), mgr, nil, false)

	var found *DownloadModel
	for _, d := range m.downloads {
		if d.ID == testID {
			found = d
			break
		}
	}
	if found == nil {
		t.Fatal("TUI Model failed to load completed download")
		return
	}
	if !found.done {
		t.Error("Expected completed download to be marked done")
	}
	if found.Elapsed != time.Duration(timeTakenMs)*time.Millisecond {
		t.Errorf("Elapsed = %v, want %v", found.Elapsed, time.Duration(timeTakenMs)*time.Millisecond)
	}
	if found.Speed != avgSpeed {
		t.Errorf("Speed = %f, want %f", found.Speed, avgSpeed)
	}
}

func TestTUI_Startup_LoadsErroredDownloadsIntoDoneTab(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-tui-error-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	setupTestEnv(t, tmpDir)

	testID := "tui-error-id"
	testURL := "http://example.com/error.bin"
	testDest := filepath.Join(tmpDir, "error.bin")
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:       testID,
		URL:      testURL,
		DestPath: testDest,
		Filename: filepath.Base(testDest),
		Status:   "error",
		Error:    "connection reset by peer",
	}); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewLifecycleManager(nil, nil, nil)
	m := InitialRootModel(1700, "test-version", service.NewLocalDownloadService(mgr), mgr, nil, false)

	var found *DownloadModel
	for _, d := range m.downloads {
		if d.ID == testID {
			found = d
			break
		}
	}
	if found == nil {
		t.Fatal("TUI Model failed to load errored download")
	}
	if !found.done {
		t.Fatal("expected errored download to appear in done tab")
	}
	if found.err == nil {
		t.Fatal("expected errored download to retain its error")
	}
	if found.err.Error() != "connection reset by peer" {
		t.Errorf("error = %q, want %q", found.err.Error(), "connection reset by peer")
	}
}

func TestTUI_Startup_UsesFallbackForErroredDownloadWithoutMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-tui-error-fallback-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	setupTestEnv(t, tmpDir)

	const testID = "tui-error-fallback-id"
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:       testID,
		Filename: "error.bin",
		Status:   "error",
	}); err != nil {
		t.Fatal(err)
	}

	bus := orchestrator.NewEventBus()
	mgr := orchestrator.NewLifecycleManager(nil, bus, nil)
	m := InitialRootModel(1700, "test-version", service.NewLocalDownloadService(mgr), mgr, nil, false)

	var found *DownloadModel
	for _, d := range m.downloads {
		if d.ID == testID {
			found = d
			break
		}
	}
	if found == nil {
		t.Fatal("TUI Model failed to load errored download")
	}
	if found.err == nil {
		t.Fatal("expected fallback error")
	}
	if found.err.Error() != "download failed" {
		t.Errorf("error = %q, want %q", found.err.Error(), "download failed")
	}
}

// Helper functions (duplicated from cmd/startup_test.go because packages differ)
func setupTestEnv(t *testing.T, tmpDir string) {
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)

	surgeDir := config.GetSurgeDir()
	if err := os.MkdirAll(surgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Setup Settings (AutoResume=false default)
	settings := config.DefaultSettings()
	settings.General.AutoResume.Value = false // Ensure we test that "queued" overrides this
	if err := config.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Configure DB
	_ = testutil.SetupStateDB(t)
}

func seedDownload(t *testing.T, id, url, dest, status string) {
	manualState := &types.DownloadRecord{
		ID:         id,
		URL:        url,
		Filename:   filepath.Base(dest),
		DestPath:   dest,
		TotalSize:  1000,
		Downloaded: 0,
		PausedAt:   0,
		CreatedAt:  time.Now().Unix(),
	}
	if err := store.AddToMasterList(types.DownloadRecord{
		ID:         id,
		URL:        url,
		DestPath:   dest,
		Filename:   filepath.Base(dest),
		Status:     status,
		TotalSize:  1000,
		Downloaded: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(url, dest, manualState); err != nil {
		t.Fatal(err)
	}
}
