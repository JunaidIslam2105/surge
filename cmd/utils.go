package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
	"github.com/charmbracelet/x/term"
)

type activeConnectionDetails struct {
	port       int
	token      string
	runtimeDir string
	stateDir   string
}

func readPortFile(runtimeDir string) int {
	portFile := filepath.Join(runtimeDir, "port")
	data, err := os.ReadFile(portFile)
	if err != nil {
		return 0
	}
	var port int
	_, _ = fmt.Sscanf(string(data), "%d", &port)
	return port
}

var checkSystemServiceRunning = isSystemServiceRunning

func readPIDFile(runtimeDir string) int {
	pidFile := filepath.Join(runtimeDir, "pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	var pid int
	_, _ = fmt.Sscanf(string(data), "%d", &pid)
	return pid
}

func readStateToken(stateDir string) string {
	token, err := readTokenFromFile(filepath.Join(stateDir, "token"))
	if err != nil {
		return ""
	}
	return token
}

func activeConnectionCandidates() []activeConnectionDetails {
	userCand := activeConnectionDetails{
		runtimeDir: config.GetRuntimeDir(),
		stateDir:   config.GetStateDir(),
	}
	sysCand := activeConnectionDetails{
		runtimeDir: config.GetSystemRuntimeDir(),
		stateDir:   config.GetSystemStateDir(),
	}
	if checkSystemServiceRunning() {
		return []activeConnectionDetails{sysCand, userCand}
	}
	return []activeConnectionDetails{userCand, sysCand}
}

func getActiveConnectionDetails() (activeConnectionDetails, bool) {
	for _, candidate := range activeConnectionCandidates() {
		port := readPortFile(candidate.runtimeDir)
		if port <= 0 {
			continue
		}
		candidate.port = port
		candidate.token = readStateToken(candidate.runtimeDir)
		if candidate.token == "" {
			candidate.token = readStateToken(candidate.stateDir)
		}
		return candidate, true
	}
	return activeConnectionDetails{}, false
}

// readActivePort reads the active port and keeps legacy callers on the same
// user-first/system-fallback resolution path as token resolution.
func readActivePort() int {
	details, ok := getActiveConnectionDetails()
	if !ok {
		return 0
	}
	return details.port
}

// ParseURLArg parses a command line argument that might contain comma-separated mirrors
// Returns the primary URL and a list of all mirrors (including the primary)
func ParseURLArg(arg string) (string, []string) {
	parts := strings.Split(arg, ",")
	var urls []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		// Only an http(s) URL starts a new mirror. A segment without a
		// scheme means the comma lived inside the previous URL (e.g. the
		// "formats=PNG,ITEM TILE,LOG" query in an archive.org compress link),
		// so glue it back instead of treating it as a separate mirror.
		if len(urls) > 0 && !startsNewMirror(trimmed) {
			urls[len(urls)-1] += "," + trimmed
			continue
		}
		urls = append(urls, trimmed)
	}
	if len(urls) == 0 {
		return "", nil
	}
	return urls[0], urls
}

// startsNewMirror reports whether a comma-separated segment begins a new
// mirror rather than continuing the previous URL. Only http(s) URLs are
// downloadable, so a segment is a mirror boundary only when it starts with the
// http:// or https:// prefix. Matching the literal prefix (rather than a
// non-empty url.Parse scheme) keeps bare-scheme query values like "http:" glued
// to the previous URL, and matches how the rest of the CLI recognizes URLs
// (internal/clipboard/validator.go).
func startsNewMirror(segment string) bool {
	return strings.HasPrefix(segment, "http://") || strings.HasPrefix(segment, "https://")
}

// ValidateAndNormalizeURL ensures a provided download string has a valid URL scheme
// and normalizes missing schemes for common domains.
func ValidateAndNormalizeURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	if u.Scheme == "" {
		// Could be a domain (e.g. example.com/file.zip or github.com/release/v1.0.0), try prepending https
		if strings.Contains(rawURL, ".") {
			return "https://" + rawURL, nil
		}
		return "", fmt.Errorf("missing or unsupported scheme (must be http:// or https://)")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme: %s (must be http:// or https://)", u.Scheme)
	}

	return rawURL, nil
}

func resolveLocalToken() string {
	token, _ := resolveLocalTokenForDetails(activeConnectionDetails{})
	return token
}

func resolveLocalTokenForDetails(details activeConnectionDetails) (string, error) {
	if token := strings.TrimSpace(globalToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("SURGE_TOKEN")); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(details.token); token != "" {
		return token, nil
	}
	if details.port != 0 {
		return "", fmt.Errorf("local server is running on port %d but its token could not be read. Try connecting with elevated privileges", details.port)
	}
	return ensureAuthToken(), nil
}

// resolveHostTarget returns the target host, prioritizing the --host flag over the SURGE_HOST environment variable.
func resolveHostTarget() string {
	if host := strings.TrimSpace(globalHost); host != "" {
		return host
	}
	return strings.TrimSpace(os.Getenv("SURGE_HOST"))
}

// resolveClientOutputPath resolves the output path for CLI client commands.
func resolveClientOutputPath(outputDir string) string {
	if resolveHostTarget() != "" {
		// Pass-through for remote connections so the daemon uses its own default/CWD.
		return outputDir
	}

	if strings.TrimSpace(outputDir) == "" {
		pwd, err := os.Getwd()
		if err == nil {
			return pwd
		}
		return "."
	}
	return utils.EnsureAbsPath(outputDir)
}

func resolveAPIConnection(requireServer bool) (string, string, error) {
	target := resolveHostTarget()
	if target == "" {
		details, ok := getActiveConnectionDetails()
		if ok {
			token, err := resolveLocalTokenForDetails(details)
			if err != nil {
				return "", "", err
			}
			return fmt.Sprintf("http://127.0.0.1:%d", details.port), token, nil
		}
		if !requireServer {
			return "", "", nil
		}
		return "", "", errors.New("surge is not running locally. start it or pass --host (or set SURGE_HOST)")
	}

	clientCfg := currentRemoteClientConfig()
	parsed, err := parseConnectTarget(target, clientCfg.AllowInsecureHTTP)
	if err != nil {
		return "", "", err
	}
	token, err := resolveTokenForConnectTarget(parsed)
	if err != nil {
		return "", "", err
	}
	return parsed.BaseURL, token, nil
}

func doAPIRequest(method string, baseURL string, token string, path string, body io.Reader) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), path)
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client, err := newRemoteAPIHTTPClient()
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func sendToServer(url string, mirrors []string, outPath string, baseURL string, token string) error {
	return sendToServerWithApproval(url, mirrors, outPath, baseURL, token, true)
}

func sendToServerWithApproval(url string, mirrors []string, outPath string, baseURL string, token string, skipApproval bool) error {
	reqBody := DownloadRequest{
		URL:          url,
		Mirrors:      mirrors,
		Path:         outPath,
		SkipApproval: skipApproval,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := doAPIRequest(http.MethodPost, baseURL, token, "/download", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s - %s", resp.Status, string(body))
	}

	return nil
}

func sendBatchToServer(urls []string, outPath string, baseURL string, token string, skipApproval bool) error {
	reqBody := BatchDownloadRequest{
		Path:         outPath,
		SkipApproval: skipApproval,
	}
	for _, arg := range urls {
		url, mirrors := ParseURLArg(arg)
		if url == "" {
			continue
		}
		reqBody.Downloads = append(reqBody.Downloads, DownloadRequest{
			URL:     url,
			Mirrors: mirrors,
			Path:    outPath,
		})
	}
	if len(reqBody.Downloads) == 0 {
		return fmt.Errorf("no valid URLs to add")
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := doAPIRequest(http.MethodPost, baseURL, token, "/download/batch", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s - %s", resp.Status, string(body))
	}

	return nil
}

// GetRemoteDownloads fetches all downloads from the running server
func GetRemoteDownloads(baseURL string, token string) ([]types.DownloadStatus, error) {
	resp, err := doAPIRequest(http.MethodGet, baseURL, token, "/list", nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %s", resp.Status)
	}

	var statuses []types.DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return nil, err
	}

	return statuses, nil
}

// ExecuteAPIAction connects to the server, resolves the ID, and sends a request.
func ExecuteAPIAction(rawID, endpoint, method, successMsg string) error {
	baseURL, token, err := resolveAPIConnection(true)
	if err != nil {
		return fmt.Errorf("failed to connect to Surge server: %w", err)
	}

	id, err := resolveDownloadID(rawID)
	if err != nil {
		return fmt.Errorf("failed to resolve download ID: %w", err)
	}

	// The HTTP API expects the download id as the "id" query parameter (see
	// withRequiredID in cmd/http_api.go); a path segment (e.g. /pause/<id>)
	// doesn't match the registered routes and 404s. Matches the extension and
	// internal/core/remote_service.go, which already use ?id=.
	resp, err := doAPIRequest(method, baseURL, token, fmt.Sprintf("%s?id=%s", endpoint, url.QueryEscape(id)), nil)
	if err != nil {
		return fmt.Errorf("failed to send request to server: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s - %s", resp.Status, string(body))
	}

	fmt.Println(successMsg)
	return nil
}

// resolveDownloadID resolves a partial ID (prefix) to a full download ID.
// If the input is at least 8 characters and matches a single download, returns the full ID.
// Returns the original ID if no match found or if it's already a full ID.
func resolveDownloadID(partialID string) (string, error) {
	if len(partialID) >= 32 {
		return partialID, nil // Already a full UUID
	}

	strictRemote := resolveHostTarget() != ""
	var candidates []string

	// 1. Try to get candidates from running server
	baseURL, token, err := resolveAPIConnection(false)
	if err != nil {
		return "", err
	}
	if baseURL != "" {
		remoteDownloads, err := GetRemoteDownloads(baseURL, token)
		if err != nil {
			if strictRemote {
				return "", fmt.Errorf("failed to list remote downloads: %w", err)
			}
		} else {
			appendCandidateIDs(&candidates, remoteDownloads)
		}
	}

	if strictRemote {
		return resolveIDFromCandidates(partialID, candidates)
	}

	// 2. Get all downloads from database
	downloads, err := store.ListAllDownloads()
	if err == nil {
		for _, d := range downloads {
			candidates = append(candidates, d.ID)
		}
	} else if len(candidates) == 0 {
		// Only short-circuit when both remote and DB are unavailable.
		return partialID, nil
	}

	return resolveIDFromCandidates(partialID, candidates)
}

func appendCandidateIDs(candidates *[]string, downloads []types.DownloadStatus) {
	for _, d := range downloads {
		*candidates = append(*candidates, d.ID)
	}
}

func resolveIDFromCandidates(partialID string, candidates []string) (string, error) {
	// Find matches among all candidates
	var matches []string
	seen := make(map[string]bool)

	for _, id := range candidates {
		if strings.HasPrefix(id, partialID) {
			if !seen[id] {
				matches = append(matches, id)
				seen[id] = true
			}
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous ID prefix '%s' matches %d downloads", partialID, len(matches))
	}

	return partialID, nil // No match, use as-is (will fail with "not found" later)
}

var checkIsTerminal = func() bool {
	return term.IsTerminal(os.Stdout.Fd())
}
