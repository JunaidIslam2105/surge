package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// ErrInvalidTLSConfig is returned when the provided TLS configuration (e.g. CA file) cannot be parsed or read.
var ErrInvalidTLSConfig = errors.New("invalid TLS configuration")

type poolKey struct {
	proxyURL    string
	customDNS   string
	maxConns    int
	tlsCAFile   string
	tlsInsecure bool
}

// transportLease tracks a specific transport's usage and cleanup lifecycle.
type transportLease struct {
	transport *http.Transport
	refs      int
	idleTimer *time.Timer
	timerGen  int
	key       poolKey
}

// NetworkPool manages shared HTTP transports for TCP connection reuse.
type NetworkPool struct {
	mu           sync.Mutex
	configMap    map[poolKey]*transportLease
	transportMap map[*http.Transport]*transportLease
}

// DefaultNetworkPool is the global instance managed by the engine layer.
var DefaultNetworkPool = &NetworkPool{
	configMap:    make(map[poolKey]*transportLease),
	transportMap: make(map[*http.Transport]*transportLease),
}

// sessionCacheMu protects sessionCaches.
var sessionCacheMu sync.Mutex

// sessionCaches is a process-lifetime LRU TLS session cache partitioned by TLS policy.
// It ensures that sessions cannot cross trust-policy boundaries.
var sessionCaches = make(map[tlsPolicyKey]tls.ClientSessionCache)

type tlsPolicyKey struct {
	caFile   string
	insecure bool
}

func getSessionCache(caFile string, insecure bool) tls.ClientSessionCache {
	sessionCacheMu.Lock()
	defer sessionCacheMu.Unlock()
	key := tlsPolicyKey{caFile, insecure}
	if c, ok := sessionCaches[key]; ok {
		return c
	}
	c := tls.NewLRUClientSessionCache(256)
	sessionCaches[key] = c
	return c
}

// AcquireTransport returns a shared transport for the given configuration.
func (p *NetworkPool) AcquireTransport(proxyURL, customDNS string, maxConns int, tlsCAFile string, tlsInsecure bool) (*http.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.configMap == nil {
		p.configMap = make(map[poolKey]*transportLease)
	}
	if p.transportMap == nil {
		p.transportMap = make(map[*http.Transport]*transportLease)
	}

	tlsCAFile = strings.TrimSpace(tlsCAFile)
	key := poolKey{proxyURL, customDNS, maxConns, tlsCAFile, tlsInsecure}

	lease, ok := p.configMap[key]
	if !ok {
		t, err := p.createNewTransport(proxyURL, customDNS, maxConns, tlsCAFile, tlsInsecure)
		if err != nil {
			return nil, err
		}
		lease = &transportLease{
			transport: t,
			key:       key,
		}
		p.configMap[key] = lease
		p.transportMap[t] = lease
	}

	if lease.idleTimer != nil {
		lease.idleTimer.Stop()
		lease.idleTimer = nil
		lease.timerGen++
	}

	lease.refs++
	utils.Debug("NetworkPool: AcquireTransport (key=%+v, refs=%d)", key, lease.refs)

	return lease.transport, nil
}

// ReleaseTransport marks a specific transport lease as returned.
func (p *NetworkPool) ReleaseTransport(t *http.Transport) {
	if t == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	lease, ok := p.transportMap[t]
	if !ok {
		utils.Debug("NetworkPool: ReleaseTransport called for unmanaged transport")
		return
	}

	lease.refs--
	if lease.refs < 0 {
		lease.refs = 0
	}
	utils.Debug("NetworkPool: ReleaseTransport (key=%+v, refs=%d)", lease.key, lease.refs)

	if lease.refs == 0 {
		lease.timerGen++
		gen := lease.timerGen
		lease.idleTimer = time.AfterFunc(10*time.Second, func() {
			p.mu.Lock()
			defer p.mu.Unlock()

			if lease.refs == 0 && lease.timerGen == gen {
				utils.Debug("NetworkPool: idle timeout reached, evicting transport")
				lease.transport.CloseIdleConnections()
				delete(p.configMap, lease.key)
				delete(p.transportMap, lease.transport)
			}
		})
	}
}

// CloseAll evicts all transports and closes their idle connections immediately.
func (p *NetworkPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, lease := range p.transportMap {
		if lease.idleTimer != nil {
			lease.idleTimer.Stop()
		}
		lease.transport.CloseIdleConnections()
	}

	p.configMap = make(map[poolKey]*transportLease)
	p.transportMap = make(map[*http.Transport]*transportLease)
}

func (p *NetworkPool) createNewTransport(proxyURL, customDNS string, maxConns int, tlsCAFile string, tlsInsecure bool) (*http.Transport, error) {
	utils.Debug("NetworkPool: creating new shared transport (proxy=%s, limit=%d)", proxyURL, maxConns)

	tlsCfg, err := utils.BuildTLSConfig(tlsCAFile, tlsInsecure)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTLSConfig, err)
	}
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}
	tlsCfg.ClientSessionCache = getSessionCache(tlsCAFile, tlsInsecure)

	dialer := &net.Dialer{
		Timeout:   types.DialTimeout,
		KeepAlive: types.KeepAliveDuration,
	}
	utils.ConfigureDialer(dialer, customDNS)

	proxyFunc := http.ProxyFromEnvironment
	if proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			proxyFunc = http.ProxyURL(parsed)
		} else {
			utils.Debug("NetworkPool: invalid proxy URL %s: %v", proxyURL, err)
		}
	}

	finalMaxConns := maxConns
	if finalMaxConns <= 0 {
		finalMaxConns = types.PoolMaxConnsPerHost
	}

	return &http.Transport{
		Proxy: proxyFunc,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},

		MaxIdleConns:        types.PoolMaxIdleConns,
		MaxIdleConnsPerHost: types.PoolMaxIdleConnsPerHost,
		MaxConnsPerHost:     finalMaxConns,

		IdleConnTimeout:       types.DefaultIdleConnTimeout,
		TLSHandshakeTimeout:   types.DefaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: types.DefaultResponseHeaderTimeout,
		ExpectContinueTimeout: types.DefaultExpectContinueTimeout,

		DisableCompression: true,
		ForceAttemptHTTP2:  false,
		TLSClientConfig:    tlsCfg,
		TLSNextProto:       make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}, nil
}
