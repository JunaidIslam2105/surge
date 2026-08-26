package service

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/SurgeDM/Surge/internal/utils"
)

type HTTPClientOptions struct {
	Timeout            time.Duration
	InsecureSkipVerify bool
	CAFile             string
}

func NewHTTPClient(opts HTTPClientOptions) (*http.Client, error) {
	transport, err := NewHTTPTransport(opts)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
	}, nil
}

func NewStreamingHTTPClient(opts HTTPClientOptions) (*http.Client, error) {
	transport, err := NewHTTPTransport(opts)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: transport,
	}, nil
}

func NewHTTPTransport(opts HTTPClientOptions) (*http.Transport, error) {
	tlsConfig, err := newTLSConfig(opts)
	if err != nil {
		return nil, err
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}, nil
}

func newTLSConfig(opts HTTPClientOptions) (*tls.Config, error) {
	return utils.BuildTLSConfig(opts.CAFile, opts.InsecureSkipVerify)
}
