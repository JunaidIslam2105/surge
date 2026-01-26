package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/surge-downloader/surge/internal/engine/events"
)

// TestPollingStartsOnDownloadStarted verifies that receiving a DownloadStartedMsg
// triggers the polling loop, updating the model with progress.
func TestPollingStartsOnDownloadStarted(t *testing.T) {
	// 1. Setup minimal model
	m := InitialRootModel(0, "0.0.0", nil, make(chan any))

	downloadID := "test-id"
	// Manually add a download to simulate "Queued" state
	dm := NewDownloadModel(downloadID, "http://example.com/file", "file.test", 1000)
	m.downloads = append(m.downloads, dm)

	// Pre-fill state with progress so we can verify polling reads it
	expectedDownloaded := int64(500)
	dm.state.Downloaded.Store(expectedDownloaded)

	// 2. Create a headless program
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(nil))

	// 3. Run program in background
	go func() {
		if _, err := p.Run(); err != nil {
			t.Logf("Program finished with error: %v", err)
		}
	}()
	time.Sleep(50 * time.Millisecond) // Wait for init

	// 4. Send DownloadStartedMsg
	// This should trigger the fix (once implemented) to start polling
	p.Send(events.DownloadStartedMsg{
		DownloadID: downloadID,
		Filename:   "file.test",
		Total:      1000,
		URL:        "http://example.com/file",
		DestPath:   "/tmp/file.test",
	})

	// 5. Wait for poll interval (150ms) + buffer
	// We wait enough time for at least one tick to happen
	time.Sleep(300 * time.Millisecond)

	// 6. Kill program (this might be hard to sync, but we just need to check state)
	p.Quit()

	// Since p.Run() blocks, we need to inspect the final model.
	// But tea.Run() returns the final model!
	// Let's restructure to capture it.
}

func TestPollingStartsOnDownloadStarted_CaptureModel(t *testing.T) {
	m := InitialRootModel(0, "0.0.0", nil, make(chan any))

	downloadID := "test-id"
	dm := NewDownloadModel(downloadID, "http://example.com/file", "file.test", 1000)
	m.downloads = append(m.downloads, dm)

	expectedDownloaded := int64(500)
	dm.state.Downloaded.Store(expectedDownloaded)

	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))

	go func() {
		time.Sleep(100 * time.Millisecond)
		p.Send(events.DownloadStartedMsg{
			DownloadID: downloadID,
			Filename:   "file.test",
			Total:      1000,
			URL:        "http://example.com/file",
			DestPath:   "/tmp/file.test",
		})
		time.Sleep(400 * time.Millisecond) // Wait for poll
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
		t.Fatal("Download model lost")
	}

	// Without fix: polling never runs, so target.Downloaded remains 0 (or whatever it was init with)
	// NewDownloadModel inits Downloaded to 0.
	// With fix: polling runs, reads 500 from state, updates target.Downloaded to 500.

	if target.Downloaded != expectedDownloaded {
		t.Errorf("Polling did not update status. Got Downloaded=%d, want %d. (Fix likely missing)", target.Downloaded, expectedDownloaded)
	}
}
