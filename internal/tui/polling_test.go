package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// TestStateSync verifies that the TUI uses the shared state object
// from the worker, allowing external progress updates to be seen.
func TestStateSync(t *testing.T) {
	m := InitialRootModel(0, "0.0.0", nil, make(chan any))

	downloadID := "external-id"
	// Create the "worker" state - this is the source of truth
	workerState := types.NewProgressState(downloadID, 1000)

	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))

	go func() {
		// Simulate download start (from external source)
		// Current implementation of DownloadStartedMsg doesn't carry state
		// So TUI will create its own state (BUG).
		time.Sleep(100 * time.Millisecond)
		p.Send(events.DownloadStartedMsg{
			DownloadID: downloadID,
			Filename:   "external.file",
			Total:      1000,
			URL:        "http://example.com/external",
			DestPath:   "/tmp/external.file",
			State:      workerState,
		})

		// Simulate worker updating the state
		time.Sleep(200 * time.Millisecond)
		workerState.Downloaded.Store(500)

		time.Sleep(200 * time.Millisecond) // Wait for poll
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
	}

	// Without fix: TUI creates its own state, so Downloaded stays 0
	// With fix: TUI uses workerState, so Downloaded becomes 500
	if target.Downloaded != 500 {
		t.Errorf("State not synced. TUI Downloaded=%d, Worker Downloaded=500", target.Downloaded)
	}
}
