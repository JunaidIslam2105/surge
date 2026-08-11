// Package version provides functionality for checking for Surge updates via GitHub API.
package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/SurgeDM/Surge/internal/utils"
)

const (
	GitHubAPIURL   = "https://api.github.com/repos/SurgeDM/Surge/releases/latest"
	RequestTimeout = 10 * time.Second
)

var (
	ErrNetwork = errors.New("update check: network error")
	ErrAPI     = errors.New("update check: GitHub API error")
	ErrParse   = errors.New("update check: invalid response")
)

// Updater checks GitHub releases using injectable HTTP configuration.
type Updater struct {
	Client  *http.Client
	APIURL  string
	Timeout time.Duration
}

// New returns an Updater configured for Surge's GitHub releases endpoint.
func New() *Updater {
	return &Updater{
		Client:  &http.Client{Timeout: RequestTimeout},
		APIURL:  GitHubAPIURL,
		Timeout: RequestTimeout,
	}
}

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	CurrentVersion  string // The current version of Surge
	LatestVersion   string // The latest version available on GitHub
	ReleaseURL      string // URL to the GitHub release page
	UpdateAvailable bool   // Whether an update is available
}

// GitHubAsset represents a downloadable release asset from GitHub.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// GitHubRelease represents the relevant fields from the GitHub API response
type GitHubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Assets  []GitHubAsset `json:"assets"`
}

// CheckForUpdate checks if a newer version of Surge is available on GitHub.
// Returns nil, nil if there's a network error (fail silently).
// Returns UpdateInfo with UpdateAvailable=false if current version is up to date.
// Returns UpdateInfo with UpdateAvailable=true if a newer version exists.
func CheckForUpdate(currentVersion string) (*UpdateInfo, error) {
	return checkForUpdate(currentVersion, New())
}

func checkForUpdate(currentVersion string, updater *Updater) (*UpdateInfo, error) {
	info, err := updater.Check(currentVersion)
	if err != nil && (errors.Is(err, ErrNetwork) || errors.Is(err, ErrAPI) || errors.Is(err, ErrParse)) {
		utils.Debug("Update check failed: %v", err)
		return nil, nil
	}
	return info, err
}

// Check checks if a newer version of Surge is available on GitHub.
// Returns nil, nil for development builds (skipped).
func (u *Updater) Check(currentVersion string) (*UpdateInfo, error) {
	// Skip check for development builds
	if currentVersion == "dev" || currentVersion == "" {
		return nil, nil
	}

	release, err := u.LatestRelease()
	if err != nil {
		return nil, err
	}

	updateInfo := &UpdateInfo{
		CurrentVersion:  currentVersion,
		LatestVersion:   release.TagName,
		ReleaseURL:      release.HTMLURL,
		UpdateAvailable: isNewerVersion(release.TagName, currentVersion),
	}

	return updateInfo, nil
}

// LatestRelease fetches Surge's latest GitHub release.
func (u *Updater) LatestRelease() (*GitHubRelease, error) {
	if u == nil {
		u = New()
	}

	apiURL := u.APIURL
	if apiURL == "" {
		apiURL = GitHubAPIURL
	}

	timeout := u.Timeout
	if timeout == 0 {
		timeout = RequestTimeout
	}

	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}

	// Set User-Agent as required by GitHub API
	req.Header.Set("User-Agent", "Surge-Update-Checker")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrAPI, resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParse, err)
	}

	return &release, nil
}

// normalizeVersion removes the 'v' prefix and trims whitespace
func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	return version
}

// IsNewerVersion compares two semver strings and returns true if latest > current.
// Invalid versions are treated as not updateable so malformed release tags fail closed.
func IsNewerVersion(latest, current string) bool {
	latest = normalizeVersion(latest)
	current = normalizeVersion(current)

	latestVersion, err := semver.NewVersion(latest)
	if err != nil {
		return false
	}
	currentVersion, err := semver.NewVersion(current)
	if err != nil {
		return false
	}
	return latestVersion.Compare(currentVersion) > 0
}

func isNewerVersion(latest, current string) bool {
	return IsNewerVersion(latest, current)
}
