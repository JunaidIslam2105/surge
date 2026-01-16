package state

import (
	"testing"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/download/types"
)

func TestURLHash(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantLen int
	}{
		{"simple URL", "https://example.com/file.zip", 16},
		{"URL with path", "https://example.com/path/to/file.zip", 16},
		{"URL with query", "https://example.com/file.zip?token=abc", 16},
		{"different domain", "https://other.org/download", 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := URLHash(tt.url)
			if len(hash) != tt.wantLen {
				t.Errorf("URLHash(%s) length = %d, want %d", tt.url, len(hash), tt.wantLen)
			}
		})
	}
}

func TestURLHashUniqueness(t *testing.T) {
	url1 := "https://example.com/file1.zip"
	url2 := "https://example.com/file2.zip"

	hash1 := URLHash(url1)
	hash2 := URLHash(url2)

	if hash1 == hash2 {
		t.Errorf("Different URLs produced same hash: %s", hash1)
	}
}

func TestURLHashConsistency(t *testing.T) {
	url := "https://example.com/consistent.zip"

	hash1 := URLHash(url)
	hash2 := URLHash(url)

	if hash1 != hash2 {
		t.Errorf("Same URL produced different hashes: %s vs %s", hash1, hash2)
	}
}

func TestStateHashUniqueness(t *testing.T) {
	// Same URL, different destinations should produce different hashes
	url := "https://example.com/file.zip"
	dest1 := "C:\\Downloads\\file.zip"
	dest2 := "C:\\Downloads\\file(1).zip"

	hash1 := StateHash(url, dest1)
	hash2 := StateHash(url, dest2)

	if hash1 == hash2 {
		t.Errorf("Same URL with different destinations produced same StateHash: %s", hash1)
	}

	// Verify StateHash is consistent
	hash3 := StateHash(url, dest1)
	if hash1 != hash3 {
		t.Errorf("Same URL+dest produced different StateHash: %s vs %s", hash1, hash3)
	}
}

func TestSaveLoadState(t *testing.T) {
	// Ensure directories exist
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	testURL := "https://test.example.com/save-load-test.zip"
	testDestPath := "C:\\Downloads\\testfile.zip"
	originalState := &types.DownloadState{
		URL:        testURL,
		DestPath:   testDestPath,
		TotalSize:  1000000,
		Downloaded: 500000,
		Tasks: []types.Task{
			{Offset: 500000, Length: 250000},
			{Offset: 750000, Length: 250000},
		},
		Filename: "save-load-test.zip",
	}

	// Save state
	if err := SaveState(testURL, testDestPath, originalState); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Load state
	loadedState, err := LoadState(testURL, testDestPath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	// Verify fields
	if loadedState.URL != originalState.URL {
		t.Errorf("URL = %s, want %s", loadedState.URL, originalState.URL)
	}
	if loadedState.Downloaded != originalState.Downloaded {
		t.Errorf("Downloaded = %d, want %d", loadedState.Downloaded, originalState.Downloaded)
	}
	if loadedState.TotalSize != originalState.TotalSize {
		t.Errorf("TotalSize = %d, want %d", loadedState.TotalSize, originalState.TotalSize)
	}
	if len(loadedState.Tasks) != len(originalState.Tasks) {
		t.Errorf("Tasks count = %d, want %d", len(loadedState.Tasks), len(originalState.Tasks))
	}
	if loadedState.Filename != originalState.Filename {
		t.Errorf("Filename = %s, want %s", loadedState.Filename, originalState.Filename)
	}

	// Verify hashes were set
	if loadedState.URLHash == "" {
		t.Error("URLHash was not set")
	}
}

func TestDeleteState(t *testing.T) {
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	testURL := "https://test.example.com/delete-test.zip"
	testDestPath := "C:\\Downloads\\delete-test.zip"
	state := &types.DownloadState{
		URL:      testURL,
		DestPath: testDestPath,
		Filename: "delete-test.zip",
	}

	// Save state
	if err := SaveState(testURL, testDestPath, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify it was saved
	if _, err := LoadState(testURL, testDestPath); err != nil {
		t.Fatalf("State was not saved properly: %v", err)
	}

	// Delete state
	if err := DeleteState("test-id", testURL, testDestPath); err != nil {
		t.Fatalf("DeleteState failed: %v", err)
	}

	// Verify it was deleted
	_, err := LoadState(testURL, testDestPath)
	if err == nil {
		t.Error("LoadState should fail after DeleteState")
	}
}

func TestStateOverwrite(t *testing.T) {
	// This tests the scenario: pause at 30%, resume to 80%, pause again
	// The state should reflect 80%, not 30%
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	testURL := "https://test.example.com/overwrite-test.zip"
	testDestPath := "C:\\Downloads\\overwrite-test.zip"

	// First pause at 30%
	state1 := &types.DownloadState{
		URL:        testURL,
		DestPath:   testDestPath,
		TotalSize:  1000000,
		Downloaded: 300000, // 30%
		Tasks:      []types.Task{{Offset: 300000, Length: 700000}},
		Filename:   "overwrite-test.zip",
	}
	if err := SaveState(testURL, testDestPath, state1); err != nil {
		t.Fatalf("First SaveState failed: %v", err)
	}

	// Second pause at 80% (simulating resume + more downloading)
	state2 := &types.DownloadState{
		URL:        testURL,
		DestPath:   testDestPath,
		TotalSize:  1000000,
		Downloaded: 800000, // 80%
		Tasks:      []types.Task{{Offset: 800000, Length: 200000}},
		Filename:   "overwrite-test.zip",
	}
	if err := SaveState(testURL, testDestPath, state2); err != nil {
		t.Fatalf("Second SaveState failed: %v", err)
	}

	// Load and verify it's 80%, not 30%
	loaded, err := LoadState(testURL, testDestPath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.Downloaded != 800000 {
		t.Errorf("Downloaded = %d, want 800000 (state should be overwritten)", loaded.Downloaded)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].Offset != 800000 {
		t.Errorf("Tasks not properly overwritten, got offset %d", loaded.Tasks[0].Offset)
	}

	// Cleanup
	DeleteState("test-id", testURL, testDestPath)
}

// TestDuplicateURLStateIsolation verifies that downloading the same URL multiple times
// creates separate state files for each download (the bug being fixed)
func TestDuplicateURLStateIsolation(t *testing.T) {
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	testURL := "https://example.com/samefile.zip"
	dest1 := "C:\\Downloads\\samefile.zip"
	dest2 := "C:\\Downloads\\samefile(1).zip"
	dest3 := "C:\\Downloads\\samefile(2).zip"

	// Create 3 downloads of the same URL with different destinations
	state1 := &types.DownloadState{
		URL:        testURL,
		DestPath:   dest1,
		TotalSize:  1000000,
		Downloaded: 100000, // 10%
		Tasks:      []types.Task{{Offset: 100000, Length: 900000}},
		Filename:   "samefile.zip",
	}
	state2 := &types.DownloadState{
		URL:        testURL,
		DestPath:   dest2,
		TotalSize:  1000000,
		Downloaded: 500000, // 50%
		Tasks:      []types.Task{{Offset: 500000, Length: 500000}},
		Filename:   "samefile(1).zip",
	}
	state3 := &types.DownloadState{
		URL:        testURL,
		DestPath:   dest3,
		TotalSize:  1000000,
		Downloaded: 900000, // 90%
		Tasks:      []types.Task{{Offset: 900000, Length: 100000}},
		Filename:   "samefile(2).zip",
	}

	// Save all three states (simulating pausing 3 downloads)
	if err := SaveState(testURL, dest1, state1); err != nil {
		t.Fatalf("SaveState 1 failed: %v", err)
	}
	if err := SaveState(testURL, dest2, state2); err != nil {
		t.Fatalf("SaveState 2 failed: %v", err)
	}
	if err := SaveState(testURL, dest3, state3); err != nil {
		t.Fatalf("SaveState 3 failed: %v", err)
	}

	// Load and verify each has its correct state (THE FIX)
	loaded1, err := LoadState(testURL, dest1)
	if err != nil {
		t.Fatalf("LoadState 1 failed: %v", err)
	}
	if loaded1.Downloaded != 100000 {
		t.Errorf("State 1 Downloaded = %d, want 100000", loaded1.Downloaded)
	}
	if loaded1.DestPath != dest1 {
		t.Errorf("State 1 DestPath = %s, want %s", loaded1.DestPath, dest1)
	}

	loaded2, err := LoadState(testURL, dest2)
	if err != nil {
		t.Fatalf("LoadState 2 failed: %v", err)
	}
	if loaded2.Downloaded != 500000 {
		t.Errorf("State 2 Downloaded = %d, want 500000", loaded2.Downloaded)
	}
	if loaded2.DestPath != dest2 {
		t.Errorf("State 2 DestPath = %s, want %s", loaded2.DestPath, dest2)
	}

	loaded3, err := LoadState(testURL, dest3)
	if err != nil {
		t.Fatalf("LoadState 3 failed: %v", err)
	}
	if loaded3.Downloaded != 900000 {
		t.Errorf("State 3 Downloaded = %d, want 900000", loaded3.Downloaded)
	}
	if loaded3.DestPath != dest3 {
		t.Errorf("State 3 DestPath = %s, want %s", loaded3.DestPath, dest3)
	}

	// Cleanup
	DeleteState("id1", testURL, dest1)
	DeleteState("id2", testURL, dest2)
	DeleteState("id3", testURL, dest3)
}
