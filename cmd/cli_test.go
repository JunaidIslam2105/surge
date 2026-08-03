package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// TestResolveDownloadID_Remote verifies that resolveDownloadID queries the server
func TestResolveDownloadID_Remote(t *testing.T) {
	// 1. Mock Server
	downloads := []types.DownloadStatus{
		{ID: "aabbccdd-1234-5678-90ab-cdef12345678", Filename: "test_remote.zip"},
	}

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			_ = json.NewEncoder(w).Encode(downloads)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Extract port
	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	// 2. Mock active port file

	setupIsolatedCmdState(t)
	resetCommandConnectionState(t)
	saveActivePort(port)
	defer removeActivePort()

	origToken := globalToken
	globalToken = "test-token"
	t.Cleanup(func() { globalToken = origToken })

	// 3. Test resolveDownloadID
	partial := "aabbcc"
	full, err := resolveDownloadID(partial)
	if err != nil {
		t.Fatalf("Failed to resolve ID: %v", err)
	}

	if full != downloads[0].ID {
		t.Errorf("Expected %s, got %s", downloads[0].ID, full)
	}
}

func TestResolveDownloadID_RemoteStillWorksWhenDBUnavailable(t *testing.T) {
	downloads := []types.DownloadStatus{
		{ID: "ddeeff00-1234-5678-90ab-cdef12345678", Filename: "test_remote_db_fail.zip"},
	}

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			_ = json.NewEncoder(w).Encode(downloads)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	origHost := globalHost
	origToken := globalToken
	globalHost = "127.0.0.1:" + portStr
	globalToken = "test-token"
	t.Cleanup(func() {
		globalHost = origHost
		globalToken = origToken
	})

	store.CloseDB()
	store.Configure(filepath.Join(t.TempDir(), "missing", "surge.db")) // Intentionally invalid path

	full, err := resolveDownloadID("ddeeff")
	if err != nil {
		t.Fatalf("resolveDownloadID failed: %v", err)
	}
	if full != downloads[0].ID {
		t.Fatalf("expected %s, got %s", downloads[0].ID, full)
	}
}

func TestResolveDownloadID_StrictRemoteDoesNotFallbackToDBOnRemoteError(t *testing.T) {
	setupIsolatedCmdState(t)

	entry := types.DownloadRecord{
		ID:       "11223344-1234-5678-90ab-cdef12345678",
		Filename: "db-only.bin",
	}
	if err := store.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed db entry: %v", err)
	}

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	origHost := globalHost
	origToken := globalToken
	globalHost = "127.0.0.1:" + portStr
	globalToken = "test-token"
	t.Cleanup(func() {
		globalHost = origHost
		globalToken = origToken
	})

	_, err := resolveDownloadID("112233")
	if err == nil {
		t.Fatal("expected remote list error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to list remote downloads") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveDownloadID_LocalModeFallsBackToDBWhenRemoteListFails(t *testing.T) {
	setupIsolatedCmdState(t)

	entry := types.DownloadRecord{
		ID:       "99aabbcc-1234-5678-90ab-cdef12345678",
		Filename: "fallback.bin",
	}
	if err := store.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed db entry: %v", err)
	}

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	saveActivePort(port)
	t.Cleanup(removeActivePort)

	origToken := globalToken
	globalToken = "test-token"
	t.Cleanup(func() { globalToken = origToken })

	full, err := resolveDownloadID("99aabb")
	if err != nil {
		t.Fatalf("resolveDownloadID failed: %v", err)
	}
	if full != entry.ID {
		t.Fatalf("expected fallback to DB id %s, got %s", entry.ID, full)
	}
}

// TestLsCmd_Alias verify 'l' alias exists
func TestLsCmd_Alias(t *testing.T) {
	found := false
	for _, alias := range lsCmd.Aliases {
		if alias == "l" {
			found = true
			break
		}
	}
	if !found {
		t.Error("lsCmd should have 'l' alias")
	}
}

// TestGetRemoteDownloads verify it parses response
func TestGetRemoteDownloads(t *testing.T) {
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"123","filename":"foo.bin","status":"downloading"}]`))
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	downloads, err := GetRemoteDownloads(fmt.Sprintf("http://127.0.0.1:%d", port), "")
	if err != nil {
		t.Fatalf("Failed to get downloads: %v", err)
	}

	if len(downloads) != 1 {
		t.Fatalf("Expected 1 download, got %d", len(downloads))
	}
	if downloads[0].ID != "123" {
		t.Errorf("Mxpected ID 123, got %s", downloads[0].ID)
	}
}

func TestReadURLsFromFile_ParsesAndFilters(t *testing.T) {
	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "urls.txt")
	content := strings.Join([]string{
		"",
		"   # comment line",
		"https://example.com/a.zip",
		"https://example.com/a.zip", // Duplicate
		"  https://example.com/b.zip#fragment  ",
		"   ",
		"#another-comment",
		"https://example.com/c.zip",
	}, "\n")
	if err := os.WriteFile(urlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write url file: %v", err)
	}

	urls, err := utils.ReadURLsFromFile(urlFile)
	if err != nil {
		t.Fatalf("readURLsFromFile returned error: %v", err)
	}

	want := []string{
		"https://example.com/a.zip",
		"https://example.com/b.zip#fragment",
		"https://example.com/c.zip",
	}
	if len(urls) != len(want) {
		t.Fatalf("expected %d urls, got %d (%v)", len(want), len(urls), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("url[%d] = %q, want %q", i, urls[i], want[i])
		}
	}

	// Test empty / comment-only file
	emptyFile := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte("# just a comment\n\n  "), 0o644); err != nil {
		t.Fatalf("failed to write empty url file: %v", err)
	}
	_, err = utils.ReadURLsFromFile(emptyFile)
	if err == nil {
		t.Fatalf("expected an error for empty/comment-only file, got nil")
	}
}

func TestReadURLsFromFile_WhitespaceAndInlineComments(t *testing.T) {
	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "urls.txt")
	content := strings.Join([]string{
		"https://example.com/a.zip https://example.com/b.zip",
		"https://example.com/c.zip    # inline comment",
		"   ",
		"# full comment",
		"https://example.com/d.zip\thttps://example.com/e.zip",
	}, "\n")
	if err := os.WriteFile(urlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write url file: %v", err)
	}

	urls, err := utils.ReadURLsFromFile(urlFile)
	if err != nil {
		t.Fatalf("readURLsFromFile returned error: %v", err)
	}

	want := []string{
		"https://example.com/a.zip",
		"https://example.com/b.zip",
		"https://example.com/c.zip",
		"https://example.com/d.zip",
		"https://example.com/e.zip",
	}
	if len(urls) != len(want) {
		t.Fatalf("expected %d urls, got %d (%v)", len(want), len(urls), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("url[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestReadURLsFromFile_DedupesTrailingSlashVariants(t *testing.T) {
	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "urls.txt")
	content := strings.Join([]string{
		"https://example.com/file.bin/",
		"https://example.com/file.bin",
		"https://example.com/file.bin///",
		"https://example.com/other.bin",
	}, "\n")
	if err := os.WriteFile(urlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write url file: %v", err)
	}

	urls, err := utils.ReadURLsFromFile(urlFile)
	if err != nil {
		t.Fatalf("ReadURLsFromFile returned error: %v", err)
	}

	want := []string{
		"https://example.com/file.bin/",
		"https://example.com/other.bin",
	}
	if len(urls) != len(want) {
		t.Fatalf("expected %d urls, got %d (%v)", len(want), len(urls), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("url[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestReadURLsFromFile_LongLine(t *testing.T) {
	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "urls.txt")
	longToken := strings.Repeat("a", 70*1024)
	longURL := "https://example.com/" + longToken
	if err := os.WriteFile(urlFile, []byte(longURL+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write url file: %v", err)
	}

	urls, err := utils.ReadURLsFromFile(urlFile)
	if err != nil {
		t.Fatalf("readURLsFromFile returned error for long URL: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 url, got %d", len(urls))
	}
	if urls[0] != longURL {
		t.Fatalf("long URL mismatch")
	}
}

func TestReadURLsFromFile_MissingFile(t *testing.T) {
	_, err := utils.ReadURLsFromFile(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected an error for missing file")
	}
}

func TestServerPIDLifecycle(t *testing.T) {
	setupIsolatedCmdState(t)
	removePID()

	savePID()
	pid := readPID()
	if pid != os.Getpid() {
		t.Fatalf("readPID() = %d, want current pid %d", pid, os.Getpid())
	}

	removePID()
	if got := readPID(); got != 0 {
		t.Fatalf("expected pid=0 after removePID, got %d", got)
	}
}

func TestFormatSize_Table(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 B"},
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "kb", bytes: 1024, want: "1.0 KiB"},
		{name: "kb-fraction", bytes: 1536, want: "1.5 KiB"},
		{name: "mb", bytes: 1024 * 1024, want: "1.0 MiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.FormatBytes(tt.bytes); got != tt.want {
				t.Fatalf("ConvertBytesToHumanReadable(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestPrintDownloadDetail_TextAndJSON(t *testing.T) {
	status := types.DownloadStatus{
		ID:         "aabbccdd-1234-5678-90ab-cdef12345678",
		URL:        "https://example.com/file.zip",
		Filename:   "file.zip",
		Status:     "downloading",
		Progress:   55.5,
		Downloaded: 1110,
		TotalSize:  2000,
		Speed:      2.5 * float64(utils.MiB),
		Error:      "sample error",
	}

	textOut := captureStdout(t, func() {
		printDownloadDetail(status, false)
	})
	if !strings.Contains(textOut, "ID:         "+status.ID) {
		t.Fatalf("expected text output to contain ID, got: %s", textOut)
	}
	if !strings.Contains(textOut, "Speed:      2.5 MiB/s") {
		t.Fatalf("expected text output to contain speed, got: %s", textOut)
	}
	if !strings.Contains(textOut, "Error:      sample error") {
		t.Fatalf("expected text output to contain error, got: %s", textOut)
	}

	jsonOut := captureStdout(t, func() {
		printDownloadDetail(status, true)
	})
	var decoded types.DownloadStatus
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("failed to decode JSON output: %v (out=%q)", err, jsonOut)
	}
	if decoded.ID != status.ID {
		t.Fatalf("decoded id = %q, want %q", decoded.ID, status.ID)
	}
}

func TestAddCmdRunE_ReturnsExpectedErrors(t *testing.T) {
	t.Run("no running server", func(t *testing.T) {
		setupIsolatedCmdState(t)
		resetCommandConnectionState(t)

		if err := addCmd.Flags().Set("batch", ""); err != nil {
			t.Fatalf("failed to clear batch flag: %v", err)
		}

		err := addCmd.RunE(addCmd, []string{"https://example.com/file.zip"})
		if err == nil {
			t.Errorf("expected add command to return an error when no server is running")
			return
		}
		if !strings.Contains(err.Error(), "surge is not running locally") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid batch file", func(t *testing.T) {
		setupIsolatedCmdState(t)
		resetCommandConnectionState(t)

		missingBatchPath := filepath.Join(t.TempDir(), "missing-urls.txt")
		if err := addCmd.Flags().Set("batch", missingBatchPath); err != nil {
			t.Fatalf("failed to set batch flag: %v", err)
		}
		t.Cleanup(func() {
			_ = addCmd.Flags().Set("batch", "")
		})

		err := addCmd.RunE(addCmd, nil)
		if err == nil {
			t.Errorf("expected add command to return an error for a missing batch file")
			return
		}
		if !strings.Contains(err.Error(), "error reading batch file") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestActionCommandsRunE_ReturnNoServerErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "pause",
			run: func() error {
				if err := pauseCmd.Flags().Set("all", "false"); err != nil {
					return err
				}
				return pauseCmd.RunE(pauseCmd, []string{"deadbeef"})
			},
		},
		{
			name: "resume",
			run: func() error {
				if err := resumeCmd.Flags().Set("all", "false"); err != nil {
					return err
				}
				return resumeCmd.RunE(resumeCmd, []string{"deadbeef"})
			},
		},
		{
			name: "rm",
			run: func() error {
				if err := rmCmd.Flags().Set("clean", "false"); err != nil {
					return err
				}
				return rmCmd.RunE(rmCmd, []string{"deadbeef"})
			},
		},
		{
			name: "rm_clean",
			run: func() error {
				if err := rmCmd.Flags().Set("clean", "true"); err != nil {
					return err
				}
				defer func() { _ = rmCmd.Flags().Set("clean", "false") }()
				return rmCmd.RunE(rmCmd, []string{})
			},
		},
		{
			name: "refresh",
			run: func() error {
				return refreshCmd.RunE(refreshCmd, []string{"deadbeef", "https://example.com/new.zip"})
			},
		},
		{
			name: "connect",
			run: func() error {
				return connectCmd.RunE(connectCmd, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupIsolatedCmdState(t)
			resetCommandConnectionState(t)

			err := tt.run()
			if err == nil {
				t.Fatalf("expected %s command to return an error", tt.name)
			}

			want := "surge is not running locally"
			if tt.name == "connect" {
				want = "no local Surge server detected"
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("unexpected %s error: %v", tt.name, err)
			}
		})
	}
}

func TestConnectCmd_HostSourcesBypassLocalAutodetect(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		setup func(t *testing.T)
	}{
		{
			name:  "host flag",
			args:  []string{"connect", "--host", "198.1.1.1:7800"},
			setup: func(_ *testing.T) {},
		},
		{
			name: "SURGE_HOST env",
			args: []string{"connect"},
			setup: func(t *testing.T) {
				t.Setenv("SURGE_HOST", "198.1.1.1:7800")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origSettings := globalSettings
			origPool := GlobalPool
			origProgressCh := GlobalProgressCh
			t.Cleanup(func() {
				_ = executeGlobalShutdown("test teardown")
				globalSettings = origSettings
				GlobalPool = origPool
				GlobalProgressCh = origProgressCh
			})

			setupIsolatedCmdState(t)
			resetCommandConnectionState(t)
			_ = rootCmd.PersistentFlags().Set("host", "")
			t.Cleanup(func() {
				_ = rootCmd.PersistentFlags().Set("host", "")
			})

			tt.setup(t)
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()
			if err == nil {
				t.Fatal("expected connect command to return an error")
			}

			if strings.Contains(err.Error(), "no local Surge server detected") {
				t.Fatalf("connect command ignored configured host source: %v", err)
			}

			if !strings.Contains(err.Error(), "requires authentication") {
				t.Fatalf("expected remote target auth error, got: %v", err)
			}
			if !strings.Contains(err.Error(), "https://198.1.1.1:7800") {
				t.Fatalf("expected remote target path with token error, got: %v", err)
			}
		})
	}
}

func TestActionCommandsRunE_ReturnAmbiguousIDErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "pause",
			run: func() error {
				if err := pauseCmd.Flags().Set("all", "false"); err != nil {
					return err
				}
				return pauseCmd.RunE(pauseCmd, []string{"deadbe"})
			},
		},
		{
			name: "resume",
			run: func() error {
				if err := resumeCmd.Flags().Set("all", "false"); err != nil {
					return err
				}
				return resumeCmd.RunE(resumeCmd, []string{"deadbe"})
			},
		},
		{
			name: "rm",
			run: func() error {
				if err := rmCmd.Flags().Set("clean", "false"); err != nil {
					return err
				}
				return rmCmd.RunE(rmCmd, []string{"deadbe"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupIsolatedCmdState(t)
			resetCommandConnectionState(t)

			server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/list" {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
			var port int
			_, _ = fmt.Sscanf(portStr, "%d", &port)
			saveActivePort(port)
			t.Cleanup(removeActivePort)

			origToken := globalToken
			globalToken = "test-token"
			t.Cleanup(func() { globalToken = origToken })

			entries := []types.DownloadRecord{
				{ID: "deadbeef-1234-5678-90ab-cdef12345678", Filename: "first.bin"},
				{ID: "deadbead-1234-5678-90ab-cdef12345678", Filename: "second.bin"},
			}
			for _, entry := range entries {
				if err := store.AddToMasterList(entry); err != nil {
					t.Fatalf("failed to seed db entry %s: %v", entry.ID, err)
				}
			}

			err := tt.run()
			if err == nil {
				t.Fatalf("expected %s command to return an ambiguous ID error", tt.name)
			}
			if !strings.Contains(err.Error(), "ambiguous ID prefix") {
				t.Fatalf("unexpected %s error: %v", tt.name, err)
			}
		})
	}
}

func TestPrintDownloads_FromDatabase_TableAndJSON(t *testing.T) {
	setupIsolatedCmdState(t)
	removeActivePort()

	entry := types.DownloadRecord{
		ID:         "12345678-1234-1234-1234-1234567890ab",
		URL:        "https://example.com/asset.bin",
		Filename:   "this-is-a-very-long-file-name-that-should-truncate.bin",
		Status:     "downloading",
		Downloaded: 512,
		TotalSize:  1024,
	}
	if err := store.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed db entry: %v", err)
	}

	tableOut := captureStdout(t, func() {
		if err := printDownloads(false, "", "", false); err != nil {
			t.Fatalf("printDownloads table failed: %v", err)
		}
	})
	if !strings.Contains(tableOut, "ID") {
		t.Fatalf("expected table header in output, got: %s", tableOut)
	}
	if !strings.Contains(tableOut, "12345678") {
		t.Fatalf("expected truncated ID in output, got: %s", tableOut)
	}
	if !strings.Contains(tableOut, "...") {
		t.Fatalf("expected truncated filename in output, got: %s", tableOut)
	}
	if !strings.Contains(tableOut, "50.0%") {
		t.Fatalf("expected computed progress in output, got: %s", tableOut)
	}

	jsonOut := captureStdout(t, func() {
		if err := printDownloads(true, "", "", false); err != nil {
			t.Fatalf("printDownloads json failed: %v", err)
		}
	})
	var infos []downloadInfo
	if err := json.Unmarshal([]byte(jsonOut), &infos); err != nil {
		t.Fatalf("failed to decode json output: %v (out=%q)", err, jsonOut)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 json entry, got %d: %+v", len(infos), infos)
	}
	got := infos[0]
	if got.ID != entry.ID || got.Filename != entry.Filename || got.Status != entry.Status || got.Downloaded != entry.Downloaded || got.TotalSize != entry.TotalSize || got.Progress != 50 {
		t.Fatalf("unexpected JSON payload: %+v", infos)
	}
}

func TestPrintDownloads_JSONEmpty(t *testing.T) {
	setupIsolatedCmdState(t)
	removeActivePort()

	out := captureStdout(t, func() {
		if err := printDownloads(true, "", "", false); err != nil {
			t.Fatalf("printDownloads empty json failed: %v", err)
		}
	})
	var infos []any
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		t.Fatalf("failed to parse json output: %v (out=%q)", err, out)
	}
	if len(infos) != 0 {
		t.Fatalf("expected empty json array, got %d entries: %+v", len(infos), infos)
	}
}

func TestPrintDownloads_StrictRemoteEmpty_DoesNotFallbackToDB(t *testing.T) {
	setupIsolatedCmdState(t)

	entry := types.DownloadRecord{
		ID:       "feedface-1234-5678-90ab-cdef12345678",
		Filename: "local-only.bin",
		Status:   "completed",
	}
	if err := store.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed local db entry: %v", err)
	}

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		if err := printDownloads(true, server.URL, "", true); err != nil {
			t.Fatalf("printDownloads strict remote failed: %v", err)
		}
	})
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("expected strict remote empty json array, got %q", strings.TrimSpace(out))
	}
}

func TestShowDownloadDetails_UsesDatabaseFallback(t *testing.T) {
	setupIsolatedCmdState(t)
	removeActivePort()

	entry := types.DownloadRecord{
		ID:         "87654321-1234-1234-1234-1234567890ab",
		URL:        "https://example.com/detail.bin",
		Filename:   "detail.bin",
		Status:     "completed",
		Downloaded: 250,
		TotalSize:  500,
	}
	if err := store.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed db entry: %v", err)
	}

	out := captureStdout(t, func() {
		if err := showDownloadDetails("87654321", true, "", ""); err != nil {
			t.Fatalf("showDownloadDetails fallback failed: %v", err)
		}
	})

	var decoded types.DownloadStatus
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("failed to decode detail json: %v (out=%q)", err, out)
	}
	if decoded.ID != entry.ID {
		t.Fatalf("decoded id = %q, want %q", decoded.ID, entry.ID)
	}
	if decoded.Progress != 50 {
		t.Fatalf("decoded progress = %v, want 50", decoded.Progress)
	}
}

func TestSendToServer_SuccessAndServerError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "success accepted", statusCode: http.StatusAccepted, body: `{"id":"abc"}`},
		{name: "server error", statusCode: http.StatusInternalServerError, body: "boom", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Skipf("tcp4 listener unavailable: %v", err)
			}
			defer func() { _ = ln.Close() }()

			mux := http.NewServeMux()
			mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("expected POST, got %s", r.Method)
				}
				body, _ := io.ReadAll(r.Body)
				if !bytes.Contains(body, []byte(`"url":"https://example.com/file.zip"`)) {
					t.Fatalf("request body missing expected URL: %s", string(body))
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			})

			server := &http.Server{Handler: mux}
			go func() { _ = server.Serve(ln) }()
			t.Cleanup(func() {
				_ = server.Close()
			})

			port := ln.Addr().(*net.TCPAddr).Port
			err = sendToServer("https://example.com/file.zip", nil, "", fmt.Sprintf("http://127.0.0.1:%d", port), "")
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSendToServer_UsesBearerTokenFromEnv(t *testing.T) {
	t.Setenv("SURGE_TOKEN", "env-token-123")

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp4 listener unavailable: %v", err)
	}
	defer func() { _ = ln.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-token-123" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	port := ln.Addr().(*net.TCPAddr).Port
	err = sendToServer("https://example.com/file.zip", nil, "", fmt.Sprintf("http://127.0.0.1:%d", port), resolveLocalToken())
	if err != nil {
		t.Fatalf("expected authenticated request to succeed, got error: %v", err)
	}
}

func TestGetRemoteDownloads_UsesBearerTokenFromEnv(t *testing.T) {
	t.Setenv("SURGE_TOKEN", "env-token-123")

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp4 listener unavailable: %v", err)
	}
	defer func() { _ = ln.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-token-123" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"123","filename":"foo.bin","status":"downloading"}]`))
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	port := ln.Addr().(*net.TCPAddr).Port
	downloads, err := GetRemoteDownloads(fmt.Sprintf("http://127.0.0.1:%d", port), resolveLocalToken())
	if err != nil {
		t.Fatalf("expected authenticated request to succeed, got error: %v", err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
}

func TestGetRemoteDownloads_NonOKAndInvalidJSON(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		}))
		defer server.Close()

		_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)

		_, err := GetRemoteDownloads(fmt.Sprintf("http://127.0.0.1:%d", port), "")
		if err == nil {
			t.Fatal("expected error for non-200 response")
		}
	})

	t.Run("invalid-json", func(t *testing.T) {
		server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid"))
		}))
		defer server.Close()

		_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)

		_, err := GetRemoteDownloads(fmt.Sprintf("http://127.0.0.1:%d", port), "")
		if err == nil {
			t.Fatal("expected json decode error")
		}
	})
}

func TestProcessDownloads_RemoteAndLocal(t *testing.T) {
	t.Run("remote-mode", func(t *testing.T) {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Skipf("tcp4 listener unavailable: %v", err)
		}
		defer func() { _ = ln.Close() }()

		var received int32
		mux := http.NewServeMux()
		mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&received, 1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"ok"}`))
		})
		server := &http.Server{Handler: mux}
		go func() { _ = server.Serve(ln) }()
		t.Cleanup(func() { _ = server.Close() })

		port := ln.Addr().(*net.TCPAddr).Port
		count := processDownloads([]string{
			"https://example.com/a.zip,https://mirror.example.com/a.zip",
			"",
			"https://example.com/b.zip",
		}, "", port)

		if count != 2 {
			t.Fatalf("expected 2 successful remote adds, got %d", count)
		}
		if atomic.LoadInt32(&received) != 2 {
			t.Fatalf("expected 2 remote requests, got %d", received)
		}
	})

	t.Run("local-mode", func(t *testing.T) {
		setupIsolatedCmdState(t)
		atomic.StoreInt32(&activeDownloads, 0)

		GlobalProgressCh = make(chan types.DownloadEvent, 10)
		if GlobalPool != nil {
			GlobalPool.GracefulShutdown()
		}
		tmpPool := scheduler.New(GlobalProgressCh, 2)
		t.Cleanup(func() {
			if tmpPool != nil {
				tmpPool.GracefulShutdown()
			}
		})
		GlobalPool = tmpPool
		eventBus := orchestrator.NewEventBus()
		t.Cleanup(func() { eventBus.Shutdown() })
		getAll := func() []types.DownloadRecord { return GlobalPool.GetAll() }
		tmpLifecycle := orchestrator.NewLifecycleManager(GlobalPool, eventBus, nil, buildActiveDownloadChecker(getAll))
		t.Cleanup(func() { tmpLifecycle.Shutdown() })
		GlobalLifecycle = tmpLifecycle
		GlobalService = service.NewLocalDownloadService(GlobalLifecycle)

		probeServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte("hello"))
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		}))
		defer probeServer.Close()

		count := processDownloads([]string{
			probeServer.URL + "/local.zip",
			"",
		}, t.TempDir(), 0)

		if count != 1 {
			t.Fatalf("expected 1 successful local add, got %d", count)
		}
		if atomic.LoadInt32(&activeDownloads) != 1 {
			t.Fatalf("expected activeDownloads=1, got %d", atomic.LoadInt32(&activeDownloads))
		}
	})
}

func setupIsolatedCmdState(t *testing.T) {
	t.Helper()
	origSettings := globalSettings
	origCheck := checkSystemServiceRunning
	checkSystemServiceRunning = func() bool { return false }

	t.Cleanup(func() {
		globalSettings = origSettings
		checkSystemServiceRunning = origCheck
		resetGlobalShutdownCoordinatorForTest(nil)
	})

	setupXDGEnvIsolation(t)
	resetGlobalShutdownCoordinatorForTest(nil)
	resetGlobalEnqueueContext()

	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	store.CloseDB()
	store.Configure(filepath.Join(config.GetStateDir(), "surge.db"))
}

func resetCommandConnectionState(t *testing.T) {
	t.Helper()
	origHost := globalHost
	origToken := globalToken
	globalHost = ""
	globalToken = ""
	t.Setenv("SURGE_HOST", "")
	t.Setenv("SURGE_TOKEN", "")
	removeActivePort()
	t.Cleanup(func() {
		globalHost = origHost
		globalToken = origToken
		removeActivePort()
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	os.Stdout = old

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout failed: %v", err)
	}
	_ = r.Close()
	return string(data)
}
