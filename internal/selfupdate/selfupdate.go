// Package selfupdate installs Surge releases from GitHub.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/SurgeDM/Surge/internal/version"
)

const checksumsAssetName = "checksums.txt"

var (
	ErrNoUpdate            = errors.New("surge is already up to date")
	ErrUnsupportedPlatform = errors.New("self-update is not supported on this platform")
)

// Options configures a self-update run.
type Options struct {
	CurrentVersion string
	ExecutablePath string
	Client         *http.Client
	VersionUpdater *version.Updater
	GOOS           string
	GOARCH         string
}

// Update downloads, verifies, extracts, and installs the latest Surge release.
func Update(ctx context.Context, opts Options) (*version.UpdateInfo, error) {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.GOOS == "windows" {
		return nil, fmt.Errorf("%w: Windows cannot replace a running executable yet", ErrUnsupportedPlatform)
	}

	updater := opts.VersionUpdater
	if updater == nil {
		updater = version.New()
	}

	release, err := updater.LatestRelease()
	if err != nil {
		return nil, err
	}

	info := &version.UpdateInfo{
		CurrentVersion:  opts.CurrentVersion,
		LatestVersion:   release.TagName,
		ReleaseURL:      release.HTMLURL,
		UpdateAvailable: isUpdateAvailable(release.TagName, opts.CurrentVersion),
	}
	if !info.UpdateAvailable {
		return info, ErrNoUpdate
	}

	asset, ok := selectReleaseAsset(release.Assets, opts.GOOS, opts.GOARCH)
	if !ok {
		return info, fmt.Errorf("no release asset found for %s/%s", opts.GOOS, opts.GOARCH)
	}
	checksums, ok := findAsset(release.Assets, checksumsAssetName)
	if !ok {
		return info, fmt.Errorf("release is missing %s", checksumsAssetName)
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: version.RequestTimeout}
	}

	tmpDir, err := os.MkdirTemp("", "surge-update-*")
	if err != nil {
		return info, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	archivePath := filepath.Join(tmpDir, safeAssetFileName(asset.Name))
	if err := download(ctx, client, asset.BrowserDownloadURL, archivePath); err != nil {
		return info, err
	}

	checksumPath := filepath.Join(tmpDir, checksumsAssetName)
	if err := download(ctx, client, checksums.BrowserDownloadURL, checksumPath); err != nil {
		return info, err
	}
	if err := verifyChecksum(archivePath, checksumPath, asset.Name); err != nil {
		return info, err
	}

	binaryPath := filepath.Join(tmpDir, binaryName(opts.GOOS))
	if err := extractBinary(archivePath, binaryName(opts.GOOS), binaryPath); err != nil {
		return info, err
	}

	target := opts.ExecutablePath
	if target == "" {
		target, err = os.Executable()
		if err != nil {
			return info, err
		}
	}
	if err := installBinary(binaryPath, target); err != nil {
		return info, err
	}

	return info, nil
}

func isUpdateAvailable(latest, current string) bool {
	return version.IsNewerVersion(latest, current)
}

func safeAssetFileName(name string) string {
	base := filepath.Base(name)
	if base == "." || base == string(filepath.Separator) {
		return "release-asset"
	}
	return base
}

func selectReleaseAsset(assets []version.GitHubAsset, goos, goarch string) (version.GitHubAsset, bool) {
	wantExt := ".tar.gz"
	if goos == "windows" {
		wantExt = ".zip"
	}
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, strings.ToLower(goos)) &&
			strings.Contains(name, strings.ToLower(goarch)) &&
			strings.HasSuffix(name, wantExt) {
			return asset, true
		}
	}
	return version.GitHubAsset{}, false
}

func findAsset(assets []version.GitHubAsset, name string) (version.GitHubAsset, bool) {
	for _, asset := range assets {
		if strings.EqualFold(asset.Name, name) {
			return asset, true
		}
	}
	return version.GitHubAsset{}, false
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "surge.exe"
	}
	return "surge"
}

func download(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func verifyChecksum(archivePath, checksumPath, assetName string) error {
	want, err := checksumForAsset(checksumPath, assetName)
	if err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func checksumForAsset(checksumPath, assetName string) (string, error) {
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		fileName := strings.TrimPrefix(filepath.Base(fields[len(fields)-1]), "*")
		if fileName == assetName {
			return strings.TrimSpace(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", assetName)
}

func extractBinary(archivePath, name, dest string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractBinaryFromZip(archivePath, name, dest)
	}
	return extractBinaryFromTarGz(archivePath, name, dest)
}

func extractBinaryFromTarGz(archivePath, name, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() {
		_ = gz.Close()
	}()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if h.FileInfo().IsDir() || filepath.Base(h.Name) != name {
			continue
		}
		return writeExtractedBinary(dest, tr, h.FileInfo().Mode())
	}
	return fmt.Errorf("%s not found in archive", name)
}

func extractBinaryFromZip(archivePath, name, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = zr.Close()
	}()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeExtractedBinary(dest, rc, f.FileInfo().Mode())
		if closeErr := rc.Close(); err == nil {
			err = closeErr
		}
		return err
	}
	return fmt.Errorf("%s not found in archive", name)
}

func writeExtractedBinary(dest string, src io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if mode != 0 {
		return os.Chmod(dest, mode|0o755)
	}
	return nil
}

func installBinary(src, target string) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}

	staged, err := os.CreateTemp(filepath.Dir(target), ".surge-update-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(stagedPath)
		}
	}()

	srcFile, err := os.Open(src)
	if err != nil {
		_ = staged.Close()
		return err
	}
	if _, err := io.Copy(staged, srcFile); err != nil {
		_ = srcFile.Close()
		_ = staged.Close()
		return err
	}
	if err := srcFile.Close(); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagedPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}
