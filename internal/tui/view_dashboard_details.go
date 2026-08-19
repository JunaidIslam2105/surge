package tui

import (
	"fmt"
	"time"
)

// detailsPaneRenderCache memoizes the focused details content. The elapsed time
// displayed by the pane has one-second precision, so the current second is part
// of the key; progress and mirror state are included to keep the cache correct
// between progress events as well.
type detailsPaneRenderCache struct {
	width       int
	second      int64
	spinner     string
	selected    string
	fingerprint string
	content     string
}

func (m *RootModel) renderDetailsContentCached(d *DownloadModel, width int, spinnerView string) string {
	if d == nil {
		return ""
	}
	if m.detailsPaneCache == nil {
		m.detailsPaneCache = &detailsPaneRenderCache{}
	}

	fingerprint := detailsFingerprint(d)
	second := time.Now().Unix()
	cache := m.detailsPaneCache
	if cache.content != "" && cache.width == width && cache.second == second &&
		cache.spinner == spinnerView && cache.selected == d.ID && cache.fingerprint == fingerprint {
		return cache.content
	}

	content := renderFocusedDetails(d, width, spinnerView)
	cache.width = width
	cache.second = second
	cache.spinner = spinnerView
	cache.selected = d.ID
	cache.fingerprint = fingerprint
	cache.content = content
	return content
}

func detailsFingerprint(d *DownloadModel) string {
	mirrorState := ""
	if d.state != nil {
		for _, mirror := range d.state.GetMirrors() {
			mirrorState += fmt.Sprintf("%t:%t;", mirror.Active, mirror.Error)
		}
	}

	errText := ""
	if d.err != nil {
		errText = d.err.Error()
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d|%d|%.6f|%d|%d|%t|%t|%t|%t|%t|%t|%t|%d|%d|%s|%s|%d|%t|%s",
		d.ID, d.URL, d.Filename, d.Destination, d.Total, d.Downloaded,
		d.Speed, d.Connections, d.RateLimit, d.RateLimitSet, d.done, d.started,
		d.paused, d.pausing, d.resuming, d.rateLimited, d.Elapsed, d.StartTime.UnixNano(),
		errText, mirrorState, d.lastETA, d.hasEtaSpeed, d.FilenameLower)
}
