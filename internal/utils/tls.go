package utils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// BuildTLSConfig returns a *tls.Config for the given CA file and insecure flag,
// or nil when both are at their zero value (callers that pass nil get Go's default
// system trust store, which is the correct behaviour for the common case).
func BuildTLSConfig(caFile string, insecure bool) (*tls.Config, error) {
	caFile = strings.TrimSpace(caFile)
	if !insecure && caFile == "" {
		return nil, nil
	}

	cfg := &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec // controlled by explicit user flag

	if caFile == "" {
		return cfg, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %q: %w", caFile, err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("read CA file %q: no certificates found", caFile)
	}

	cfg.RootCAs = pool
	return cfg, nil
}
