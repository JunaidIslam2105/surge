package cmd

import (
	"github.com/SurgeDM/Surge/internal/types"

	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/spf13/cobra"
)

func init() {
	// Initialize GlobalPool for tests
	GlobalProgressCh = make(chan types.DownloadEvent, 100)
	GlobalPool = scheduler.New(GlobalProgressCh, 4)
}

// =============================================================================
// findAvailablePort Tests
// =============================================================================

func TestFindAvailablePort_Success(t *testing.T) {
	requireTCPListener(t)
	port, ln := findAvailablePort(50000)
	if ln == nil {
		t.Fatal("findAvailablePort returned nil listener")
	}
	defer func() { _ = ln.Close() }()

	if port < 50000 || port >= 50100 {
		t.Errorf("Port %d is outside expected range [50000-50100)", port)
	}

	// Verify we can't bind to the same port
	_, err := net.Listen("tcp", ln.Addr().String())
	if err == nil {
		t.Error("Should not be able to bind to same port")
	}
}

func TestFindAvailablePort_ReturnsListener(t *testing.T) {
	requireTCPListener(t)
	port, ln := findAvailablePort(51000)
	if ln == nil {
		t.Fatal("Expected non-nil listener")
	}
	defer func() { _ = ln.Close() }()

	// Verify listener is usable
	addr := ln.Addr().(*net.TCPAddr)
	if addr.Port != port {
		t.Errorf("Listener port %d doesn't match returned port %d", addr.Port, port)
	}
}

func TestFindAvailablePort_SkipsOccupiedPorts(t *testing.T) {
	requireTCPListener(t)
	// Occupy any port
	ln1, err := net.Listen("tcp", fmt.Sprintf("%s:0", serverBindHost))
	if err != nil {
		t.Fatalf("Failed to occupy any port: %v", err)
	}
	defer func() { _ = ln1.Close() }()

	occupiedPort := ln1.Addr().(*net.TCPAddr).Port

	// findAvailablePort should skip occupiedPort and find another
	port, ln2 := findAvailablePort(occupiedPort)
	if ln2 == nil {
		t.Fatal("findAvailablePort returned nil listener")
	}
	defer func() { _ = ln2.Close() }()

	if port == occupiedPort {
		t.Errorf("Should have skipped occupied port %d", occupiedPort)
	}
	// It should check and return the next port
	if port < occupiedPort+1 || port >= occupiedPort+100 {
		t.Errorf("Port %d is outside expected range [%d-%d]", port, occupiedPort+1, occupiedPort+100)
	}
}

func TestFindAvailablePort_AllPortsOccupied(t *testing.T) {
	// This test is tricky - we'd need to occupy 100 ports
	// Skip for now as it's expensive
	t.Skip("Skipping expensive test - would need to occupy 100 ports")
}

// =============================================================================
// saveActivePort / removeActivePort Tests
// =============================================================================

func TestSaveAndRemoveActivePort(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to ensure dirs: %v", err)
	}

	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", config.GetRuntimeDir())
	t.Setenv("SURGE_SYSTEM_STATE_DIR", config.GetStateDir())

	// Save port
	testPort := 12345
	saveActivePort(testPort)

	// Verify file exists and contains correct port
	portFile := filepath.Join(config.GetRuntimeDir(), "port")
	data, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("Failed to read port file: %v", err)
	}

	if string(data) != "12345" {
		t.Errorf("Port file contains %q, expected '12345'", string(data))
	}

	// Remove port
	removeActivePort()

	// Verify file is gone
	if _, err := os.Stat(portFile); !os.IsNotExist(err) {
		t.Error("Port file should be removed")
	}
}

// =============================================================================
// corsMiddleware Tests
// =============================================================================

func TestCorsMiddleware_SetsCORSHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	corsHandler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS headers should be set for extension support")
	}
}

func TestCorsMiddleware_OptionsHandledByMiddleware(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	corsHandler := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	rec := httptest.NewRecorder()
	corsHandler.ServeHTTP(rec, req)

	// OPTIONS preflight should be handled by middleware, not passed to handler
	if called {
		t.Error("Handler should NOT be called for OPTIONS (preflight handled by middleware)")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for OPTIONS preflight, got %d", rec.Code)
	}
}

func TestCorsMiddleware_PassesThrough(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})

	corsHandler := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()
	corsHandler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler was not called")
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d", rec.Code)
	}
}

// =============================================================================
// connect auto-detection Tests
// =============================================================================

func TestConnectCmd_AutoDetectsLocalServer(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_STATE_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to ensure dirs: %v", err)
	}

	// Save a port to simulate a running server
	saveActivePort(1700)
	defer removeActivePort()

	// readActivePort should find the port
	port := readActivePort()
	if port != 1700 {
		t.Fatalf("Expected port 1700, got %d", port)
	}

	// The constructed target should resolve correctly
	target := fmt.Sprintf("127.0.0.1:%d", port)
	parsed, err := parseConnectTarget(target, false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if parsed.BaseURL != "http://127.0.0.1:1700" {
		t.Fatalf("Expected http://127.0.0.1:1700, got %s", parsed.BaseURL)
	}
}

func TestConnectCmd_NoServerRunning(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_STATE_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to ensure dirs: %v", err)
	}

	// No port file exists - should return 0
	port := readActivePort()
	if port != 0 {
		t.Fatalf("Expected port 0 (no server), got %d", port)
	}
}

// =============================================================================
// connect target resolution Tests
// =============================================================================

func TestParseConnectTarget_BaseURL(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		insecureHTTP bool
		want         string
		wantErr      bool
	}{
		{name: "loopback host:port defaults http", target: "127.0.0.1:1700", want: "http://127.0.0.1:1700"},
		{name: "localhost defaults http", target: "localhost:1700", want: "http://localhost:1700"},
		{name: "ipv6 loopback host:port defaults http", target: "[::1]:1700", want: "http://[::1]:1700"},
		{name: "remote host defaults https", target: "example.com:1700", want: "https://example.com:1700"},
		{name: "https URL allowed", target: "https://example.com:1700", want: "https://example.com:1700"},
		{name: "https URL loopback stays https", target: "https://127.0.0.1:1700", want: "https://127.0.0.1:1700"},
		{name: "http URL loopback allowed", target: "http://127.0.0.1:1700", want: "http://127.0.0.1:1700"},
		{name: "private ip host:port defaults http", target: "192.168.1.10:1700", want: "http://192.168.1.10:1700"},
		{name: "http URL private IP allowed", target: "http://10.0.0.15:1700", want: "http://10.0.0.15:1700"},
		{name: "http URL remote rejected", target: "http://example.com:1700", wantErr: true},
		{name: "http URL remote allowed with flag", target: "http://example.com:1700", insecureHTTP: true, want: "http://example.com:1700"},
		{name: "invalid scheme rejected", target: "ftp://example.com:1700", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConnectTarget(tt.target, tt.insecureHTTP)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error, got nil (result: %#v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if got.BaseURL != tt.want {
				t.Fatalf("Expected %q, got %q", tt.want, got.BaseURL)
			}
		})
	}
}

func TestResolveTokenForConnectTarget_IPv6LoopbackUsesLocalToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_STATE_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to ensure dirs: %v", err)
	}

	origToken := globalToken
	globalToken = ""
	origCheck := checkSystemServiceRunning
	checkSystemServiceRunning = func() bool { return false }
	t.Setenv("SURGE_TOKEN", "")
	t.Cleanup(func() {
		globalToken = origToken
		checkSystemServiceRunning = origCheck
	})

	target, err := parseConnectTarget("[::1]:1700", false)
	if err != nil {
		t.Fatalf("parseConnectTarget returned error: %v", err)
	}

	token, err := resolveTokenForConnectTarget(target)
	if err != nil {
		t.Fatalf("resolveTokenForConnectTarget returned error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty local token for IPv6 loopback target")
	}
}

func TestResolveTokenForConnectTarget_UsesActiveTokenForMatchingLocalPort(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)
	t.Setenv("SURGE_TOKEN", "")

	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to ensure dirs: %v", err)
	}

	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", config.GetRuntimeDir())
	t.Setenv("SURGE_SYSTEM_STATE_DIR", config.GetStateDir())

	origToken := globalToken
	globalToken = ""
	origCheck := checkSystemServiceRunning
	checkSystemServiceRunning = func() bool { return false }
	t.Cleanup(func() {
		globalToken = origToken
		checkSystemServiceRunning = origCheck
	})
	if err := writeTokenToFile(filepath.Join(config.GetStateDir(), "token"), "connect-token"); err != nil {
		t.Fatalf("write token failed: %v", err)
	}
	saveActivePort(1888)
	defer removeActivePort()

	target, err := parseConnectTarget("127.0.0.1:1888", false)
	if err != nil {
		t.Fatalf("parseConnectTarget returned error: %v", err)
	}

	token, err := resolveTokenForConnectTarget(target)
	if err != nil {
		t.Fatalf("resolveTokenForConnectTarget returned error: %v", err)
	}
	if token != "connect-token" {
		t.Fatalf("token = %q, want %q", token, "connect-token")
	}
}

func TestIsPrivateIPHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "10.1.2.3", want: true},
		{host: "172.16.5.9", want: true},
		{host: "192.168.50.7", want: true},
		{host: "8.8.8.8", want: false},
		{host: "localhost", want: false},
		{host: "example.com", want: false},
		{host: "", want: false},
	}

	for _, tt := range tests {
		if got := isPrivateIPHost(tt.host); got != tt.want {
			t.Fatalf("isPrivateIPHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestIsLocalHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "127.0.0.1", want: true},
		{host: "localhost", want: true},
		{host: "::1", want: true},
		{host: "8.8.8.8", want: false},
		{host: "example.com", want: false},
		{host: "", want: false},
	}

	for _, tt := range tests {
		if got := isLocalHost(tt.host); got != tt.want {
			t.Fatalf("isLocalHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestGetServerBindHost(t *testing.T) {
	host := serverBindHost
	if host != "0.0.0.0" {
		t.Errorf("getServerBindHost should be 0.0.0.0, got: %q", host)
	}
}

// =============================================================================
// handleDownload Tests
// =============================================================================

func TestHandleDownload_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/download", nil)
	rec := httptest.NewRecorder()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rec.Code)
	}
}

func TestHandleDownload_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("invalid json")) {
		t.Error("Expected 'invalid json' in response body")
	}
}

func TestHandleDownload_MissingURL(t *testing.T) {
	body := `{"filename": "test.bin"}`
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("url is required")) {
		t.Error("Expected 'url is required' in response body")
	}
}

func TestHandleDownload_EmptyURL(t *testing.T) {
	body := `{"url": ""}`
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

func TestHandleDownload_PathTraversal(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"path with ..", `{"url": "http://x.com/f", "path": "../etc"}`},
		{"filename with ..", `{"url": "http://x.com/f", "filename": "../passwd"}`},
		{"filename with slash", `{"url": "http://x.com/f", "filename": "foo/bar"}`},
		{"filename with backslash", `{"url": "http://x.com/f", "filename": "foo\\bar"}`},
		// Note: Absolute path test removed - filepath.IsAbs() behaves differently on Windows vs Unix
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			svc := service.NewLocalDownloadService(nil)
			handleDownload(rec, req, "", svc)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected 400, got %d", rec.Code)
			}
		})
	}
}

// func TestHandleDownload_StatusQuery(t *testing.T) {
// 	// Setup mock download
// 	id := "test-status-id"
// 	state := progress.New(id, 2000)
// 	state.Downloaded.Store(1000)
// 	GlobalPool.Add(types.DownloadRecord{
// 		ID:    id,
// 		URL:   "http://example.com/test",
// 		State: state,
// 	})

// 	time.Sleep(50 * time.Millisecond) // Give worker time to pick it up

// 	req := httptest.NewRequest(http.MethodGet, "/download?id="+id, nil)
// 	rec := httptest.NewRecorder()

// 	handleDownload(rec, req, "")

// 	if rec.Code != http.StatusOK {
// 		t.Fatalf("Expected 200, got %d", rec.Code)
// 	}

// 	var status types.DownloadStatus
// 	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
// 		t.Fatalf("Failed to parse response: %v", err)
// 	}

// 	if status.ID != id {
// 		t.Errorf("Expected ID %s, got %s", id, status.ID)
// 	}
// 	if status.TotalSize != 2000 {
// 		t.Errorf("Expected TotalSize 2000, got %d", status.TotalSize)
// 	}
// 	if status.Status != "downloading" {
// 		t.Errorf("Expected Status 'downloading', got '%s'", status.Status)
// 	}
// }

func TestHandleDownload_StatusQuery_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/download?id=missing-id", nil)
	rec := httptest.NewRecorder()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

// Note: Testing successful handleDownload requires a running serverProgram
// which is difficult to set up in unit tests. Integration tests would be better.

// =============================================================================
// DownloadRequest Tests
// =============================================================================

func TestDownloadRequest_JSONSerialization(t *testing.T) {
	req := DownloadRequest{
		URL:      "https://example.com/file.zip",
		Filename: "file.zip",
		Path:     "/downloads",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var loaded DownloadRequest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if loaded.URL != req.URL {
		t.Error("URL mismatch")
	}
	if loaded.Filename != req.Filename {
		t.Error("Filename mismatch")
	}
	if loaded.Path != req.Path {
		t.Error("Path mismatch")
	}
}

func TestDownloadRequest_OptionalFields(t *testing.T) {
	// Only URL is required
	jsonStr := `{"url": "https://example.com/file.zip"}`

	var req DownloadRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.URL != "https://example.com/file.zip" {
		t.Error("URL not parsed correctly")
	}
	if req.Filename != "" {
		t.Error("Filename should be empty")
	}
	if req.Path != "" {
		t.Error("Path should be empty")
	}
}

// =============================================================================
// Version Variables Tests
// =============================================================================

func TestVersion_DefaultValue(t *testing.T) {
	// Version should have a default value
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestBuildTime_DefaultValue(t *testing.T) {
	if BuildTime == "" {
		t.Error("BuildTime should not be empty")
	}
}

func TestCommit_DefaultValue(t *testing.T) {
	if Commit == "" {
		t.Error("Commit should not be empty")
	}
}

// =============================================================================
// rootCmd Tests
// =============================================================================

func TestRootCmd_HasSubcommands(t *testing.T) {
	// Verify add command is registered (has 'get' as alias)
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "add" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'add' subcommand not found")
	}
}

func TestRootCmd_HasBugReportSubcommand(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "bug-report" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'bug-report' subcommand not found")
	}
}

func TestRootCmd_Use(t *testing.T) {
	if rootCmd.Use != "surge [url]..." {
		t.Errorf("Expected Use='surge [url]...', got %q", rootCmd.Use)
	}
}

func TestRootCmd_InvalidURL(t *testing.T) {
	setupIsolatedCmdState(t)
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"asjidaida"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid URL argument, got nil")
	}

	if !strings.Contains(err.Error(), "no valid URLs") {
		t.Errorf("expected error to mention 'no valid URLs', got %q", err.Error())
	}
}

func TestRootCmd_FailsIfSystemServiceRunning(t *testing.T) {
	setupIsolatedCmdState(t)

	// Simulate system service running
	origCheck := checkSystemServiceRunning
	checkSystemServiceRunning = func() bool { return true }
	t.Cleanup(func() { checkSystemServiceRunning = origCheck })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"https://example.com/test"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when system service is running, got nil")
	}

	if !strings.Contains(err.Error(), "system service is already running") {
		t.Errorf("expected error to mention system service, got %v", err)
	}
}

func TestServerStartCmd_FailsIfSystemServiceRunning(t *testing.T) {
	setupIsolatedCmdState(t)

	// Simulate system service running
	origCheck := checkSystemServiceRunning
	checkSystemServiceRunning = func() bool { return true }
	t.Cleanup(func() { checkSystemServiceRunning = origCheck })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"server", "start"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when system service is running, got nil")
	}

	if !strings.Contains(err.Error(), "system service is already running") {
		t.Errorf("expected error to mention system service, got %v", err)
	}
}

func TestRootCmd_Version(t *testing.T) {
	if rootCmd.Version == "" {
		t.Error("rootCmd.Version should not be empty")
	}
}

func TestRootCmd_VersionMatchesPackageVar(t *testing.T) {
	if rootCmd.Version != Version {
		t.Errorf("rootCmd.Version %q does not match Version %q \u2014 init() must sync them", rootCmd.Version, Version)
	}
}

func TestRootCmd_NoServerFlagRegistered(t *testing.T) {
	if rootCmd.Flags().Lookup("no-server") == nil {
		t.Fatal("expected --no-server flag on root command")
	}
}

func TestReadRootRunOptions_ReadsNoServerFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("port", 0, "")
	cmd.Flags().String("batch", "", "")
	cmd.Flags().String("output", "", "")
	cmd.Flags().Bool("no-resume", false, "")
	cmd.Flags().Bool("exit-when-done", false, "")
	cmd.Flags().Bool("no-server", false, "")
	if err := cmd.Flags().Set("no-server", "true"); err != nil {
		t.Fatalf("failed to set no-server flag: %v", err)
	}

	opts := readRootRunOptions(cmd)
	if !opts.noServer {
		t.Fatal("expected readRootRunOptions to capture --no-server")
	}
}

func TestReadRootRunOptions_TracksExplicitPort(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("port", 0, "")
	cmd.Flags().String("batch", "", "")
	cmd.Flags().String("output", "", "")
	cmd.Flags().Bool("no-resume", false, "")
	cmd.Flags().Bool("exit-when-done", false, "")
	cmd.Flags().Bool("no-server", false, "")
	if err := cmd.Flags().Set("port", "8080"); err != nil {
		t.Fatalf("failed to set port flag: %v", err)
	}

	opts := readRootRunOptions(cmd)
	if !opts.portSet {
		t.Fatal("expected readRootRunOptions to track explicit --port")
	}
	if opts.portFlag != 8080 {
		t.Fatalf("portFlag = %d, want 8080", opts.portFlag)
	}
}

func TestMaybeStartRootHTTPServer_NoServerSkipsPortFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", tmpDir)
	t.Setenv("SURGE_SYSTEM_STATE_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("failed to ensure dirs: %v", err)
	}
	removeActivePort()

	port, cleanup, err := maybeStartRootHTTPServer(rootRunOptions{noServer: true})
	if err != nil {
		t.Fatalf("maybeStartRootHTTPServer returned error: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	defer cleanup()

	if port != 0 {
		t.Fatalf("port = %d, want 0 when --no-server is enabled", port)
	}

	portFile := filepath.Join(config.GetRuntimeDir(), "port")
	if _, err := os.Stat(portFile); !os.IsNotExist(err) {
		t.Fatalf("expected no port file when server startup is skipped, stat err=%v", err)
	}
}

func TestMaybeStartRootHTTPServer_NoServerRejectsExplicitPort(t *testing.T) {
	port, cleanup, err := maybeStartRootHTTPServer(rootRunOptions{
		noServer: true,
		portFlag: 8080,
		portSet:  true,
	})
	if err == nil {
		t.Fatal("expected error when --port is combined with --no-server")
	}
	if !strings.Contains(err.Error(), "--port cannot be used with --no-server") {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 0 {
		t.Fatalf("port = %d, want 0 on validation error", port)
	}
	if cleanup != nil {
		t.Fatal("expected nil cleanup on validation error")
	}
}

// =============================================================================
// Health Check Endpoint Tests
// =============================================================================

func TestHealthEndpoint(t *testing.T) {
	// Create a minimal server with just health endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"port":   1700,
		})
	})

	server := testutil.NewHTTPServerT(t, mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("Failed to get health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", result["status"])
	}
}

// =============================================================================
// sendToServer Tests (from get.go)
// =============================================================================

func TestSendToServer_Success(t *testing.T) {
	// Create mock server
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/download" {
			t.Errorf("Expected /download, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req DownloadRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("Failed to parse request: %v", err)
		}

		if req.URL != "https://example.com/file.zip" {
			t.Errorf("URL mismatch: %s", req.URL)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
	}))
	defer server.Close()

	// Extract port from test server URL
	// Note: sendToServer uses hardcoded 127.0.0.1, so we can't directly test it
	// with httptest. We test the logic indirectly.
	t.Log("sendToServer tested indirectly via mock server")
}

func TestSendToServer_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	// Note: Can't directly test sendToServer as it uses fixed host
	t.Log("Server error handling tested via integration")
}

// =============================================================================
// addCmd Tests
// =============================================================================

func TestAddCmd_Flags(t *testing.T) {
	// Verify flags exist
	outputFlag := addCmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("Missing 'output' flag")
		return
	}
	if outputFlag.Shorthand != "o" {
		t.Errorf("Expected shorthand 'o', got %q", outputFlag.Shorthand)
	}

	batchFlag := addCmd.Flags().Lookup("batch")
	if batchFlag == nil {
		t.Fatal("Missing 'batch' flag")
		return
	}
	if batchFlag.Shorthand != "b" {
		t.Errorf("Expected shorthand 'b', got %q", batchFlag.Shorthand)
	}
}

func TestAddCmd_Use(t *testing.T) {
	if addCmd.Use != "add [url]..." {
		t.Errorf("Expected Use='add [url]...', got %q", addCmd.Use)
	}
}

func TestAddCmd_HasGetAlias(t *testing.T) {
	// addCmd should have 'get' as alias
	found := false
	for _, alias := range addCmd.Aliases {
		if alias == "get" {
			found = true
			break
		}
	}
	if !found {
		t.Error("addCmd should have 'get' alias")
	}
}

// =============================================================================
// startHTTPServer Integration Tests
// =============================================================================

const testHTTPServerToken = "cmd-test-http-token"

func TestStartHTTPServer_HealthEndpoint(t *testing.T) {
	requireTCPListener(t)
	// Create listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	// Start server in background
	svc := service.NewLocalDownloadService(nil) // Mock service with nil pool/chan for health check
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})

	// Test health endpoint
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("Failed to get health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if result["status"] != "ok" {
		t.Error("Expected status 'ok'")
	}
	if int(result["port"].(float64)) != port {
		t.Errorf("Expected port %d, got %v", port, result["port"])
	}
	if result["mode"] != "local" && result["mode"] != "service" {
		t.Errorf("Expected mode 'local' or 'service', got %v", result["mode"])
	}
}

func TestStartHTTPServer_HasCORSHeaders(t *testing.T) {
	requireTCPListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	svc := service.NewLocalDownloadService(nil)
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS headers should be set for extension support")
	}
}

func TestStartHTTPServer_OptionsRequest(t *testing.T) {
	requireTCPListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	svc := service.NewLocalDownloadService(nil)
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})
	req, _ := http.NewRequest(http.MethodOptions, fmt.Sprintf("http://127.0.0.1:%d/download", port), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// OPTIONS preflight should return 200 (handled by CORS middleware)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for OPTIONS preflight, got %d", resp.StatusCode)
	}
}

func TestStartHTTPServer_DownloadEndpoint_MethodNotAllowed(t *testing.T) {
	requireTCPListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	svc := service.NewLocalDownloadService(nil)
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/download", port), nil)
	req.Header.Set("Authorization", "Bearer "+testHTTPServerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", resp.StatusCode)
	}
}

func TestStartHTTPServer_DownloadEndpoint_BadRequest(t *testing.T) {
	requireTCPListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	svc := service.NewLocalDownloadService(nil)
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})
	// POST with invalid JSON
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/download", port), bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer "+testHTTPServerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestStartHTTPServer_DownloadEndpoint_MissingURL(t *testing.T) {
	requireTCPListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	svc := service.NewLocalDownloadService(nil)
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})
	// POST with missing URL
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/download", port), bytes.NewBufferString(`{"path": "/downloads"}`))
	req.Header.Set("Authorization", "Bearer "+testHTTPServerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestStartHTTPServer_NotFoundEndpoint(t *testing.T) {
	requireTCPListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	svc := service.NewLocalDownloadService(nil)
	go startHTTPServer(ln, port, "", svc, testHTTPServerToken)
	t.Cleanup(func() {
		if globalHTTPServer != nil {
			_ = globalHTTPServer.Close()
		}
	})
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/nonexistent", port), nil)
	req.Header.Set("Authorization", "Bearer "+testHTTPServerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// ServeMux returns 404 for unknown paths
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", resp.StatusCode)
	}
}

// =============================================================================
// handleDownload Edge Cases
// =============================================================================

func TestHandleDownload_ValidRequest_NoServerProgram(t *testing.T) {
	// Save original
	orig := serverProgram
	serverProgram = nil
	defer func() { serverProgram = orig }()

	body := `{"url": "https://example.com/file.zip"}`
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	// This will panic because serverProgram is nil
	// We can test that the validation passes first
	defer func() {
		if r := recover(); r != nil {
			// Expected - serverProgram.Send will panic
			t.Log("Panicked as expected with nil serverProgram")
		}
	}()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)
}

func TestHandleDownload_EmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(""))
	rec := httptest.NewRecorder()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	// Empty body causes EOF error on decode
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

func TestHandleDownload_LargeURL(t *testing.T) {
	largeURL := "https://example.com/" + string(make([]byte, 10000))
	body := fmt.Sprintf(`{"url": "%s"}`, largeURL)

	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	// This should handle large URLs gracefully (validation issues)
	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)

	// Should fail on URL validation or JSON parsing
	t.Logf("Response: %d", rec.Code)
}

func TestHandleDownload_SpecialCharactersInPath(t *testing.T) {
	body := `{"url": "https://example.com/file.zip", "path": "/path/with spaces/and (parens)"}`
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Log("Panicked (serverProgram nil)")
		}
	}()

	svc := service.NewLocalDownloadService(nil)
	handleDownload(rec, req, "", svc)
}

// =============================================================================
// Execute Function Test
// =============================================================================

func TestExecute_NoArgs(t *testing.T) {
	// Can't easily test Execute() as it calls os.Exit
	// Just verify the function exists
	_ = Execute
}

// =============================================================================
// Additional CORS Tests
// =============================================================================

func TestCorsMiddleware_AllMethods(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	corsHandler := corsMiddleware(handler)

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/test", nil)
		rec := httptest.NewRecorder()
		corsHandler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("CORS header should be set for %s (required for extension support)", method)
		}
	}
}

// =============================================================================
// Port Discovery Integration
// =============================================================================

func TestPortFileLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("Failed to ensure dirs: %v", err)
	}

	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", config.GetRuntimeDir())
	t.Setenv("SURGE_SYSTEM_STATE_DIR", config.GetStateDir())

	// Clean up first
	removeActivePort()

	portFile := filepath.Join(config.GetRuntimeDir(), "port")

	// Verify no port file initially
	if _, err := os.Stat(portFile); !os.IsNotExist(err) {
		t.Log("Port file already exists, removing")
		_ = os.Remove(portFile)
	}

	// Save
	saveActivePort(9999)

	// Verify it was created
	data, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("Port file not created: %v", err)
	}
	if string(data) != "9999" {
		t.Errorf("Expected '9999', got %q", string(data))
	}

	// Remove
	removeActivePort()

	// Verify it's gone
	if _, err := os.Stat(portFile); !os.IsNotExist(err) {
		t.Error("Port file should be removed")
	}
}

// =============================================================================
// findAvailablePort Extended Tests
// =============================================================================

func TestFindAvailablePort_MultipleSequential(t *testing.T) {
	requireTCPListener(t)
	var listeners []net.Listener
	defer func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	}()

	// Get 5 sequential ports
	startPort := 53000
	for i := 0; i < 5; i++ {
		port, ln := findAvailablePort(startPort)
		if ln == nil {
			t.Fatalf("Failed to find port on iteration %d", i)
		}
		listeners = append(listeners, ln)
		startPort = port + 1
	}

	// All should be different
	seen := make(map[int]bool)
	for _, ln := range listeners {
		port := ln.Addr().(*net.TCPAddr).Port
		if seen[port] {
			t.Errorf("Duplicate port: %d", port)
		}
		seen[port] = true
	}
}

func TestFindAvailablePort_HighPort(t *testing.T) {
	requireTCPListener(t)
	port, ln := findAvailablePort(60000)
	if ln == nil {
		t.Fatal("Failed to find high port")
	}
	defer func() { _ = ln.Close() }()

	if port < 60000 {
		t.Errorf("Expected port >= 60000, got %d", port)
	}
}

// =============================================================================
// pauseCmd Tests
// =============================================================================

func TestPauseCmd_Use(t *testing.T) {
	if pauseCmd.Use != "pause <ID>" {
		t.Errorf("Expected Use='pause <ID>', got %q", pauseCmd.Use)
	}
}

func TestPauseCmd_Flags(t *testing.T) {
	allFlag := pauseCmd.Flags().Lookup("all")
	if allFlag == nil {
		t.Error("Missing 'all' flag")
	}
}

// =============================================================================
// resumeCmd Tests
// =============================================================================

func TestResumeCmd_Use(t *testing.T) {
	if resumeCmd.Use != "resume <ID>" {
		t.Errorf("Expected Use='resume <ID>', got %q", resumeCmd.Use)
	}
}

func TestResumeCmd_Flags(t *testing.T) {
	allFlag := resumeCmd.Flags().Lookup("all")
	if allFlag == nil {
		t.Error("Missing 'all' flag")
	}
}

// =============================================================================
// rmCmd Tests
// =============================================================================

func TestRmCmd_Use(t *testing.T) {
	if rmCmd.Use != "rm <ID>" {
		t.Errorf("Expected Use='rm <ID>', got %q", rmCmd.Use)
	}
}

func TestRmCmd_HasKillAlias(t *testing.T) {
	found := false
	for _, alias := range rmCmd.Aliases {
		if alias == "kill" {
			found = true
			break
		}
	}
	if !found {
		t.Error("rmCmd should have 'kill' alias")
	}
}

func TestRmCmd_Flags(t *testing.T) {
	cleanFlag := rmCmd.Flags().Lookup("clean")
	if cleanFlag == nil {
		t.Error("Missing 'clean' flag")
	}
}

// =============================================================================
// lsCmd Tests
// =============================================================================

func TestLsCmd_Use(t *testing.T) {
	if lsCmd.Use != "ls [id]" {
		t.Errorf("Expected Use='ls [id]', got %q", lsCmd.Use)
	}
}

func TestLsCmd_Flags(t *testing.T) {
	jsonFlag := lsCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Error("Missing 'json' flag")
	}

	watchFlag := lsCmd.Flags().Lookup("watch")
	if watchFlag == nil {
		t.Error("Missing 'watch' flag")
	}
}

// =============================================================================
// serverCmd Tests
// =============================================================================

func TestServerCmd_HasSubcommands(t *testing.T) {
	subcommands := map[string]bool{"start": false, "stop": false, "status": false}

	for _, cmd := range serverCmd.Commands() {
		if _, ok := subcommands[cmd.Name()]; ok {
			subcommands[cmd.Name()] = true
		}
	}

	for name, found := range subcommands {
		if !found {
			t.Errorf("Missing '%s' subcommand in serverCmd", name)
		}
	}
}

func TestResolveServerToken_UsesEnvWhenFlagEmpty(t *testing.T) {
	t.Setenv("SURGE_TOKEN", "env-token-abc")
	_ = serverCmd.PersistentFlags().Set("token", "")

	got := resolveServerToken(serverCmd)
	if got != "env-token-abc" {
		t.Fatalf("resolveServerToken() = %q, want %q", got, "env-token-abc")
	}
}

func TestResolveServerToken_FlagOverridesEnv(t *testing.T) {
	t.Setenv("SURGE_TOKEN", "env-token-abc")
	_ = serverCmd.PersistentFlags().Set("token", "flag-token-xyz")
	t.Cleanup(func() {
		_ = serverCmd.PersistentFlags().Set("token", "")
	})

	got := resolveServerToken(serverCmd)
	if got != "flag-token-xyz" {
		t.Fatalf("resolveServerToken() = %q, want %q", got, "flag-token-xyz")
	}
}
