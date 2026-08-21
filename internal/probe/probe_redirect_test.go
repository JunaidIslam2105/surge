package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SurgeDM/Surge/internal/probe"
	"github.com/SurgeDM/Surge/internal/types"
)

func TestProbeRedirectRange(t *testing.T) {
	// Destination server supports range
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// Redirect server
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	res, err := probe.ProbeServer(context.Background(), redirect.URL, "", nil)
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}

	if !res.SupportsRange {
		t.Errorf("ProbeServer did not forward Range header: SupportsRange is false!")
	}
}

func TestProbeRedirect_SameOriginPreservesAuthHeaders(t *testing.T) {
	var gotAuth, gotCookie, gotAPIKey, gotRange string

	mux := http.NewServeMux()
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotAPIKey = r.Header.Get("X-API-Key")
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Range", "bytes 0-0/1")
		w.WriteHeader(http.StatusPartialContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := probe.ProbeServer(context.Background(), server.URL+"/redirect", "", map[string]string{
		"Authorization": "Bearer same-origin",
		"Cookie":        "session=same-origin",
		"X-API-Key":     "same-origin-key",
	})
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}

	if gotAuth != "Bearer same-origin" {
		t.Fatalf("authorization = %q, want preserved", gotAuth)
	}
	if gotCookie != "session=same-origin" {
		t.Fatalf("cookie = %q, want preserved", gotCookie)
	}
	if gotAPIKey != "same-origin-key" {
		t.Fatalf("x-api-key = %q, want preserved", gotAPIKey)
	}
	if gotRange != "bytes=0-0" {
		t.Fatalf("range = %q, want bytes=0-0", gotRange)
	}
}

func TestProbeRedirect_CrossOriginStripsExplicitHeaders(t *testing.T) {
	var gotAuth, gotCookie, gotRange string

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Range", "bytes 0-0/1")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
		http.Redirect(w, r, targetURL, http.StatusFound)
	}))
	defer redirect.Close()

	_, err := probe.ProbeServer(context.Background(), redirect.URL, "", map[string]string{
		"Authorization": "Bearer cross-origin",
		"Cookie":        "session=cross-origin",
	})
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}

	if gotAuth != "" {
		t.Fatalf("authorization leaked on cross-origin redirect: %q", gotAuth)
	}
	if gotCookie != "" {
		t.Fatalf("cookie leaked on cross-origin redirect: %q", gotCookie)
	}
	if gotRange != "bytes=0-0" {
		t.Fatalf("range = %q, want bytes=0-0", gotRange)
	}
}

func TestProbeServer_RetryWithoutRangeReusesHeaderSetup(t *testing.T) {
	var sawRangedRequest bool
	var gotAuth, gotUserAgent, gotRange string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			sawRangedRequest = true
			w.WriteHeader(http.StatusForbidden)
			return
		}

		gotAuth = r.Header.Get("Authorization")
		gotUserAgent = r.Header.Get("User-Agent")
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	res, err := probe.ProbeServer(context.Background(), server.URL, "", map[string]string{
		"Authorization": "Bearer retry-test",
	})
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}

	if !sawRangedRequest {
		t.Fatal("expected initial ranged probe request")
	}
	if gotAuth != "Bearer retry-test" {
		t.Fatalf("authorization = %q, want preserved on retry", gotAuth)
	}
	if gotUserAgent == "" {
		t.Fatal("expected retry request to keep a user-agent")
	}
	if gotRange != "" {
		t.Fatalf("range = %q, want empty on retry without range", gotRange)
	}
	if res.SupportsRange {
		t.Fatal("expected retry-without-range probe to report unsupported range")
	}
	if res.FileSize != 5 {
		t.Fatalf("fileSize = %d, want 5", res.FileSize)
	}
}

func TestProbeServerWithProxy_UsesRuntimeUserAgent(t *testing.T) {
	const wantUserAgent = "SurgeProbeTest/1.0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != wantUserAgent {
			t.Errorf("User-Agent = %q, want %q", got, wantUserAgent)
		}
		w.Header().Set("Content-Range", "bytes 0-0/1")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	if _, err := probe.ProbeServerWithProxy(context.Background(), server.URL, "", nil, &types.RuntimeConfig{UserAgent: wantUserAgent}); err != nil {
		t.Fatal(err)
	}
}
