package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

type probeHostLock struct {
	mu   sync.Mutex
	refs int
}

var probeHostLocks = struct {
	sync.Mutex
	hosts map[string]*probeHostLock
}{hosts: make(map[string]*probeHostLock)}

// ErrProbeRequestCreation is returned when a probe request cannot be initialized.
var ErrProbeRequestCreation = errors.New("failed to create probe request")

// ProbeResult contains all metadata from server probe
type ProbeResult struct {
	FileSize         int64
	SupportsRange    bool
	Filename         string
	DetectedFilename string
	ContentType      string
}

// probeHeadersContextKey is used to pass custom headers to the HTTP client's CheckRedirect function
type probeHeadersContextKey struct{}

// ProbeServer is the convenience entry point for callers that already hold a
// runtime config snapshot. For backwards compatibility, passing nil will use
// a zero-value RuntimeConfig.
func ProbeServer(ctx context.Context, rawurl string, filenameHint string, headers map[string]string) (*ProbeResult, error) {
	return ProbeServerWithProxy(ctx, rawurl, filenameHint, headers, nil)
}

// lockProbeHost sequentializes probes to one host without retaining a lock for every host ever seen.
func lockProbeHost(rawurl string) func() {
	parsed, err := neturl.Parse(rawurl)
	host := "unknown"
	if err == nil {
		host = parsed.Host
	}

	probeHostLocks.Lock()
	lock := probeHostLocks.hosts[host]
	if lock == nil {
		lock = &probeHostLock{}
		probeHostLocks.hosts[host] = lock
	}
	lock.refs++
	probeHostLocks.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		probeHostLocks.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(probeHostLocks.hosts, host)
		}
		probeHostLocks.Unlock()
	}
}

// ProbeServerWithProxy is the hot-path variant for callers that already know
// the effective proxy and want probe traffic to match the eventual download path
// without re-reading settings from disk.
func ProbeServerWithProxy(ctx context.Context, rawurl string, filenameHint string, headers map[string]string, runCfg *types.RuntimeConfig) (*ProbeResult, error) {
	utils.Debug("Probing server: %s", rawurl)

	// Embed custom headers in context so CheckRedirect can use them
	if headers != nil {
		ctx = context.WithValue(ctx, probeHeadersContextKey{}, headers)
	}

	var resp *http.Response

	var proxyURL, customDNS string
	if runCfg != nil {
		proxyURL = runCfg.ProxyURL
		customDNS = runCfg.CustomDNS
	}

	transport_ := transport.DefaultNetworkPool.AcquireTransport(proxyURL, customDNS, 0)
	defer transport.DefaultNetworkPool.ReleaseTransport(transport_)

	client := &http.Client{
		Transport: transport_,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if len(via) > 0 {
				utils.CopyRedirectHeaders(req, via[0])
			}
			return nil
		},
	}

	// Sequentialize probes to the same host to prevent rate limiting (e.g., Google Drive)
	defer lockProbeHost(rawurl)()

	var err error
	var finalCancel context.CancelFunc

	for attempt := range 3 {
		if ctx.Err() != nil {
			if err == nil {
				err = fmt.Errorf("probe request aborted: %w", ctx.Err())
			}
			break
		}

		if attempt > 0 {
			select {
			case <-ctx.Done():
				err = fmt.Errorf("probe request aborted during retry: %w", ctx.Err())
			case <-time.After(1 * time.Second):
			}
			if ctx.Err() != nil {
				break
			}
			utils.Debug("Retrying probe... attempt %d", attempt+1)
		}

		probeCtx, cancel := context.WithTimeout(ctx, types.ProbeTimeout)

		req, reqErr := newProbeRequest(probeCtx, rawurl, headers, runCfg, true)
		if reqErr != nil {
			cancel()
			err = fmt.Errorf("%w: %w", ErrProbeRequestCreation, reqErr)
			break
		}

		resp, err = client.Do(req)

		// Some origins reject ranged probes outright; a second request without Range
		// lets us still discover filename and size for sequential downloads.
		if err == nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusMethodNotAllowed) {
			utils.Debug("Probe got %d, retrying without Range header", resp.StatusCode)
			_ = resp.Body.Close() // Close previous response

			reqNoRange, reqNoRangeErr := newProbeRequest(probeCtx, rawurl, headers, runCfg, false)
			if reqNoRangeErr != nil {
				cancel()
				err = fmt.Errorf("%w without range: %w", ErrProbeRequestCreation, reqNoRangeErr)
				break
			}

			resp, err = client.Do(reqNoRange)
		}

		if err == nil {
			finalCancel = cancel
			break
		}

		cancel()
	}

	if err != nil {
		return nil, fmt.Errorf("probe request failed after retries: %w", err)
	}

	if finalCancel != nil {
		defer finalCancel()
	}

	defer func() {
		// Only drain a small amount of data (32KB) to allow connection reuse for small responses (e.g., 206 Partial Content).
		// For large responses (e.g., 200 OK), reading the whole file into discard takes too long.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32*utils.KiB))
		_ = resp.Body.Close()
	}()

	utils.Debug("Probe response status: %d", resp.StatusCode)

	result := &ProbeResult{}

	// Only a 206 response proves resume-safe range support; a 200 means the
	// downloader must fall back to a single sequential stream.
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206
		result.SupportsRange = true
		contentRange := resp.Header.Get("Content-Range")
		utils.Debug("Content-Range header: %s", contentRange)
		if contentRange != "" {
			if idx := strings.LastIndex(contentRange, "/"); idx != -1 {
				sizeStr := contentRange[idx+1:]
				if sizeStr != "*" {
					result.FileSize, _ = strconv.ParseInt(sizeStr, 10, 64)
				}
			}
		}
		utils.Debug("Range supported, file size: %d", result.FileSize)

	case http.StatusOK: // 200 - server ignores Range header
		result.SupportsRange = false
		contentLength := resp.Header.Get("Content-Length")
		if contentLength != "" {
			result.FileSize, _ = strconv.ParseInt(contentLength, 10, 64)
		}
		utils.Debug("Range NOT supported (got 200), file size: %d", result.FileSize)

	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	name, _, err := utils.DetermineFilename(rawurl, resp)
	if err != nil {
		utils.Debug("Error determining filename: %v", err)
		name = "download.bin"
	}

	result.DetectedFilename = name

	if filenameHint != "" {
		result.Filename = filenameHint
	} else {
		result.Filename = name
	}

	result.ContentType = resp.Header.Get("Content-Type")

	utils.Debug("Probe complete - filename: %s, size: %d, range: %v",
		result.Filename, result.FileSize, result.SupportsRange)

	return result, nil
}

func newProbeRequest(ctx context.Context, rawurl string, headers map[string]string, runCfg *types.RuntimeConfig, includeRange bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	applyProbeHeaders(req, headers, runCfg, includeRange)
	return req, nil
}

func applyProbeHeaders(req *http.Request, headers map[string]string, runCfg *types.RuntimeConfig, includeRange bool) {
	if req == nil {
		return
	}

	for key, val := range headers {
		if strings.EqualFold(key, "Range") {
			continue
		}
		req.Header.Set(key, val)
	}

	if includeRange {
		req.Header.Set("Range", "bytes=0-0")
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", runCfg.GetUserAgent())
	}
}

// ProbeMirrors is the convenience wrapper for callers that need mirror probing
// with an explicit runtime config snapshot.
func ProbeMirrors(ctx context.Context, mirrors []string) (valid []string, errs map[string]error) {
	return ProbeMirrorsWithProxy(ctx, mirrors, nil)
}

// ProbeMirrorsWithProxy preserves caller order so mirror priority remains stable.
func ProbeMirrorsWithProxy(ctx context.Context, mirrors []string, runCfg *types.RuntimeConfig) (valid []string, errs map[string]error) {
	candidates := orderedUniqueMirrors(mirrors)
	utils.Debug("Probing %d mirrors...", len(candidates))

	valid = make([]string, 0, len(candidates))
	errs = make(map[string]error)

	type mirrorProbeResult struct {
		valid bool
		err   error
	}

	results := make([]mirrorProbeResult, len(candidates))
	var wg sync.WaitGroup

	for i, url := range candidates {
		wg.Add(1)
		go func(idx int, target string) {
			defer wg.Done()

			// Mirror checks stay short so a dead backup does not delay the primary
			// download from starting with the best candidates we can confirm quickly.
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			result, err := ProbeServerWithProxy(probeCtx, target, "", nil, runCfg)

			outcome := mirrorProbeResult{}
			if err != nil {
				outcome.err = err
			} else {
				outcome.valid = result.SupportsRange
				if !result.SupportsRange {
					outcome.err = fmt.Errorf("does not support ranges")
				}
			}
			results[idx] = outcome
		}(i, url)
	}

	wg.Wait()

	for i, target := range candidates {
		if results[i].valid {
			valid = append(valid, target)
			continue
		}
		if results[i].err != nil {
			errs[target] = results[i].err
		}
	}

	utils.Debug("Mirror probing complete: %d valid, %d failed", len(valid), len(errs))
	return valid, errs
}

func orderedUniqueMirrors(mirrors []string) []string {
	seen := make(map[string]struct{}, len(mirrors))
	ordered := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		if _, ok := seen[mirror]; ok {
			continue
		}
		seen[mirror] = struct{}{}
		ordered = append(ordered, mirror)
	}
	return ordered
}
