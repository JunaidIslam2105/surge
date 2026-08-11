package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SurgeDM/Surge/internal/version"
)

func TestSelectReleaseAsset(t *testing.T) {
	assets := []version.GitHubAsset{
		{Name: "Surge_1.2.0_linux_arm64.tar.gz"},
		{Name: "Surge_1.2.0_windows_amd64.zip"},
		{Name: "checksums.txt"},
	}

	asset, ok := selectReleaseAsset(assets, "windows", "amd64")
	if !ok {
		t.Fatal("selectReleaseAsset() did not find windows asset")
	}
	if asset.Name != "Surge_1.2.0_windows_amd64.zip" {
		t.Fatalf("asset = %q, want windows amd64 zip", asset.Name)
	}
}

func TestChecksumForAsset(t *testing.T) {
	path := filepath.Join(t.TempDir(), checksumsAssetName)
	if err := os.WriteFile(path, []byte("abc123  *Surge_1.2.0_linux_amd64.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := checksumForAsset(path, "Surge_1.2.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("checksumForAsset() returned error: %v", err)
	}
	if got != "abc123" {
		t.Fatalf("checksum = %q, want abc123", got)
	}
}

func TestSafeAssetFileName(t *testing.T) {
	got := safeAssetFileName(filepath.Join("..", "Surge_1.2.0_linux_amd64.tar.gz"))
	if got != "Surge_1.2.0_linux_amd64.tar.gz" {
		t.Fatalf("safeAssetFileName() = %q, want base filename", got)
	}
}

func TestUpdateUnsupportedWindows(t *testing.T) {
	_, err := Update(context.Background(), Options{
		CurrentVersion: "1.0.0",
		GOOS:           "windows",
		GOARCH:         "amd64",
	})
	if err == nil {
		t.Fatal("Update() returned nil error, want unsupported platform")
	}
}

func TestInstallBinaryStagesInTargetDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installBinary is not used for Windows self-update yet")
	}

	src := filepath.Join(t.TempDir(), "surge")
	if err := os.WriteFile(src, []byte("new surge binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "surge")
	if err := os.WriteFile(target, []byte("old surge binary"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := installBinary(src, target); err != nil {
		t.Fatalf("installBinary() returned error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new surge binary" {
		t.Fatalf("installed binary = %q, want new surge binary", got)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o700 {
		t.Fatalf("installed mode = %v, want 0700", gotMode)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source should remain after staged install: %v", err)
	}
}

func TestUpdateInstallsVerifiedArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot replace an existing executable while tests are running")
	}

	const assetName = "Surge_1.2.0_linux_amd64.tar.gz"
	archiveBytes := makeTarGz(t, "surge", []byte("new surge binary"))
	sum := sha256.Sum256(archiveBytes)
	checksums := []byte(fmt.Sprintf("%x  %s\n", sum, assetName))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{
			"tag_name":"v1.2.0",
			"html_url":"%s/releases/v1.2.0",
			"assets":[
				{"name":%q,"browser_download_url":"%s/%s"},
				{"name":"checksums.txt","browser_download_url":"%s/checksums.txt"}
			]
		}`, srv.URL, assetName, srv.URL, assetName, srv.URL)
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archiveBytes)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(checksums)
	})

	target := filepath.Join(t.TempDir(), "surge")
	if err := os.WriteFile(target, []byte("old surge binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := Update(context.Background(), Options{
		CurrentVersion: "1.0.0",
		ExecutablePath: target,
		Client:         newNoKeepAliveClient(),
		VersionUpdater: &version.Updater{Client: newNoKeepAliveClient(), APIURL: srv.URL + "/latest"},
		GOOS:           "linux",
		GOARCH:         "amd64",
	})
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}
	if info == nil || !info.UpdateAvailable {
		t.Fatalf("Update() info = %+v, want update available", info)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new surge binary" {
		t.Fatalf("installed binary = %q, want new surge binary", got)
	}
}

func newNoKeepAliveClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
