package transport

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

type poolKey struct {
	proxyURL  string
	customDNS string
	maxConns  int
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

// sharedClientSessionCache is a process-lifetime LRU TLS session cache shared by
// all NetworkPool transports so new dials can resume across Transport rebuilds
// and poolKeys. Capacity 256 is host:port keyed. Do not clear on CloseAll.
var sharedClientSessionCache = tls.NewLRUClientSessionCache(256)

// AcquireTransport returns a shared transport for the given configuration.
func (p *NetworkPool) AcquireTransport(proxyURL, customDNS string, maxConns int) *http.Transport {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.configMap == nil {
		p.configMap = make(map[poolKey]*transportLease)
	}
	if p.transportMap == nil {
		p.transportMap = make(map[*http.Transport]*transportLease)
	}

	key := poolKey{proxyURL, customDNS, maxConns}

	lease, ok := p.configMap[key]
	if !ok {
		t := p.createNewTransport(proxyURL, customDNS, maxConns)
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

	return lease.transport
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

func (p *NetworkPool) createNewTransport(proxyURL, customDNS string, maxConns int) *http.Transport {
	utils.Debug("NetworkPool: creating new shared transport (proxy=%s, limit=%d)", proxyURL, maxConns)

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
		TLSNextProto:       make(map[string]func(string, *tls.Conn) http.RoundTripper),
		TLSClientConfig: &tls.Config{
			ClientSessionCache: sharedClientSessionCache,
		},
	}
}
