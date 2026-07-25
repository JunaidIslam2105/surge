package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SurgeDM/Surge/internal/config"
)

func TestFilepickerJump(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fp-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	files := []string{"apple.txt", "banana.txt", "cat.txt", "cherry.txt", "dog.txt"}
	for _, f := range files {
		os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644)
	}

	m := RootModel{
		Settings:   config.DefaultSettings(),
		filepicker: newFilepicker(tmpDir),
	}
	m.filepicker.SetHeight(20)

	// filepicker.Init() returns a Cmd that we need to execute to populate the files
	cmd := m.filepicker.Init()
	if cmd != nil {
		msg := cmd()
		m.filepicker, _ = m.filepicker.Update(msg)
	}

	// Helper to check current file
	checkCurrent := func(expected string) {
		t.Helper()
		base := filepath.Base(m.filepicker.HighlightedPath())
		if base != expected {
			t.Errorf("Expected %s, got %s", expected, base)
		}
	}

	// Depending on OS and filepicker sort, it might not be 'apple.txt' first if there are hidden files, etc.
	// But in a fresh temp dir, it should be apple.txt
	checkCurrent("apple.txt")

	// Jump to 'c'
	m, handled := m.handleFilepickerJump("c")
	if !handled {
		t.Error("Expected jump to 'c' to be handled")
	}
	checkCurrent("cat.txt")

	// Jump to 'c' again (should go to cherry.txt)
	m, handled = m.handleFilepickerJump("c")
	if !handled {
		t.Error("Expected second jump to 'c' to be handled")
	}
	checkCurrent("cherry.txt")

	// Jump to 'c' again (should wrap around to cat.txt)
	m, handled = m.handleFilepickerJump("c")
	if !handled {
		t.Error("Expected wrap around jump to 'c' to be handled")
	}
	checkCurrent("cat.txt")

	// Jump to 'd'
	m, handled = m.handleFilepickerJump("d")
	if !handled {
		t.Error("Expected jump to 'd' to be handled")
	}
	checkCurrent("dog.txt")

	// Jump to 'z' (not found)
	m, handled = m.handleFilepickerJump("z")
	if handled {
		t.Error("Expected jump to 'z' to not be handled")
	}
	// Cursor should remain at dog.txt
	checkCurrent("dog.txt")
}
