package utils

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSameSite(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		// Exact matches
		{"exact same domain", "example.com", "example.com", true},
		{"exact same IP", "127.0.0.1", "127.0.0.1", true},
		{"exact same IPv6", "[::1]", "[::1]", true},

		// Subdomains on standard TLDs
		{"standard TLD subdomains", "a.example.com", "b.example.com", true},
		{"deep subdomains", "x.y.z.example.com", "example.com", true},
		{"easynews subdomains", "members.easynews.com", "iad-dl-08.easynews.com", true},

		// Complex TLDs (eTLD+1)
		{"complex TLD subdomains", "a.example.co.uk", "b.example.co.uk", true},
		{"different sites on complex TLD", "example.co.uk", "other.co.uk", false},
		{"github.io subdomains (private TLD)", "user1.github.io", "user2.github.io", false},
		{"github.io same site", "user1.github.io", "sub.user1.github.io", true},

		// Cross-site cases
		{"filmyzilla cross-site domains", "1.filmyzilla.vin", "cdn-02-nl-zilla.filmyzdl.com", false},
		{"different domains", "example.com", "other.com", false},
		{"different TLDs", "example.com", "example.org", false},
		{"IP vs localhost", "127.0.0.1", "localhost", false},
		{"different IPs", "192.168.1.1", "192.168.1.2", false},

		// Port handling
		{"with same ports", "example.com:8080", "sub.example.com:8080", true},
		{"with different ports", "example.com:80", "sub.example.com:443", true},
		{"IP with port", "127.0.0.1:8080", "127.0.0.1:9090", true},
		{"one with port, one without", "example.com", "sub.example.com:8080", true},

		// Malformed or empty
		{"empty strings", "", "", true},
		{"one empty string", "example.com", "", false},
		{"invalid host format", "invalid host name", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameSite(tt.a, tt.b); got != tt.want {
				t.Errorf("SameSite(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCopyRedirectHeaders(t *testing.T) {
	// Go's http.Client inherently forwards standard headers (Range, User-Agent)
	// on redirects, but drops credentials (Cookie, Authorization) on cross-origin redirects.
	// CopyRedirectHeaders is designed to restore credentials specifically for cross-origin BUT same-site redirects.

	tests := []struct {
		name           string
		dstURL         string
		srcURL         string
		srcHeaders     http.Header
		initialDstHdr  http.Header
		expectedDstHdr http.Header
	}{
		// Same-site credential restoration
		{
			name:   "same-site easynews restores stripped credentials",
			dstURL: "https://iad-dl-08.easynews.com/file",
			srcURL: "https://members.easynews.com/file",
			srcHeaders: http.Header{
				"Authorization": []string{"Basic dXNlcjpwYXNz"},
				"Cookie":        []string{"session=123"},
				"Cookie2":       []string{"legacy=456"},
				"Range":         []string{"bytes=0-100"},
				"User-Agent":    []string{"Surge/1.0"},
			},
			initialDstHdr: http.Header{
				"Range":      []string{"bytes=0-100"},
				"User-Agent": []string{"Surge/1.0"},
			},
			expectedDstHdr: http.Header{
				"Authorization": []string{"Basic dXNlcjpwYXNz"},
				"Cookie":        []string{"session=123"},
				"Cookie2":       []string{"legacy=456"},
				"Range":         []string{"bytes=0-100"},
				"User-Agent":    []string{"Surge/1.0"},
			},
		},
		{
			name:   "same-site with only Cookie",
			dstURL: "https://auth.example.com",
			srcURL: "https://api.example.com",
			srcHeaders: http.Header{
				"Cookie": []string{"token=foo"},
			},
			initialDstHdr: http.Header{},
			expectedDstHdr: http.Header{
				"Cookie": []string{"token=foo"},
			},
		},
		// Cross-site credential stripping (trusting Go)
		{
			name:   "cross-site filmyzilla respects standard library stripping",
			dstURL: "https://cdn-02-nl-zilla.filmyzdl.com/movie.mp4",
			srcURL: "https://1.filmyzilla.vin/download",
			srcHeaders: http.Header{
				"Cookie":     []string{"sess=abc"},
				"Referer":    []string{"https://1.filmyzilla.vin/"},
				"User-Agent": []string{"Surge/1.0"},
			},
			initialDstHdr: http.Header{
				// Go stripped Cookie, updated Referer, kept User-Agent
				"Referer":    []string{"https://1.filmyzilla.vin/"},
				"User-Agent": []string{"Surge/1.0"},
			},
			expectedDstHdr: http.Header{
				"Referer":    []string{"https://1.filmyzilla.vin/"},
				"User-Agent": []string{"Surge/1.0"},
			},
		},
		{
			name:   "cross-scheme (http to https) is treated as cross-site",
			dstURL: "https://example.com/file",
			srcURL: "http://example.com/file",
			srcHeaders: http.Header{
				"Authorization": []string{"Basic dXNlcjpwYXNz"},
				"Cookie":        []string{"session=123"},
				"Range":         []string{"bytes=0-100"},
			},
			initialDstHdr: http.Header{
				"Range": []string{"bytes=0-100"},
			},
			expectedDstHdr: http.Header{
				"Range": []string{"bytes=0-100"},
			},
		},
		// Edge cases
		{
			name:           "nil request headers safe check",
			dstURL:         "https://b.example.com",
			srcURL:         "https://a.example.com",
			srcHeaders:     nil, // shouldn't panic
			initialDstHdr:  http.Header{},
			expectedDstHdr: http.Header{},
		},
		{
			name:   "no credentials to restore",
			dstURL: "https://b.example.com",
			srcURL: "https://a.example.com",
			srcHeaders: http.Header{
				"X-Custom-Header": []string{"value"},
			},
			initialDstHdr: http.Header{
				"X-Custom-Header": []string{"value"},
			},
			expectedDstHdr: http.Header{
				"X-Custom-Header": []string{"value"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dstReq := &http.Request{
				URL:    mustParseURL(t, tt.dstURL),
				Header: tt.initialDstHdr,
			}
			srcReq := &http.Request{
				URL:    mustParseURL(t, tt.srcURL),
				Header: tt.srcHeaders,
			}

			CopyRedirectHeaders(dstReq, srcReq)

			if len(dstReq.Header) != len(tt.expectedDstHdr) {
				t.Errorf("Header count mismatch. got %v, want %v", dstReq.Header, tt.expectedDstHdr)
			}
			for k, expectedVals := range tt.expectedDstHdr {
				gotVals := dstReq.Header.Values(k)
				if !equalSlice(gotVals, expectedVals) {
					t.Errorf("Header[%s] = %v, want %v", k, gotVals, expectedVals)
				}
			}
			for k := range dstReq.Header {
				if _, ok := tt.expectedDstHdr[k]; !ok {
					t.Errorf("Unexpected header in dst: %s = %v", k, dstReq.Header.Values(k))
				}
			}
		})
	}
}

func TestCopyRedirectHeaders_NilRequests(t *testing.T) {
	// Should not panic
	CopyRedirectHeaders(nil, nil)

	req := &http.Request{URL: mustParseURL(t, "https://example.com")}
	CopyRedirectHeaders(req, nil)
	CopyRedirectHeaders(nil, req)

	reqNoURL := &http.Request{}
	CopyRedirectHeaders(req, reqNoURL)
	CopyRedirectHeaders(reqNoURL, req)
}

func mustParseURL(t *testing.T, s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("failed to parse url %q: %v", s, err)
	}
	return u
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
