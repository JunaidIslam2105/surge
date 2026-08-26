package concurrent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"testing"

	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func TestPrewarmConnections_Reuse(t *testing.T) {
	fileSize := int64(1 * utils.KiB)
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server.Close()

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerDownload: 2,
		DialHedgeCount:            0,
	}

	downloader := NewConcurrentDownloader("test-reuse", nil, nil, runtime)
	tr, err := transport.DefaultNetworkPool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, runtime.GetMaxConnectionsPerDownload(), "", false)
	if err != nil {
		t.Fatalf("AcquireTransport failed: %v", err)
	}
	defer transport.DefaultNetworkPool.ReleaseTransport(tr)
	client := &http.Client{Transport: tr}

	ctx := context.Background()
	mirrors := []string{server.URL()}

	// 1. Prewarm connections
	// This should populate the idle pool with one connection
	downloader.prewarmConnections(ctx, client, 1, 0, mirrors)

	// 2. Perform a request and check for reuse
	reused := false
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				reused = true
			}
		},
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, server.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if !reused {
		t.Error("Expected connection to be reused after prewarming, but it was not. Handshake leak likely present.")
	}
}
