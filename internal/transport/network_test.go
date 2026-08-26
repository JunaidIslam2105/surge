package transport

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestNetworkPool_ClientSessionCache_SharedAcrossPoolKeys(t *testing.T) {
	pool := &NetworkPool{}

	t1, err := pool.AcquireTransport("http://proxy1", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	t2, err := pool.AcquireTransport("http://proxy2", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
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
}

func TestNetworkPool_CloseAll_PreservesClientSessionCache(t *testing.T) {
	pool := &NetworkPool{}

	tr, err := pool.AcquireTransport("", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.ClientSessionCache == nil {
		t.Fatal("expected wired ClientSessionCache before CloseAll")
	}
	cache := tr.TLSClientConfig.ClientSessionCache
	pool.ReleaseTransport(tr)

	pool.CloseAll()

	tr2, err := pool.AcquireTransport("", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	defer pool.ReleaseTransport(tr2)

	if tr2.TLSClientConfig == nil {
		t.Fatal("expected non-nil TLSClientConfig after CloseAll re-Acquire")
	}
	if tr2.TLSClientConfig.ClientSessionCache != cache {
		t.Fatal("expected ClientSessionCache to survive CloseAll")
	}
	assertHTTP2Disabled(t, tr2)
}

func TestNetworkPool_ClientSessionCache_HTTP2Disabled(t *testing.T) {
	pool := &NetworkPool{}
	tr, err := pool.AcquireTransport("", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
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
	transport1, err := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	client1 := &http.Client{Transport: transport1}
	req1, _ := http.NewRequest("GET", server.URL, nil)
	resp1, err := client1.Do(req1)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	_ = resp1.Body.Close()
	pool.ReleaseTransport(transport1)

	// Second request with trace
	transport2, err := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
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

	transport, err := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
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
	transport2, err := pool.AcquireTransport(runtime.ProxyURL, runtime.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	defer pool.ReleaseTransport(transport2)
	if lease.idleTimer != nil {
		t.Error("Expected idle timer to be stopped after AcquireTransport()")
	}
	pool.ReleaseTransport(transport)
}

func TestNetworkPool_ConfigChange(t *testing.T) {
	pool := &NetworkPool{}

	r1 := &types.RuntimeConfig{ProxyURL: "http://proxy1"}
	t1, err := pool.AcquireTransport(r1.ProxyURL, r1.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	pool.ReleaseTransport(t1)

	r2 := &types.RuntimeConfig{ProxyURL: "http://proxy2"}
	t2, err := pool.AcquireTransport(r2.ProxyURL, r2.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	pool.ReleaseTransport(t2)

	if t1 == t2 {
		t.Error("Expected different transport after config change")
	}

	// Get with same config should reuse
	t3, err := pool.AcquireTransport(r2.ProxyURL, r2.CustomDNS, 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	pool.ReleaseTransport(t3)

	if t2 != t3 {
		t.Error("Expected transport reuse for identical config")
	}
}

// TestNetworkPool_TLSKeyIsolation verifies that different TLS settings produce
// distinct pool entries and that insecure=true sets InsecureSkipVerify on the transport.
func TestNetworkPool_TLSKeyIsolation(t *testing.T) {
	pool := &NetworkPool{}

	plain, err := pool.AcquireTransport("", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	pool.ReleaseTransport(plain)

	insecure, err := pool.AcquireTransport("", "", 0, "", true)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	pool.ReleaseTransport(insecure)

	if plain == insecure {
		t.Fatal("expected distinct transports for different TLS configs")
	}

	tlsCfg := insecure.TLSClientConfig
	if tlsCfg == nil {
		t.Fatal("expected TLSClientConfig to be set for insecure transport")
	}
	if !tlsCfg.InsecureSkipVerify { //nolint:gosec // intentional test assertion
		t.Error("expected InsecureSkipVerify=true on insecure transport")
	}

	// A plain transport must not skip verification.
	if cfg := plain.TLSClientConfig; cfg != nil {
		if cfg.InsecureSkipVerify { //nolint:gosec
			t.Error("plain transport must not have InsecureSkipVerify set")
		}
	}

	// Verify that invalid CA file returns an error
	withCA, err := pool.AcquireTransport("", "", 0, "nonexistent-but-distinct-key.pem", false)
	if err == nil {
		t.Error("expected error when CA file does not exist")
		pool.ReleaseTransport(withCA)
	}

	// Requesting the same key twice returns the same transport (pool reuse)
	a, err := pool.AcquireTransport("", "", 0, "", true)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	b, err := pool.AcquireTransport("", "", 0, "", true)
	if err != nil {
		t.Fatalf("failed to acquire transport: %v", err)
	}
	pool.ReleaseTransport(a)
	pool.ReleaseTransport(b)
	if a != b {
		t.Error("expected pool to reuse transport for identical TLS config")
	}
	// Verify the insecure transport has the correct TLS config
	if a.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig on reused insecure transport")
	}
	cfg := a.TLSClientConfig
	if !cfg.InsecureSkipVerify { //nolint:gosec
		t.Error("reused insecure transport must still have InsecureSkipVerify=true")
	}
	_ = tls.Config{} // keep tls import used
}

func TestNetworkPool_CAPathNormalization(t *testing.T) {
	pool := &NetworkPool{}

	dummyCert := `-----BEGIN CERTIFICATE-----
MIICyjCCAbKgAwIBAgIBATANBgkqhkiG9w0BAQsFADAPMQ0wCwYDVQQKEwRUZXN0
MB4XDTI2MDgwMjA4MDYwMFoXDTI2MDgwMjA5MDYwMFowDzENMAsGA1UEChMEVGVz
dDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMMlFG2Y3KO2vUL2zkgj
F4Lt5ZGCc+QDPQhIb1BQKTMVp5qLMbDdj8UdbVSJvBttVLORuXHA9JcrIxS+kNMY
++OaIPHY8T7bYiTAvrFeQwrAOBimcgF7rozlnlmKJcN/XNrAzV2XsJYj19hU6qMS
Tlg8HNvP+Kwhn/vNAojdivwJLxq7Sdnzwab2SihfzsitZU+rCk4lNLeeme+40yBP
FSxG3hd/Cx2Kup+Uf8FVBIiJe3cCMwVHUov5BefgoYRTGmu+dfWHAZSRx5tNh12H
Nc/hzRnrxDD07A+ebBxK8CXeIlJWhMZh033l+YU+AdmdeM0+iYAXc/tPnO60Y1h+
H5UCAwEAAaMxMC8wDgYDVR0PAQH/BAQDAgIEMB0GA1UdDgQWBBQII54A6/rxppec
MJZg3sLfrWYmrDANBgkqhkiG9w0BAQsFAAOCAQEAYBZNYEK5bZx8Yc6nGH0GXqQy
FogInhACTeuU57EfMRuidIgf34PE5BKS2vsAeewEvay3g9g8B1E90cedYp/bm3De
hGoDv4lfAy9mGxAYWsyPzEvmrk9pz+LiIdlaiXnx/XB0ROrxPX8uGRT4Ai0UpxUf
m0NwWW3U9en5rKT2mh4C+DAV+z7EUyAEhErnvi9Cyt8uDdG7ePaMkURAKHk3UDku
oJ6zu3yqryOxDx3vS2YgLN9Qi/i84wNH4kdqO7hvSp6XgjHCD2xqUNWAzp03hg+Y
Zi0UEBDDbjOfYNXKGyPnGr7HulifnCnboNLOeDEWQof5PKeaR2cf+8cI2BDX9A==
-----END CERTIFICATE-----`

	caFile, err := os.CreateTemp("", "ca-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(caFile.Name()) }()
	
	if _, err := caFile.Write([]byte(dummyCert)); err != nil {
		t.Fatalf("failed to write dummy cert: %v", err)
	}
	_ = caFile.Close()

	caPath := caFile.Name()
	caPathPadded := "  \t" + caPath + "\n "

	t1, err := pool.AcquireTransport("", "", 0, caPath, false)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer pool.ReleaseTransport(t1)

	t2, err := pool.AcquireTransport("", "", 0, caPathPadded, false)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	defer pool.ReleaseTransport(t2)

	if t1 != t2 {
		t.Fatal("expected transports to share the same pool lease due to path normalization")
	}
}

func TestNetworkPool_SessionCachePartitioning(t *testing.T) {
	pool := &NetworkPool{}

	dummyCert := `-----BEGIN CERTIFICATE-----
MIICyjCCAbKgAwIBAgIBATANBgkqhkiG9w0BAQsFADAPMQ0wCwYDVQQKEwRUZXN0
MB4XDTI2MDgwMjA4MDYwMFoXDTI2MDgwMjA5MDYwMFowDzENMAsGA1UEChMEVGVz
dDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMMlFG2Y3KO2vUL2zkgj
F4Lt5ZGCc+QDPQhIb1BQKTMVp5qLMbDdj8UdbVSJvBttVLORuXHA9JcrIxS+kNMY
++OaIPHY8T7bYiTAvrFeQwrAOBimcgF7rozlnlmKJcN/XNrAzV2XsJYj19hU6qMS
Tlg8HNvP+Kwhn/vNAojdivwJLxq7Sdnzwab2SihfzsitZU+rCk4lNLeeme+40yBP
FSxG3hd/Cx2Kup+Uf8FVBIiJe3cCMwVHUov5BefgoYRTGmu+dfWHAZSRx5tNh12H
Nc/hzRnrxDD07A+ebBxK8CXeIlJWhMZh033l+YU+AdmdeM0+iYAXc/tPnO60Y1h+
H5UCAwEAAaMxMC8wDgYDVR0PAQH/BAQDAgIEMB0GA1UdDgQWBBQII54A6/rxppec
MJZg3sLfrWYmrDANBgkqhkiG9w0BAQsFAAOCAQEAYBZNYEK5bZx8Yc6nGH0GXqQy
FogInhACTeuU57EfMRuidIgf34PE5BKS2vsAeewEvay3g9g8B1E90cedYp/bm3De
hGoDv4lfAy9mGxAYWsyPzEvmrk9pz+LiIdlaiXnx/XB0ROrxPX8uGRT4Ai0UpxUf
m0NwWW3U9en5rKT2mh4C+DAV+z7EUyAEhErnvi9Cyt8uDdG7ePaMkURAKHk3UDku
oJ6zu3yqryOxDx3vS2YgLN9Qi/i84wNH4kdqO7hvSp6XgjHCD2xqUNWAzp03hg+Y
Zi0UEBDDbjOfYNXKGyPnGr7HulifnCnboNLOeDEWQof5PKeaR2cf+8cI2BDX9A==
-----END CERTIFICATE-----`

	caFile1, err := os.CreateTemp("", "ca1-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(caFile1.Name()) }()
	if _, err := caFile1.Write([]byte(dummyCert)); err != nil {
		t.Fatalf("failed to write cert: %v", err)
	}
	_ = caFile1.Close()

	caFile2, err := os.CreateTemp("", "ca2-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(caFile2.Name()) }()
	if _, err := caFile2.Write([]byte(dummyCert)); err != nil {
		t.Fatalf("failed to write cert: %v", err)
	}
	_ = caFile2.Close()

	// Transport 1: Default TLS policy (no CA file, not insecure)
	t1, err := pool.AcquireTransport("", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire t1: %v", err)
	}
	defer pool.ReleaseTransport(t1)

	// Transport 2: Insecure TLS policy
	t2, err := pool.AcquireTransport("", "", 0, "", true)
	if err != nil {
		t.Fatalf("failed to acquire t2: %v", err)
	}
	defer pool.ReleaseTransport(t2)

	if t1.TLSClientConfig.ClientSessionCache == t2.TLSClientConfig.ClientSessionCache {
		t.Fatal("expected distinct session caches for different TLS policies")
	}

	// Transport 3: Re-acquire Default TLS policy to verify resumption works within policy
	t3, err := pool.AcquireTransport("", "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to acquire t3: %v", err)
	}
	defer pool.ReleaseTransport(t3)
	
	if t1.TLSClientConfig.ClientSessionCache != t3.TLSClientConfig.ClientSessionCache {
		t.Fatal("expected session cache to be shared within same TLS policy")
	}

	// Transport 4: Custom CA File 1
	t4, err := pool.AcquireTransport("", "", 0, caFile1.Name(), false)
	if err != nil {
		t.Fatalf("failed to acquire t4: %v", err)
	}
	defer pool.ReleaseTransport(t4)

	// Transport 5: Custom CA File 2 (distinct file but identical cert, tests that partition key is path)
	t5, err := pool.AcquireTransport("", "", 0, caFile2.Name(), false)
	if err != nil {
		t.Fatalf("failed to acquire t5: %v", err)
	}
	defer pool.ReleaseTransport(t5)

	if t4.TLSClientConfig.ClientSessionCache == t5.TLSClientConfig.ClientSessionCache {
		t.Fatal("expected distinct session caches for different CA file paths")
	}
}
