package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/types"
)

type mockService struct {
	deleteErr error
	deletedID string
}

func (m *mockService) Delete(id string) error {
	m.deletedID = id
	return m.deleteErr
}

func (m *mockService) Purge(id string) error {
	return m.Delete(id)
}

func (m *mockService) List() ([]types.DownloadStatus, error)    { return nil, nil }
func (m *mockService) History() ([]types.DownloadRecord, error) { return nil, nil }
func (m *mockService) Add(url string, path string, filename string, mirrors []string, headers map[string]string, isExplicitCategory bool, workers int, minChunkSize int64) (string, error) {
	return "", nil
}
func (m *mockService) AddWithID(url string, path string, filename string, mirrors []string, headers map[string]string, id string, isExplicitCategory bool, workers int, minChunkSize int64) (string, error) {
	return "", nil
}
func (m *mockService) ResumeBatch(ids []string) []error { return nil }
func (m *mockService) StreamEvents(ctx context.Context) (<-chan types.DownloadEvent, func(), error) {
	return nil, nil, nil
}
func (m *mockService) Publish(msg types.DownloadEvent) error              { return nil }
func (m *mockService) Pause(id string) error                              { return nil }
func (m *mockService) Resume(id string) error                             { return nil }
func (m *mockService) UpdateURL(id string, newURL string) error           { return nil }
func (m *mockService) GetStatus(id string) (*types.DownloadStatus, error) { return nil, nil }
func (m *mockService) Shutdown() error                                    { return nil }
func (m *mockService) ClearCompleted() (int64, error) {
	return 0, nil
}
func (m *mockService) ClearFailed() (int64, error) {
	return 0, nil
}
func (m *mockService) SetRateLimit(id string, rate int64) error { return nil }
func (m *mockService) ClearRateLimit(id string) error           { return nil }

func TestUpdateDashboard_DeleteResilience(t *testing.T) {
	// This test validates the TUI's defensive layer independently of the service
	// implementation. Even though Service.Delete currently returns nil for missing
	// IDs, the TUI should still gracefully handle ErrNotFound if it occurs.
	dm := &DownloadModel{ID: "ghost-id", Filename: "ghost.zip", done: true}
	svc := &mockService{deleteErr: types.ErrNotFound}

	m := RootModel{
		state:     DashboardState,
		activeTab: TabDone,
		downloads: []*DownloadModel{dm},
		Service:   svc,
		keys:      config.DefaultKeyMap(),
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(0) // Select the ghost download

	// Simulate pressing 'x' (Delete)
	msg := tea.KeyPressMsg{Code: 'x', Text: "x"}
	updated, _ := m.updateDashboard(msg)
	m2 := updated.(RootModel)

	if len(m2.downloads) != 0 {
		t.Errorf("Expected download to be removed even on 'not found' error, but %d entries remain", len(m2.downloads))
	}
	if svc.deletedID != "ghost-id" {
		t.Errorf("Expected Service.Delete to be called with 'ghost-id', got %q", svc.deletedID)
	}
}

func TestUpdateDashboard_DeleteSuccess(t *testing.T) {
	dm := &DownloadModel{ID: "real-id", Filename: "real.zip", done: true}
	svc := &mockService{deleteErr: nil}

	m := RootModel{
		state:     DashboardState,
		activeTab: TabDone,
		downloads: []*DownloadModel{dm},
		Service:   svc,
		keys:      config.DefaultKeyMap(),
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(0)

	msg := tea.KeyPressMsg{Code: 'x', Text: "x"}
	updated, _ := m.updateDashboard(msg)
	m2 := updated.(RootModel)

	if len(m2.downloads) != 0 {
		t.Errorf("Expected download to be removed on success, but %d entries remain", len(m2.downloads))
	}
}

func TestUpdateListItems_SelectsNewLastItemAfterSelectedLastIsRemoved(t *testing.T) {
	first := &DownloadModel{ID: "first-id", Filename: "first.zip", done: true}
	last := &DownloadModel{ID: "last-id", Filename: "last.zip", done: true}
	m := RootModel{
		activeTab: TabDone,
		downloads: []*DownloadModel{first, last},
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(1)

	m.removeDownloadByID(last.ID)
	m.UpdateListItems()

	if got := m.list.Index(); got != 0 {
		t.Fatalf("list index after removing selected last item = %d, want 0", got)
	}
}

func TestUpdateDashboard_DeleteOtherError(t *testing.T) {
	dm := &DownloadModel{ID: "error-id", Filename: "error.zip", done: true}
	svc := &mockService{deleteErr: errors.New("some other error")}

	m := RootModel{
		state:     DashboardState,
		activeTab: TabDone,
		downloads: []*DownloadModel{dm},
		Service:   svc,
		keys:      config.DefaultKeyMap(),
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(0)

	msg := tea.KeyPressMsg{Code: 'x', Text: "x"}
	updated, _ := m.updateDashboard(msg)
	m2 := updated.(RootModel)

	if len(m2.downloads) != 1 {
		t.Errorf("Expected download to REMAIN on non-not-found error, but %d entries remain", len(m2.downloads))
	}
}

func TestUpdateDashboard_DeletePausedDownloadPromptsConfirmation(t *testing.T) {
	dm := &DownloadModel{ID: "paused-id", Filename: "paused.zip", paused: true}
	svc := &mockService{}

	m := RootModel{
		state:     DashboardState,
		downloads: []*DownloadModel{dm},
		Service:   svc,
		keys:      config.DefaultKeyMap(),
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(0)

	updated, _ := m.updateDashboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m2 := updated.(RootModel)

	if m2.state != RemoveConfirmState {
		t.Fatalf("expected remove confirmation state, got %v", m2.state)
	}
	if m2.removeTargetID != "paused-id" {
		t.Fatalf("removeTargetID = %q, want paused-id", m2.removeTargetID)
	}
	if svc.deletedID != "" {
		t.Fatalf("expected delete not to run before confirmation, got %q", svc.deletedID)
	}
	if len(m2.downloads) != 1 {
		t.Fatalf("expected download to remain before confirmation, got %d", len(m2.downloads))
	}
}

func TestUpdateDashboard_DeleteActiveDownloadPromptsConfirmation(t *testing.T) {
	dm := &DownloadModel{ID: "active-id", Filename: "active.zip", started: true, Speed: 1024}
	svc := &mockService{}

	m := RootModel{
		state:     DashboardState,
		activeTab: TabActive,
		downloads: []*DownloadModel{dm},
		Service:   svc,
		keys:      config.DefaultKeyMap(),
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(0)

	updated, _ := m.updateDashboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m2 := updated.(RootModel)

	if m2.state != RemoveConfirmState {
		t.Fatalf("expected remove confirmation state, got %v", m2.state)
	}
	if svc.deletedID != "" {
		t.Fatalf("expected delete not to run before confirmation, got %q", svc.deletedID)
	}
}

func TestUpdateDashboard_DeleteErroredDownloadPromptsConfirmation(t *testing.T) {
	dm := &DownloadModel{ID: "error-id", Filename: "partial.zip", done: true, err: errors.New("network failed")}
	svc := &mockService{}

	m := RootModel{
		state:     DashboardState,
		activeTab: TabDone,
		downloads: []*DownloadModel{dm},
		Service:   svc,
		keys:      config.DefaultKeyMap(),
		list:      NewDownloadList(80, 20),
	}
	m.UpdateListItems()
	m.list.Select(0)

	updated, _ := m.updateDashboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m2 := updated.(RootModel)

	if m2.state != RemoveConfirmState {
		t.Fatalf("expected remove confirmation state, got %v", m2.state)
	}
	if m2.removeTargetID != "error-id" {
		t.Fatalf("removeTargetID = %q, want error-id", m2.removeTargetID)
	}
	if svc.deletedID != "" {
		t.Fatalf("expected delete not to run before confirmation, got %q", svc.deletedID)
	}
}

func TestRemoveConfirm_QueuesIncomingSingleRequest(t *testing.T) {
	m := RootModel{
		state:          RemoveConfirmState,
		removeTargetID: "paused-id",
		Settings:       config.DefaultSettings(),
		keys:           config.DefaultKeyMap(),
		list:           NewDownloadList(80, 20),
	}

	updated, _ := m.handleDownloadEvent(types.DownloadEvent{
		Type:     types.EventRequest,
		URL:      "https://example.com/new.zip",
		Filename: "new.zip",
		Path:     "/tmp/downloads",
	})
	m2 := updated.(RootModel)

	if m2.state != RemoveConfirmState {
		t.Fatalf("expected remove confirmation state to remain, got %v", m2.state)
	}
	if m2.removeTargetID != "paused-id" {
		t.Fatalf("removeTargetID = %q, want paused-id", m2.removeTargetID)
	}
	if len(m2.pendingRequestQueue) != 1 || m2.pendingRequestQueue[0].URL != "https://example.com/new.zip" {
		t.Fatalf("expected incoming request queued, got %#v", m2.pendingRequestQueue)
	}
}

func TestRemoveConfirm_QueuesIncomingBatchRequest(t *testing.T) {
	m := RootModel{
		state:          RemoveConfirmState,
		removeTargetID: "paused-id",
		Settings:       config.DefaultSettings(),
		keys:           config.DefaultKeyMap(),
		list:           NewDownloadList(80, 20),
	}

	updated, _ := m.handleDownloadEvent(types.DownloadEvent{
		Type: types.EventBatchRequest,
		Path: "/tmp/downloads",
		BatchEvents: []types.DownloadEvent{
			{Type: types.EventRequest, URL: "https://example.com/one.zip"},
		},
	})
	m2 := updated.(RootModel)

	if m2.state != RemoveConfirmState {
		t.Fatalf("expected remove confirmation state to remain, got %v", m2.state)
	}
	if len(m2.pendingBatchRequestQueue) != 1 {
		t.Fatalf("expected incoming batch request queued, got %d", len(m2.pendingBatchRequestQueue))
	}
}

func TestRemoveConfirm_CancelShowsNextQueuedRequest(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Extension.ExtensionPrompt.Value = true
	dm := &DownloadModel{ID: "paused-id", Filename: "paused.zip", paused: true}

	m := RootModel{
		state:          RemoveConfirmState,
		removeTargetID: "paused-id",
		downloads:      []*DownloadModel{dm},
		Settings:       settings,
		keys:           config.DefaultKeyMap(),
		inputs:         newInputModels(),
		list:           NewDownloadList(80, 20),
		pendingRequestQueue: []types.DownloadEvent{
			{Type: types.EventRequest, URL: "https://example.com/new.zip", Filename: "new.zip", Path: "/tmp/downloads"},
		},
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m2 := updated.(RootModel)

	if m2.state != ExtensionConfirmationState {
		t.Fatalf("expected queued request to open extension confirmation, got %v", m2.state)
	}
	if m2.removeTargetID != "" {
		t.Fatalf("expected removeTargetID cleared, got %q", m2.removeTargetID)
	}
	if len(m2.pendingRequestQueue) != 0 {
		t.Fatalf("expected queued request consumed, got %d", len(m2.pendingRequestQueue))
	}
	if m2.pendingURL != "https://example.com/new.zip" {
		t.Fatalf("pendingURL = %q, want queued request URL", m2.pendingURL)
	}
	if len(m2.downloads) != 1 {
		t.Fatalf("expected cancelled remove to keep download, got %d", len(m2.downloads))
	}
}

func TestRemoveConfirm_ConfirmShowsNextQueuedBatchRequest(t *testing.T) {
	dm := &DownloadModel{ID: "paused-id", Filename: "paused.zip", paused: true}
	svc := &mockService{}

	m := RootModel{
		state:          RemoveConfirmState,
		removeTargetID: "paused-id",
		downloads:      []*DownloadModel{dm},
		Service:        svc,
		Settings:       config.DefaultSettings(),
		keys:           config.DefaultKeyMap(),
		inputs:         newInputModels(),
		list:           NewDownloadList(80, 20),
		pendingBatchRequestQueue: []types.DownloadEvent{
			{
				Type: types.EventBatchRequest,
				Path: "/tmp/downloads",
				BatchEvents: []types.DownloadEvent{
					{Type: types.EventRequest, URL: "https://example.com/one.zip"},
				},
			},
		},
	}
	m.UpdateListItems()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m2 := updated.(RootModel)

	if m2.state != BatchConfirmState {
		t.Fatalf("expected queued batch to open batch confirmation, got %v", m2.state)
	}
	if svc.deletedID != "paused-id" {
		t.Fatalf("expected Service.Delete paused-id, got %q", svc.deletedID)
	}
	if len(m2.downloads) != 0 {
		t.Fatalf("expected confirmed remove to delete download, got %d", len(m2.downloads))
	}
	if len(m2.pendingBatchRequestQueue) != 0 {
		t.Fatalf("expected queued batch consumed, got %d", len(m2.pendingBatchRequestQueue))
	}
	if len(m2.pendingBatchRequests) != 1 || m2.pendingBatchRequests[0].URL != "https://example.com/one.zip" {
		t.Fatalf("expected pending batch request loaded, got %#v", m2.pendingBatchRequests)
	}
}

func TestRemoveConfirm_CancelKeepsDownload(t *testing.T) {
	dm := &DownloadModel{ID: "paused-id", Filename: "paused.zip", paused: true}
	svc := &mockService{}

	m := RootModel{
		state:          RemoveConfirmState,
		removeTargetID: "paused-id",
		downloads:      []*DownloadModel{dm},
		Service:        svc,
		keys:           config.DefaultKeyMap(),
		list:           NewDownloadList(80, 20),
	}
	m.UpdateListItems()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m2 := updated.(RootModel)

	if m2.state != DashboardState {
		t.Fatalf("expected dashboard state after cancel, got %v", m2.state)
	}
	if len(m2.downloads) != 1 {
		t.Fatalf("expected download to remain after cancel, got %d", len(m2.downloads))
	}
	if svc.deletedID != "" {
		t.Fatalf("expected delete not to run after cancel, got %q", svc.deletedID)
	}
}

func TestRemoveConfirm_ConfirmDeletesDownload(t *testing.T) {
	dm := &DownloadModel{ID: "paused-id", Filename: "paused.zip", paused: true}
	svc := &mockService{}

	m := RootModel{
		state:          RemoveConfirmState,
		removeTargetID: "paused-id",
		downloads:      []*DownloadModel{dm},
		Service:        svc,
		keys:           config.DefaultKeyMap(),
		list:           NewDownloadList(80, 20),
	}
	m.UpdateListItems()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m2 := updated.(RootModel)

	if m2.state != DashboardState {
		t.Fatalf("expected dashboard state after confirm, got %v", m2.state)
	}
	if len(m2.downloads) != 0 {
		t.Fatalf("expected download to be removed after confirm, got %d", len(m2.downloads))
	}
	if svc.deletedID != "paused-id" {
		t.Fatalf("expected Service.Delete paused-id, got %q", svc.deletedID)
	}
}
