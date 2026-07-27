package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestNetworkPool_ClientSessionCache_SharedAcrossPoolKeys(t *testing.T) {
	pool := &NetworkPool{}

	t1 := pool.AcquireTransport("http://proxy1", "", 0)
	t2 := pool.AcquireTransport("http://proxy2", "", 0)
	defer pool.ReleaseTransport(t1)
	defer pool.ReleaseTransport(t2)

	if t1 == t2 {
		t.Fatal("expected distinct transports for different poolKeys")
	}
	if t1.TLSClientConfig == nil || t2.TLSClientConfig == nil {
		t.Fatal("expected non-nil TLSClientConfig on both transports")
	}
	if t1.TLSClientConfig == t2.TLSClientConfig {
		t.Fatal("expected distinct *tls.Config instances per transport")
	}
	if t1.TLSClientConfig.ClientSessionCache == nil {
		t.Fatal("expected non-nil ClientSessionCache")
	}
	if t1.TLSClientConfig.ClientSessionCache != t2.TLSClientConfig.ClientSessionCache {
		t.Fatal("expected shared ClientSessionCache pointer across poolKeys")
	}
	if t1.TLSClientConfig.ClientSessionCache != sharedClientSessionCache {
		t.Fatal("expected ClientSessionCache to be package sharedClientSessionCache")
	}
}

func TestNetworkPool_CloseAll_PreservesClientSessionCache(t *testing.T) {
	pool := &NetworkPool{}

	tr := pool.AcquireTransport("", "", 0)
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.ClientSessionCache == nil {
		t.Fatal("expected wired ClientSessionCache before CloseAll")
	}
	cache := tr.TLSClientConfig.ClientSessionCache
	pool.ReleaseTransport(tr)

	pool.CloseAll()

	tr2 := pool.AcquireTransport("", "", 0)
	defer pool.ReleaseTransport(tr2)

	if tr2.TLSClientConfig == nil {
		t.Fatal("expected non-nil TLSClientConfig after CloseAll re-Acquire")
	}
	if tr2.TLSClientConfig.ClientSessionCache != cache {
		t.Fatal("expected ClientSessionCache to survive CloseAll")
	}
	if tr2.TLSClientConfig.ClientSessionCache != sharedClientSessionCache {
		t.Fatal("expected surviving cache to remain package sharedClientSessionCache")
	}
	assertHTTP2Disabled(t, tr2)
}

func TestNetworkPool_ClientSessionCache_HTTP2Disabled(t *testing.T) {
	pool := &NetworkPool{}
	tr := pool.AcquireTransport("", "", 0)
	defer pool.ReleaseTransport(tr)
	assertHTTP2Disabled(t, tr)
}

func assertHTTP2Disabled(t *testing.T, tr *http.Transport) {
	t.Helper()
	if tr.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 == false")
	}
	if tr.TLSNextProto == nil {
		t.Error("expected non-nil TLSNextProto map")
	} else if len(tr.TLSNextProto) != 0 {
		t.Errorf("expected empty TLSNextProto, got len=%d", len(tr.TLSNextProto))
	}
	if tr.DialContext == nil {
		t.Error("expected custom DialContext")
	}
}

func TestNetworkPool_Reuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pool := &NetworkPool{}
	runtime := &types.RuntimeConfig{}

	// First request
	transport1 := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0)
	client1 := &http.Client{Transport: transport1}
	req1, _ := http.NewRequest("GET", server.URL, nil)
	resp1, err := client1.Do(req1)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	_ = resp1.Body.Close()
	pool.ReleaseTransport(transport1)

	// Second request with trace
	transport2 := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0)
	client2 := &http.Client{Transport: transport2}
	reused := false
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				reused = true
			}
		},
	}
	req2, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), "GET", server.URL, nil)
	resp2, err := client2.Do(req2)
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	_ = resp2.Body.Close()
	pool.ReleaseTransport(transport2)

	if !reused {
		t.Error("Expected connection to be reused")
	}
}

func TestNetworkPool_IdleCleanup(t *testing.T) {
	pool := &NetworkPool{}
	runtime := &types.RuntimeConfig{}

	transport := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0)
	lease, ok := pool.transportMap[transport]
	if !ok {
		t.Fatal("Expected transport to be in transportMap")
	}

	if lease.refs != 1 {
		t.Errorf("Expected refs=1, got %d", lease.refs)
	}
	if lease.idleTimer != nil {
		t.Error("Expected no idle timer when refs > 0")
	}

	pool.ReleaseTransport(transport)
	if lease.refs != 0 {
		t.Errorf("Expected refs=0, got %d", lease.refs)
	}
	if lease.idleTimer == nil {
		t.Error("Expected idle timer to be started after ReleaseTransport()")
	}

	// Calling AcquireTransport again should stop the timer
	pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0)
	if lease.idleTimer != nil {
		t.Error("Expected idle timer to be stopped after AcquireTransport()")
	}
	pool.ReleaseTransport(transport)
}

func TestNetworkPool_ConfigChange(t *testing.T) {
	pool := &NetworkPool{}

	r1 := &types.RuntimeConfig{ProxyURL: "http://proxy1"}
	t1 := pool.AcquireTransport(r1.ProxyURL, r1.CustomDNS, 0)
	pool.ReleaseTransport(t1)

	r2 := &types.RuntimeConfig{ProxyURL: "http://proxy2"}
	t2 := pool.AcquireTransport(r2.ProxyURL, r2.CustomDNS, 0)
	pool.ReleaseTransport(t2)

	if t1 == t2 {
		t.Error("Expected different transport after config change")
	}

	// Get with same config should reuse
	t3 := pool.AcquireTransport(r2.ProxyURL, r2.CustomDNS, 0)
	pool.ReleaseTransport(t3)

	if t2 != t3 {
		t.Error("Expected transport reuse for identical config")
	}
}
