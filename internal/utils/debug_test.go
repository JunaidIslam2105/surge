package utils_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/utils"
)

func TestDebug_CreatesLogFile(t *testing.T) {
	logsDir := t.TempDir()
	utils.CloseDebug()
	utils.ConfigureDebug(logsDir)
	t.Cleanup(utils.CloseDebug)

	// Call Debug
	utils.Debug("Test message from unit test")

	// Check if any debug log file was created
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("Failed to read logs directory: %v", err)
	}

	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "debug-") && strings.HasSuffix(entry.Name(), ".log") {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("Debug did not create a log file")
	}
}

func TestDebug_FormatsMessage(t *testing.T) {
	// Test that Debug can handle format strings with arguments
	// This shouldn't panic
	utils.Debug("Test message with %s and %d", "string", 42)
	utils.Debug("Simple message without formatting")
	utils.Debug("Message with special chars: %% \\n \\t")
}

func TestDebug_HandlesEmptyMessage(t *testing.T) {
	// Debug should handle empty messages gracefully
	utils.Debug("")
	utils.Debug("   ")
}

func TestDebug_MultipleArguments(t *testing.T) {
	// Test with various argument types
	utils.Debug("int: %d, float: %f, string: %s, bool: %t", 42, 3.14, "hello", true)
	utils.Debug("Multiple strings: %s %s %s", "one", "two", "three")
}

func TestLogFilePath(t *testing.T) {
	// Verify logs directory path is valid
	logsDir := config.GetLogsDir()

	if logsDir == "" {
		t.Error("GetLogsDir returned empty string")
	}

	// Path should contain expected directory name
	if !strings.Contains(strings.ToLower(logsDir), "surge") {
		t.Errorf("Logs directory should be under surge config, got: %s", logsDir)
	}

	if !strings.HasSuffix(logsDir, "logs") {
		t.Errorf("Logs directory should end with 'logs', got: %s", logsDir)
	}

	// Should be a valid path format
	if !filepath.IsAbs(logsDir) {
		t.Errorf("Logs directory should be absolute path, got: %s", logsDir)
	}
}

func TestCleanupLogs(t *testing.T) {
	// Use a temporary directory for this test
	tempDir, err := os.MkdirTemp("", "surge-logs-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Configure debug to use this temp dir
	utils.ConfigureDebug(tempDir)

	// Reset configuration after test (though this changes global state, so might affect other tests potentially)
	// But in these unit tests, parallelism isn't enabled by default.
	defer utils.ConfigureDebug(config.GetLogsDir())

	// Create 10 dummy log files
	baseTime := time.Now()
	for i := 0; i < 10; i++ {
		// Use file name format matching debug.go: debug-YYYYMMDD-HHMMSS.log
		// We add 'i' to time to ensure uniqueness and order
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		filename := fmt.Sprintf("debug-%s.log", ts.Format("20060102-150405"))
		path := filepath.Join(tempDir, filename)

		err := os.WriteFile(path, []byte("dummy log"), 0o644)
		if err != nil {
			t.Fatalf("Failed to write dummy log: %v", err)
		}
	}

	// Verify we created 10
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("Expected 10 files, got %d", len(entries))
	}

	// Test cleanup: Keep 5
	utils.CleanupLogs(5)

	// Verify we have 5 left
	entries, err = os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read dir after cleanup: %v", err)
	}

	if len(entries) != 5 {
		// For debugging failure
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("Expected 5 files, got %d. Files: %v", len(entries), names)
	}

	// Verify we kept the NEWEST ones (indices 5, 6, 7, 8, 9 from loop)
	// The file created with i=9 should be present
	newestTS := baseTime.Add(9 * time.Hour).Format("20060102-150405")
	expectedName := fmt.Sprintf("debug-%s.log", newestTS)
	found := false
	for _, e := range entries {
		if e.Name() == expectedName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected newest file %s to be present, but it was not", expectedName)
	}
}
