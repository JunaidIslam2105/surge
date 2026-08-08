package tui

import (
	"testing"

	"github.com/SurgeDM/Surge/internal/config"
)

func TestHandleClipboardPaste_StandardURL(t *testing.T) {
	m := RootModel{
		inputs:   newInputModels(),
		Settings: config.DefaultSettings(),
	}

	updated, _ := m.handleClipboardPaste("https://example.com/file.zip")
	m2 := updated.(RootModel)

	if m2.state != InputState {
		t.Fatalf("expected InputState, got %v", m2.state)
	}
	if m2.hideMirrors {
		t.Fatal("expected hideMirrors=false for standard URL")
	}
	if m2.inputs[0].Value() != "https://example.com/file.zip" {
		t.Fatalf("expected url to be set, got %v", m2.inputs[0].Value())
	}
}

func TestHandleClipboardPaste_CurlCommand(t *testing.T) {
	m := RootModel{
		inputs:   newInputModels(),
		Settings: config.DefaultSettings(),
	}

	curlCmd := `curl 'https://example.com/file.zip' -H 'User-Agent: test'`
	updated, _ := m.handleClipboardPaste(curlCmd)
	m2 := updated.(RootModel)

	if m2.state != InputState {
		t.Fatalf("expected InputState, got %v", m2.state)
	}
	if !m2.hideMirrors {
		t.Fatal("expected hideMirrors=true for curl command")
	}
	if m2.inputs[0].Value() != "https://example.com/file.zip" {
		t.Fatalf("expected url to be set, got %v", m2.inputs[0].Value())
	}
	if m2.pendingHeaders == nil || m2.pendingHeaders["User-Agent"] != "test" {
		t.Fatalf("expected headers to be set, got %v", m2.pendingHeaders)
	}
}
