package tui

import (
	"fmt"
	"time"
)

// detailsPaneRenderKey identifies focused details content. The elapsed time
// displayed by the pane has one-second precision, so the current second is part
// of the key; progress and mirror state are included to keep the cache correct
// between progress events as well.
type detailsPaneRenderKey struct {
	width       int
	second      int64
	spinner     string
	selected    string
	fingerprint string
}

func (m *RootModel) renderDetailsContentCached(d *DownloadModel, width int, spinnerView string) string {
	if d == nil {
		return ""
	}
	if m.detailsPaneCache == nil {
		m.detailsPaneCache = &renderCache[detailsPaneRenderKey]{}
	}

	key := detailsPaneRenderKey{
		width: width, second: time.Now().Unix(), spinner: spinnerView,
		selected: d.ID, fingerprint: detailsFingerprint(d),
	}
	if render, ok := m.detailsPaneCache.Get(key); ok {
		return render
	}

	content := renderFocusedDetails(d, width, spinnerView)
	return m.detailsPaneCache.Set(key, content)
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
