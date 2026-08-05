package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/SurgeDM/Surge/internal/tui/colors"
)

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestFormatDurationForUI(t *testing.T) {
	tests := []struct {
		name string
		dur  time.Duration
		want string
	}{
		{"zero", 0, "0:00"},
		{"negative", -5 * time.Second, "0:00"},
		{"30 seconds", 30 * time.Second, "0:30"},
		{"1 minute", 60 * time.Second, "1:00"},
		{"5m30s", 5*time.Minute + 30*time.Second, "5:30"},
		{"59m59s", 59*time.Minute + 59*time.Second, "59:59"},
		{"1 hour", time.Hour, "1:00:00"},
		{"1h2m3s", time.Hour + 2*time.Minute + 3*time.Second, "1:02:03"},
		{"23h59m59s", 23*time.Hour + 59*time.Minute + 59*time.Second, "23:59:59"},
		{"1 day", 24 * time.Hour, "1d"},
		{"1d 5h", 29 * time.Hour, "1d 5h"},
		{"30+ days", 31 * 24 * time.Hour, "\u221E"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDurationForUI(tt.dur)
			if got != tt.want {
				t.Errorf("formatDurationForUI(%v) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}

func TestRenderHeaderBox_LocalModeHidesServerAddressAndStatusDot(t *testing.T) {
	InitializeTUI()

	m := RootModel{
		ServerPort: 0,
		ServerHost: "127.0.0.1",
		IsRemote:   false,
	}

	plain := ansiEscapeRE.ReplaceAllString(m.renderHeaderBox(60, 8), "")
	if !strings.Contains(plain, "Local mode") {
		t.Fatalf("expected Local mode banner, got %q", plain)
	}
	if strings.Contains(plain, "127.0.0.1:0") {
		t.Fatalf("unexpected server address in local no-server mode: %q", plain)
	}
	if strings.Contains(plain, "●") {
		t.Fatalf("unexpected status dot in local no-server mode: %q", plain)
	}
}

func TestRenderHeaderBox_CompactLocalModeStaysEmptyWhenServerDisabled(t *testing.T) {
	InitializeTUI()

	m := RootModel{
		ServerPort: 0,
		IsRemote:   false,
	}

	plain := ansiEscapeRE.ReplaceAllString(m.renderHeaderBox(24, 6), "")
	if strings.Contains(plain, "127.0.0.1:0") {
		t.Fatalf("unexpected compact server address in no-server mode: %q", plain)
	}
	if strings.Contains(plain, "●") {
		t.Fatalf("unexpected compact status dot in no-server mode: %q", plain)
	}
}

func TestGetDownloadStatus(t *testing.T) {
	spinnerView := "⠋"

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
			expected: "⠋ Pausing...",
		},
		{
			name: "Resuming State",
			model: &DownloadModel{
				resuming: true,
			},
			expected: "⠋ Resuming...",
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
			expected: "⠋ Queued",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := getDownloadStatus(tt.model, spinnerView)
			plainStatus := ansiEscapeRE.ReplaceAllString(status, "")
			if !strings.Contains(plainStatus, tt.expected) {
				t.Errorf("getDownloadStatus() = %q, want it to contain %q", plainStatus, tt.expected)
			}
		})
	}
}

func TestView_DashboardFitsViewportWithoutTopCutoff(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)

	cases := []struct {
		width  int
		height int
	}{
		{120, 35},
		{100, 30},
		{80, 24},
	}

	for _, tc := range cases {
		m.width = tc.width
		m.height = tc.height

		view := m.View()
		if strings.HasPrefix(view.Content, "\n") {
			t.Fatalf("view starts with a blank line at %dx%d", tc.width, tc.height)
		}

		plain := ansiEscapeRE.ReplaceAllString(view.Content, "")
		trimmed := strings.TrimRight(plain, "\n")
		lines := strings.Split(trimmed, "\n")

		if len(lines) > tc.height {
			t.Fatalf("view exceeds viewport height at %dx%d: got %d lines", tc.width, tc.height, len(lines))
		}

		if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
			t.Fatalf("top line is empty at %dx%d (possible top cutoff)", tc.width, tc.height)
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			t.Fatalf("bottom line is empty at %dx%d (footer likely clipped)", tc.width, tc.height)
		}
	}
}

func TestView_QuitConfirmContainsButtons(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = QuitConfirmState
	m.width = 120
	m.height = 35

	plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(plain, "Yep!") {
		t.Fatal("expected Yep! button in quit confirm view")
	}
	if !strings.Contains(plain, "Nope") {
		t.Fatal("expected Nope button in quit confirm view")
	}
	if !strings.Contains(plain, "Are you sure you want to quit?") {
		t.Fatal("expected confirmation message in quit confirm view")
	}
}

func TestView_QuitConfirmShowsActiveDownloadDetail(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = QuitConfirmState
	m.width = 120
	m.height = 35
	m.downloads = []*DownloadModel{
		{Speed: 1.0}, // active download
	}

	plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(plain, "1 active download(s) will be paused") {
		t.Fatalf("expected active download detail, got:\n%s", plain)
	}
}

func TestView_QuitConfirmNoFocusedRendersCorrectly(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = QuitConfirmState
	m.quitConfirmFocused = 1
	m.width = 120
	m.height = 35

	plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(plain, "Nope") {
		t.Fatal("expected Nope button present when No is focused")
	}
}

func TestView_QuitConfirmTinyTerminalDoesNotPanic(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = QuitConfirmState
	m.width = 10
	m.height = 5
	_ = m.View()
}

func TestView_RemoveConfirmTruncatesUTF8FilenameSafely(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = RemoveConfirmState
	m.removeTargetID = "id-1"
	m.width = 120
	m.height = 35
	m.downloads = []*DownloadModel{
		{ID: "id-1", Filename: "日本語日本語日本語日本語日本語日本語.zip", paused: true},
	}

	plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
	if strings.Contains(plain, "\uFFFD") {
		t.Fatalf("expected UTF-8 filename truncation without replacement characters, got:\n%s", plain)
	}
	if !strings.Contains(plain, "File: 日本語") {
		t.Fatalf("expected filename prefix in remove confirmation, got:\n%s", plain)
	}
	if !strings.Contains(plain, "\u2026") {
		t.Fatalf("expected truncated filename ellipsis, got:\n%s", plain)
	}
}

func TestView_SettingsTinyTerminalDoesNotPanic(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = SettingsState
	m.width = 20
	m.height = 8

	view := m.View()
	if strings.TrimSpace(ansiEscapeRE.ReplaceAllString(view.Content, "")) == "" {
		t.Fatal("expected non-empty settings view for tiny terminal")
	}
}

func TestView_SettingsNoLineExceedsTerminalWidth(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = SettingsState

	sizes := []struct{ width, height int }{
		{120, 35},
		{96, 24},
		{72, 18},
		{60, 16},
		{50, 14},
	}

	for _, tc := range sizes {
		m.width = tc.width
		m.height = tc.height

		for i, line := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(line) > tc.width {
				t.Fatalf("settings line %d exceeds width at %dx%d: got width %d", i, tc.width, tc.height, lipgloss.Width(line))
			}
		}
	}
}

func TestView_SettingsResizeSequenceKeepsSelectedVisible(t *testing.T) {
	metadata := config.GetSettingsMetadata()["General"]
	if len(metadata) == 0 {
		t.Fatal("expected General settings metadata")
	}

	selectedRow := len(metadata) - 1
	selectedLabel := metadata[selectedRow].Label

	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = SettingsState
	m.SettingsActiveTab = 0
	m.SettingsSelectedRow = selectedRow

	sequence := []struct{ width, height int }{
		{120, 35},
		{76, 18},
		{80, 24},
		{100, 30},
	}

	for _, tc := range sequence {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		m = updated.(RootModel)
		m.state = SettingsState

		plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
		if strings.TrimSpace(plain) == "" {
			t.Fatalf("empty settings view after resize to %dx%d", tc.width, tc.height)
		}
		if !strings.Contains(plain, selectedLabel) {
			t.Fatalf("selected setting label %q not visible after resize to %dx%d", selectedLabel, tc.width, tc.height)
		}
	}
}

func TestView_SettingsEditModeNarrowWidthNoOverflow(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.state = SettingsState
	m.width = 55
	m.height = 16
	m.SettingsActiveTab = 0
	m.SettingsSelectedRow = 0 // default_download_dir
	m.SettingsIsEditing = true
	m.SettingsInput.SetValue(strings.Repeat("x", 180))
	m.updateSettingsInputWidthForViewport()

	for i, line := range strings.Split(m.View().Content, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("settings edit line %d exceeds width at %dx%d: got width %d", i, m.width, m.height, lipgloss.Width(line))
		}
	}
}

func TestView_NetworkActivityShowsFiveAxisLabelsWhenTall(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 140
	m.height = 40

	view := m.View()
	plain := ansiEscapeRE.ReplaceAllString(view.Content, "")

	if !strings.Contains(plain, "781 KiB/s") || !strings.Contains(plain, "195 KiB/s") {
		t.Fatalf("expected 5-axis labels (including 781 KiB/s and 195 KiB/s), got:\n%s", plain)
	}
}

func BenchmarkLogoGradient(b *testing.B) {
	logoText := `
   _______  ___________ ____ 
  / ___/ / / / ___/ __ '/ _ \
 (__  ) /_/ / /  / /_/ /  __/
/____/\__,_/_/   \__, /\___/ 
                /____/       `

	startColor := colors.Pink()
	endColor := colors.Magenta()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ApplyGradient(logoText, startColor, endColor)
	}
}

func BenchmarkCachedLogo(b *testing.B) {
	logoText := `
   _______  ___________ ____
  / ___/ / / / ___/ __ '/ _ \\
 (__  ) /_/ / /  / /_/ /  __/
/____/\__,_/_/   \__, /\___/
                /____/       `

	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	// Pre-warm cache
	gradientLogo := ApplyGradient(logoText, colors.Pink(), colors.Magenta())
	m.logoCache = lipgloss.NewStyle().Render(gradientLogo)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if m.logoCache != "" {
			_ = m.logoCache
		} else {
			_ = ApplyGradient(logoText, colors.Pink(), colors.Magenta())
		}
	}
}

// Tests for issue #252: TUI layout breakage on non-standard terminal sizes

func TestView_NoLineExceedsTerminalWidth(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)

	sizes := []struct{ width, height int }{
		{160, 40}, // full layout
		{120, 30}, // full layout lower bound
		{100, 24}, // compact: no stats box
		{80, 24},  // narrow: no stats box, no logo
		{60, 20},  // very narrow: right column hidden
	}

	for _, tc := range sizes {
		m.width = tc.width
		m.height = tc.height

		for i, line := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(line) > tc.width {
				t.Errorf("line %d exceeds width at %dx%d: got width %d, content: %q",
					i, tc.width, tc.height, lipgloss.Width(line), line[:min(len(line), 80)])
			}
		}
	}
}

func TestView_NoBoxCorruptionAtNarrowWidths(t *testing.T) {
	// Check for doubled box-drawing characters that indicate overlapping panes
	corruptionPatterns := []string{
		"╭╭", "╮╮", "╰╰", "╯╯", // doubled corners
	}

	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)

	sizes := []struct{ width, height int }{
		{160, 40},
		{120, 30},
		{100, 24},
		{80, 24},
		{60, 20},
	}

	for _, tc := range sizes {
		m.width = tc.width
		m.height = tc.height

		plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
		for _, pattern := range corruptionPatterns {
			if strings.Contains(plain, pattern) {
				t.Errorf("box corruption %q found at %dx%d", pattern, tc.width, tc.height)
			}
		}
	}
}

func TestView_RightColumnHiddenAtNarrowWidth(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 60
	m.height = 20

	view := m.View()
	plain := ansiEscapeRE.ReplaceAllString(view.Content, "")

	// Should NOT contain right column headers
	if strings.Contains(plain, "Network Activity") {
		t.Fatal("right column should be hidden at narrow width, but 'Network Activity' found")
	}
	if strings.Contains(plain, "File Details") {
		t.Fatal("right column should be hidden at narrow width, but 'File Details' found")
	}
}

func TestView_LogoHiddenAtNarrowWidth(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 60
	m.height = 24

	view := m.View()
	plain := ansiEscapeRE.ReplaceAllString(view.Content, "")

	// ASCII logo starts with underscore-like characters that form the logo shape
	// At narrow widths, the large logo should be hidden
	if strings.Contains(plain, "_______") {
		t.Fatal("logo should be hidden when leftWidth < 60, but found underscores")
	}
}

func TestView_FooterHidesHelpTextAtNarrowWidth(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 40
	m.height = 24

	view := m.View()
	plain := ansiEscapeRE.ReplaceAllString(view.Content, "")
	lines := strings.Split(plain, "\n")

	// Last line should be version-only, no help text
	lastLine := lines[len(lines)-1]
	// Help text contains specific key bindings like "enter", "tab", etc.
	if strings.Contains(lastLine, "enter") || strings.Contains(lastLine, "tab") ||
		strings.Contains(lastLine, "del") || strings.Contains(lastLine, "down") {
		t.Fatalf("footer should hide help text at narrow width, got: %q", lastLine)
	}
}

func TestView_TerminalTooSmallMessage(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)

	sizes := []struct{ width, height int }{
		{40, 10},
		{30, 20},
		{44, 11},
	}

	for _, tc := range sizes {
		m.width = tc.width
		m.height = tc.height

		plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
		if !strings.Contains(plain, "Terminal too small") {
			t.Errorf("expected 'Terminal too small' at %dx%d, got:\n%s", tc.width, tc.height, plain)
		}
	}
}

func TestHelpModal_RendersAndClosesOnEsc(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 40
	m.state = HelpModalState

	// Should render without panic
	view := m.View()
	output := ansiEscapeRE.ReplaceAllString(view.Content, "")
	if !strings.Contains(output, "Keyboard Shortcuts") {
		t.Error("help modal should contain 'Keyboard Shortcuts' title")
	}

	// Esc should transition back to DashboardState
	newModel, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updated := newModel.(RootModel)
	if updated.state != DashboardState {
		t.Errorf("expected DashboardState after esc, got %d", updated.state)
	}
}

func TestView_DoesNotPanicAtExtremeSizes(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)

	extremeSizes := []struct{ width, height int }{
		{40, 12},  // very narrow and short
		{40, 80},  // narrow and tall
		{200, 15}, // wide but extremely short
		{1, 1},    // minimum possible
		{0, 24},   // zero width (loading)
	}

	for _, tc := range extremeSizes {
		m.width = tc.width
		m.height = tc.height
		// Should not panic
		_ = m.View()
	}
}

// --- Footer status bar tests ---

// footerLine extracts the plain-text last line of the dashboard view.
func footerLine(m RootModel) string {
	plain := ansiEscapeRE.ReplaceAllString(m.View().Content, "")
	trimmed := strings.TrimRight(plain, "\r\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimRight(lines[len(lines)-1], "\r")
}

func TestFooter_GlyphsAlwaysPresent(t *testing.T) {
	InitializeTUI()
	m := InitialRootModel(1701, "1.2.3", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35

	last := footerLine(m)
	for _, glyph := range []string{"\u2B07", "\u26A1", "\u23fb"} {
		if !strings.Contains(last, glyph) {
			t.Errorf("footer missing glyph %q, got: %q", glyph, last)
		}
	}
}

func TestFooter_VersionShown(t *testing.T) {
	InitializeTUI()
	m := InitialRootModel(1701, "9.8.7", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35

	last := footerLine(m)
	if !strings.Contains(last, "v9.8.7") {
		t.Errorf("footer should contain version string v9.8.7, got: %q", last)
	}
}

func TestFooter_IdleSpeedShowsZero(t *testing.T) {
	InitializeTUI()
	// No active downloads \u2192 speed should render as "0 B/s"
	m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35

	last := footerLine(m)
	if !strings.Contains(last, "0 B/s") {
		t.Errorf("footer should show '0 B/s' when idle, got: %q", last)
	}
}

func TestFooter_ActiveSpeedShowsMBps(t *testing.T) {
	InitializeTUI()
	m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35
	// Inject an active download at 2.5 MB/s (in bytes/s)
	m.downloads = []*DownloadModel{
		{Speed: 2.5 * 1024 * 1024},
	}
	m.cachedTotalSpeed = int64(2.5 * 1024 * 1024)

	last := footerLine(m)
	if !strings.Contains(last, "MiB/s") {
		t.Errorf("footer should show MiB/s for active download, got: %q", last)
	}
	if !strings.Contains(last, "2.5") {
		t.Errorf("footer should show 2.5 MiB/s, got: %q", last)
	}
}

func TestFooter_ActiveSpeedShowsKBps(t *testing.T) {
	InitializeTUI()
	m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35
	// 512 KB/s = 0.5 MB/s \u2192 should render as KB/s
	m.downloads = []*DownloadModel{
		{Speed: 512 * 1024},
	}
	m.cachedTotalSpeed = 512 * 1024

	last := footerLine(m)
	if !strings.Contains(last, "KiB/s") {
		t.Errorf("footer should show KiB/s for sub-MiB/s speed, got: %q", last)
	}
}

func TestFooter_ZeroRateLimitShowsInfinity(t *testing.T) {
	InitializeTUI()
	m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35
	// Default settings have no global rate limit \u2192 \u221E
	m.Settings = config.DefaultSettings()

	last := footerLine(m)
	if !strings.Contains(last, "\u221E") {
		t.Errorf("footer should show \u221E when rate limit is 0, got: %q", last)
	}
}

func TestFooter_GlobalRateLimitShown(t *testing.T) {
	InitializeTUI()
	m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 120
	m.height = 35

	settings := config.DefaultSettings()
	settings.Network.GlobalRateLimit.Value = "2mb" // 2 MB/s limit
	m.Settings = settings

	last := footerLine(m)
	// FormatRateLimit(2_000_000) \u2192 "2.0 MB/s" or similar; just check the unit appears
	if !strings.Contains(last, "/s") {
		t.Errorf("footer should show rate limit with /s unit, got: %q", last)
	}
	// Not 0 when active limit is set
}

func TestFooter_HidesHelpAtNarrowWidth(t *testing.T) {
	InitializeTUI()
	// width=55: above MinTermWidth(45) so the real dashboard renders, but below
	// the 60-char threshold where we drop help text from the footer.
	m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
	m.width = 55
	m.height = 24

	last := footerLine(m)
	// Speed/limit/version glyphs must still appear
	for _, glyph := range []string{"\u2B07", "\u26A1", "\u23fb"} {
		if !strings.Contains(last, glyph) {
			t.Errorf("narrow footer missing glyph %q, got: %q", glyph, last)
		}
	}
	// Help key text must be absent at this width
	if strings.Contains(last, "enter") || strings.Contains(last, "tab") || strings.Contains(last, "del") {
		t.Errorf("footer should hide help text at narrow width, got: %q", last)
	}
}

func TestFooter_NoLineOverflowAtVariousSizes(t *testing.T) {
	InitializeTUI()
	sizes := []struct{ width, height int }{
		{160, 40},
		{120, 35},
		{100, 24},
		{80, 24},
		{60, 20},
	}
	for _, tc := range sizes {
		m := InitialRootModel(1701, "1.0.0", nil, orchestrator.NewLifecycleManager(nil, nil, nil), nil, false)
		m.width = tc.width
		m.height = tc.height
		for i, line := range strings.Split(m.View().Content, "\n") {
			if w := lipgloss.Width(line); w > tc.width {
				t.Errorf("line %d overflows at %dx%d: width=%d", i, tc.width, tc.height, w)
			}
		}
	}
}
