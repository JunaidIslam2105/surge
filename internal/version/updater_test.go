package version

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient returns an http.Client that does not keep idle connections
// alive, so goleak in TestMain has nothing lingering after srv.Close().
func newTestClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func releaseHandler(t *testing.T, body string, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request missing User-Agent header")
		}
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Error("request missing GitHub API Accept header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Errorf("failed to write response body: %v", err)
		}
	}
}

func TestUpdaterCheckUpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t,
		`{"tag_name":"v1.4.2","html_url":"https://github.com/SurgeDM/Surge/releases/tag/v1.4.2"}`, http.StatusOK))
	defer srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: srv.URL}
	info, err := u.Check("1.4.0")
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if info == nil {
		t.Fatal("Check() returned nil info")
	}
	if !info.UpdateAvailable {
		t.Error("UpdateAvailable = false, want true")
	}
	if info.CurrentVersion != "1.4.0" {
		t.Errorf("CurrentVersion = %q, want %q", info.CurrentVersion, "1.4.0")
	}
	if info.LatestVersion != "v1.4.2" {
		t.Errorf("LatestVersion = %q, want %q", info.LatestVersion, "v1.4.2")
	}
	if info.ReleaseURL != "https://github.com/SurgeDM/Surge/releases/tag/v1.4.2" {
		t.Errorf("ReleaseURL = %q, want release page URL", info.ReleaseURL)
	}
}

func TestUpdaterCheckUpToDate(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t,
		`{"tag_name":"v1.4.2","html_url":"https://github.com/SurgeDM/Surge/releases/tag/v1.4.2"}`, http.StatusOK))
	defer srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: srv.URL}
	info, err := u.Check("1.4.2")
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if info == nil {
		t.Fatal("Check() returned nil info")
	}
	if info.UpdateAvailable {
		t.Error("UpdateAvailable = true, want false")
	}
}

func TestUpdaterCheckCurrentAhead(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t,
		`{"tag_name":"v1.4.2","html_url":"https://github.com/SurgeDM/Surge/releases/tag/v1.4.2"}`, http.StatusOK))
	defer srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: srv.URL}
	info, err := u.Check("2.0.0")
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if info != nil && info.UpdateAvailable {
		t.Error("UpdateAvailable = true, want false (current ahead of latest)")
	}
}

func TestUpdaterCheckDevBuild(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: srv.URL}
	for _, v := range []string{"dev", ""} {
		info, err := u.Check(v)
		if err != nil {
			t.Errorf("Check(%q) returned error: %v", v, err)
		}
		if info != nil {
			t.Errorf("Check(%q) = %+v, want nil", v, info)
		}
	}
	if hit {
		t.Error("server was hit for a development build; check should have been skipped")
	}
}

func TestUpdaterCheckAPIError(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t, `{"message":"Not Found"}`, http.StatusNotFound))
	defer srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: srv.URL}
	info, err := u.Check("1.4.0")
	if err == nil {
		t.Fatal("Check() returned nil error, want ErrAPI")
	}
	if !errors.Is(err, ErrAPI) {
		t.Errorf("err = %v, want errors.Is(err, ErrAPI)", err)
	}
	if info != nil {
		t.Errorf("Check() = %+v, want nil info on API error", info)
	}
}

func TestUpdaterCheckParseError(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t, `not json {`, http.StatusOK))
	defer srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: srv.URL}
	_, err := u.Check("1.4.0")
	if err == nil {
		t.Fatal("Check() returned nil error, want ErrParse")
	}
	if !errors.Is(err, ErrParse) {
		t.Errorf("err = %v, want errors.Is(err, ErrParse)", err)
	}
}

func TestUpdaterCheckNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	u := &Updater{Client: newTestClient(), APIURL: deadURL}
	_, err := u.Check("1.4.0")
	if err == nil {
		t.Fatal("Check() returned nil error, want ErrNetwork")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("err = %v, want errors.Is(err, ErrNetwork)", err)
	}
}

func TestUpdaterCheckTimeout(t *testing.T) {
	client := &http.Client{
		Timeout: 50 * time.Millisecond,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}
	u := &Updater{Client: client, APIURL: "https://example.test/latest"}

	start := time.Now()
	_, err := u.Check("1.4.0")
	if err == nil {
		t.Fatal("Check() returned nil error, want ErrNetwork on timeout")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("err = %v, want errors.Is(err, ErrNetwork)", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Check() took %v, want quick timeout", elapsed)
	}
}

func TestUpdaterCheckNilClientUsesTimeout(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t,
		`{"tag_name":"v1.4.2","html_url":"https://github.com/SurgeDM/Surge/releases/tag/v1.4.2"}`, http.StatusOK))
	defer srv.Close()

	u := &Updater{APIURL: srv.URL, Timeout: 5 * time.Second}
	info, err := u.Check("1.4.0")
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if info == nil || !info.UpdateAvailable {
		t.Error("expected update available")
	}
}

func TestCheckForUpdateWrapper(t *testing.T) {
	// The package-level wrapper should construct a default Updater and hit
	// the real GitHub API URL. We only verify it returns without panicking
	// for a dev build (no network involved).
	info, err := CheckForUpdate("dev")
	if err != nil {
		t.Fatalf("CheckForUpdate(\"dev\") returned error: %v", err)
	}
	if info != nil {
		t.Errorf("CheckForUpdate(\"dev\") = %+v, want nil", info)
	}
}

func TestCheckForUpdateWrapperSuppressesUpdateCheckErrors(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t, `{"message":"Not Found"}`, http.StatusNotFound))
	defer srv.Close()

	info, err := checkForUpdate("1.4.0", &Updater{Client: newTestClient(), APIURL: srv.URL})
	if err != nil {
		t.Fatalf("checkForUpdate() returned error: %v", err)
	}
	if info != nil {
		t.Errorf("checkForUpdate() = %+v, want nil info on suppressed API error", info)
	}
}
