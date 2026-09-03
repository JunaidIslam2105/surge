package tui

import (
	"github.com/SurgeDM/Surge/internal/orchestrator"
	engineprogress "github.com/SurgeDM/Surge/internal/progress"

	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

// TestStateSync verifies that the TUI uses the shared state object
// from the worker, allowing external progress updates to be seen.
func TestStateSync(t *testing.T) {
	_ = testutil.SetupStateDB(t)

	// Initialize model with progress channel and service
	bus := orchestrator.NewEventBus()
	mgr := orchestrator.NewLifecycleManager(nil, bus, nil)
	m := InitialRootModel(1700, "dev", service.NewLocalDownloadService(mgr), mgr, nil, false)

	downloadID := "external-id"
	// Create the "worker" state - this is the source of truth
	workerState := engineprogress.New(downloadID, 1000)

	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))

	go func() {
		// Simulate download start (from external source)
		// Current implementation of DownloadStartedMsg doesn't carry state
		// So TUI will create its own state (BUG).
		time.Sleep(200 * time.Millisecond)
		p.Send(types.DownloadEvent{
			Type:       types.EventStarted,
			DownloadID: downloadID,
			Filename:   "external.file",
			Total:      1000,
			URL:        "http://example.com/external",
			DestPath:   "/tmp/external.file",
			State:      &types.DownloadRecord{ProgressState: workerState},
		})

		// Simulate worker updating the state -> Send Progress Event
		// Note: The ProgressReporter reads from VerifiedProgress (via GetProgress)
		time.Sleep(300 * time.Millisecond)
		workerState.Bytes.VerifiedProgress.Store(500)
		p.Send(types.DownloadEvent{
			Type:       types.EventProgress,
			DownloadID: downloadID,
			Downloaded: 500,
			Total:      1000,
			Speed:      100, // Dummy speed
			Elapsed:    10 * time.Second,
		})

		// Wait effectively for 2 poll cycles (150ms * 2 = 300ms) + buffer
		time.Sleep(500 * time.Millisecond)
		p.Quit()
	}()

	finalModel, err := p.Run()
	if err != nil {
		t.Fatalf("Program failed: %v", err)
	}

	finalRoot := finalModel.(RootModel)
	var target *DownloadModel
	for _, d := range finalRoot.downloads {
		if d.ID == downloadID {
			target = d
			break
		}
	}

	if target == nil {
		t.Fatal("Download model not found")
		return
	}

	// Without fix: TUI creates its own state, so Downloaded stays 0
	// With fix: TUI uses workerState, so Downloaded becomes 500
	if target.Downloaded != 500 {
		t.Errorf("State not synced. TUI Downloaded=%d, Worker Downloaded=500", target.Downloaded)
	}
}
