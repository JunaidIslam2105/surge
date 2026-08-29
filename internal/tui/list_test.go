package tui

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
)

var testAnsiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestNewDownloadListDisablesBuiltInFiltering(t *testing.T) {
	if NewDownloadList(80, 20).FilteringEnabled() {
		t.Fatal("download list filtering must remain disabled; dashboard search owns filtering")
	}
}

func TestDownloadItem_Description(t *testing.T) {
	spinnerView := "\u280b"

	tests := []struct {
		name     string
		model    *DownloadModel
		expected string
	}{
		{
			name: "Pausing State",
			model: &DownloadModel{
				pausing: true,
			},
			expected: "\u280b Pausing...",
		},
		{
			name: "Resuming State",
			model: &DownloadModel{
				resuming: true,
			},
			expected: "\u280b Resuming...",
		},
		{
			name: "Queued State",
			model: &DownloadModel{
				Speed:      0,
				Downloaded: 0,
				done:       false,
				paused:     false,
				err:        nil,
			},
			expected: "\u280b Queued",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := DownloadItem{
				download:    tt.model,
				spinnerView: spinnerView,
			}
			desc := item.Description()
			plainDesc := testAnsiEscapeRE.ReplaceAllString(desc, "")
			if !strings.Contains(plainDesc, tt.expected) {
				t.Errorf("Description() = %q, want it to contain %q", plainDesc, tt.expected)
			}
		})
	}
}

func TestDownloadDelegateReservesRoomForCategory(t *testing.T) {
	d := newDownloadDelegate()
	m := list.New([]list.Item{}, d, 42, 10)
	item := DownloadItem{
		download: &DownloadModel{Filename: strings.Repeat("very-long-name-", 8) + ".mkv"},
		category: "Shows",
	}
	var buf bytes.Buffer
	d.Render(&buf, m, 0, item)
	if got := testAnsiEscapeRE.ReplaceAllString(buf.String(), ""); !strings.Contains(got, "[Shows]") {
		t.Fatalf("long-title row omitted category: %q", got)
	}
}

func BenchmarkDownloadDelegateRender(b *testing.B) {
	d := newDownloadDelegate()
	m := list.New([]list.Item{}, d, 100, 100)

	// mock download logic
	di := DownloadItem{
		download: &DownloadModel{
			ID:         "123",
			Filename:   "ubuntu-22.04.iso",
			Total:      1024 * 1024 * 1000,
			Downloaded: 1024 * 1024 * 500,
			Speed:      10 * 1024 * 1024,
		},
	}

	var buf bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		d.Render(&buf, m, 0, di)
	}
}
