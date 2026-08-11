package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/utils"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Viewport layout
const maxUIDuration = 30 * 24 * time.Hour

// formatDurationForUI formats a duration as a human-readable clock string.
// Returns "M:SS" for sub-hour durations, "H:MM:SS" for multi-hour, "Xd Yh" for days.
func formatDurationForUI(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= maxUIDuration {
		return "\u221e"
	}

	totalSec := int(d.Seconds())

	if totalSec >= 86400 {
		days := totalSec / 86400
		hours := (totalSec % 86400) / 3600
		if hours > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}

	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	secs := totalSec % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// renderModalWithOverlay renders a modal centered on screen with a dark overlay effect
func (m RootModel) renderModalWithOverlay(modal string) string {
	// Place modal centered with dark gray background fill for overlay effect
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(" "), // Changed from "░" to avoid terminal rendering glitches
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("236"))),
	)
}

func (m RootModel) wrapView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m RootModel) View() tea.View {
	if m.width == 0 {
		return m.wrapView("Loading...")
	}

	// Terminal too small to render any meaningful layout
	if m.width < MinTermWidth || m.height < MinTermHeight {
		msg := lipgloss.NewStyle().Foreground(colors.Cyan()).Render(fmt.Sprintf("Terminal too small (min: %d\u00D7%d)", MinTermWidth, MinTermHeight))
		return m.wrapView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg))
	}

	if m.shuttingDown {
		modal := components.ConfirmationModal{
			Title:       "Shutting Down",
			Message:     "Pausing downloads and saving resume state...",
			Detail:      "Please wait",
			Keys:        components.NoKeys{},
			Help:        m.help,
			BorderColor: colors.Cyan(),
		}
		modal.Width, modal.Height = GetDynamicModalDimensions(m.width, m.height, 40, 6, 60, 10)
		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	// === Handle Modal States First ===
	// These overlays sit on top of the dashboard or replace it

	if m.state == InputState {
		var activeInputs []textinput.Model
		var activeLabels []string

		activeInputs = append(activeInputs, m.inputs[0])
		activeLabels = append(activeLabels, "URL:")

		if !m.hideMirrors {
			activeInputs = append(activeInputs, m.inputs[1])
			activeLabels = append(activeLabels, "Mirrors:")
		}

		activeInputs = append(activeInputs, m.inputs[2], m.inputs[3])
		activeLabels = append(activeLabels, "Path:", "Filename:")

		mappedFocus := m.focusedInput
		if m.hideMirrors && m.focusedInput > 0 {
			mappedFocus = m.focusedInput - 1
		}

		browseHint := 2
		if m.hideMirrors {
			browseHint = 1
		}

		modal := components.AddDownloadModal{
			Title:           "Add Download",
			Inputs:          activeInputs,
			Labels:          activeLabels,
			FocusedInput:    mappedFocus,
			BrowseHintIndex: browseHint,
			BrowseHintKey:   m.keys.Input.Tab.Help().Key,
			Help:            m.help,
			HelpKeys:        m.keys.Input,
			BorderColor:     colors.Pink(),
		}
		// Resolve dynamic dimensions
		w, _ := GetDynamicModalDimensions(m.width, m.height, 46, 8, 80, 0)
		modal.Width = w
		h := lipgloss.Height(modal.View()) + BoxStyle.GetVerticalFrameSize()
		_, modal.Height = GetDynamicModalDimensions(m.width, m.height, 46, 8, w, h)

		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == FilePickerState {
		// Create a local copy to avoid modifying model during view (though View takes value receiver m)
		fp := m.filepicker
		picker := components.NewFilePickerModal(
			" Select Directory ",
			&fp,
			m.help,
			m.keys.FilePicker,
			colors.Pink(),
		)
		// Resolve dynamic dimensions
		w, h := GetDynamicModalDimensions(m.width, m.height, 60, 10, 90, 20)
		picker.Width = w
		picker.Height = h

		box := picker.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == SettingsState {
		return m.wrapView(m.viewSettings())
	}

	if m.state == SpeedLimitsState {
		return m.wrapView(m.renderModalWithOverlay(m.viewSpeedLimits()))
	}

	if m.state == DuplicateWarningState {
		modal := components.ConfirmationModal{
			Title:       "\u26a0 Duplicate Detected",
			Message:     "A download with this URL already exists",
			Detail:      m.duplicateInfo,
			Keys:        m.keys.Duplicate,
			Help:        m.help,
			BorderColor: colors.Pink(),
		}
		// Resolve dynamic dimensions
		w, _ := GetDynamicModalDimensions(m.width, m.height, 40, 6, 60, 0)
		modal.Width = w
		// ConfirmationModal's internal height calculation depends on width (for help wrap)
		// but since it's a fixed-width confirmation message, we can approximate or call View()
		// Note: ConfirmationModal renders itself into the height passed,
		// so we need a reasonable estimate for 'h'.
		h := 10 // typical height for confirmation
		if m.duplicateInfo != "" {
			h = 11
		}
		_, modal.Height = GetDynamicModalDimensions(m.width, m.height, 40, 6, w, h)

		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == ExtensionConfirmationState {
		extInputs := []textinput.Model{m.inputs[2], m.inputs[3]}
		focused := 0
		if m.focusedInput == 3 {
			focused = 1
		}
		modal := components.AddDownloadModal{
			Title:           "Extension Download",
			Inputs:          extInputs,
			Labels:          []string{"Path:", "Filename:"},
			FocusedInput:    focused,
			ShowURL:         true,
			URL:             m.pendingURL,
			BrowseHintIndex: 0,
			BrowseHintKey:   m.keys.Extension.Browse.Help().Key,
			Help:            m.help,
			HelpKeys:        m.keys.Extension,
			BorderColor:     colors.Cyan(),
		}
		// Resolve dynamic dimensions
		w, _ := GetDynamicModalDimensions(m.width, m.height, 60, 10, 86, 0)
		modal.Width = w
		h := lipgloss.Height(modal.View()) + BoxStyle.GetVerticalFrameSize()
		_, modal.Height = GetDynamicModalDimensions(m.width, m.height, 60, 10, w, h)

		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == BatchFilePickerState {
		fp := m.filepicker
		picker := components.NewFilePickerModal(
			" Select URL File (.txt) ",
			&fp,
			m.help,
			m.keys.FilePicker,
			colors.Cyan(),
		)
		// Resolve dynamic dimensions
		w, h := GetDynamicModalDimensions(m.width, m.height, 60, 10, 90, 20)
		picker.Width = w
		picker.Height = h

		box := picker.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == BatchConfirmState {
		urlCount := len(m.pendingBatchURLs)
		if len(m.pendingBatchRequests) > 0 {
			urlCount = len(m.pendingBatchRequests)
		}
		batchDetail := fmt.Sprintf("Path: %s", m.inputs[2].View())
		if m.batchFilePath != "" && m.batchFilePath != strings.TrimSpace(m.inputs[2].Value()) {
			batchDetail = fmt.Sprintf("Source: %s\nPath: %s", m.batchFilePath, m.inputs[2].View())
		}
		modal := components.ConfirmationModal{
			Title:       "Batch Import",
			Message:     fmt.Sprintf("Add %d downloads?", urlCount),
			Detail:      batchDetail,
			Keys:        m.keys.BatchConfirm,
			Help:        m.help,
			BorderColor: colors.Cyan(),
		}
		// Resolve dynamic dimensions
		w, _ := GetDynamicModalDimensions(m.width, m.height, 40, 6, 60, 0)
		modal.Width = w
		h := 10 // typical height for confirmation
		_, modal.Height = GetDynamicModalDimensions(m.width, m.height, 40, 6, w, h)

		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == BugReportTargetState {
		w, h := GetDynamicModalDimensions(m.width, m.height, 40, 8, 64, 12)
		modal := components.ConfirmationModal{
			Title:            "Bug Report",
			Message:          "What would you like to report?",
			Detail:           "Surge Core includes CLI/TUI/server components.",
			Keys:             m.keys.BugReport,
			Help:             m.help,
			BorderColor:      colors.Cyan(),
			Width:            w,
			Height:           h,
			ShowYesNoButtons: true,
			YesNoFocused:     m.quitConfirmFocused,
			YesLabel:         "Surge Core",
			NoLabel:          "Extension",
		}
		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == BugReportSystemDetailsState {
		w, h := GetDynamicModalDimensions(m.width, m.height, 40, 8, 66, 12)
		modal := components.ConfirmationModal{
			Title:            "Core Bug Report",
			Message:          "Include system details in issue body?",
			Detail:           "(OS, version, commit)",
			Keys:             m.keys.QuitConfirm,
			Help:             m.help,
			BorderColor:      colors.Cyan(),
			Width:            w,
			Height:           h,
			ShowYesNoButtons: true,
			YesNoFocused:     m.quitConfirmFocused,
		}
		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == BugReportLogPathState {
		w, h := GetDynamicModalDimensions(m.width, m.height, 40, 8, 72, 12)
		modal := components.ConfirmationModal{
			Title:            "Core Bug Report",
			Message:          "Include latest debug log path in issue body?",
			Detail:           "Choose yes to prefill the latest path when available.",
			Keys:             m.keys.QuitConfirm,
			Help:             m.help,
			BorderColor:      colors.Cyan(),
			Width:            w,
			Height:           h,
			ShowYesNoButtons: true,
			YesNoFocused:     m.quitConfirmFocused,
		}
		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == QuitConfirmState {
		return m.wrapView(m.renderModalWithOverlay(m.viewQuitConfirm()))
	}

	if m.state == RestartConfirmState {
		return m.wrapView(m.renderModalWithOverlay(m.viewRestartConfirm()))
	}

	if m.state == PurgeConfirmState {
		return m.wrapView(m.renderModalWithOverlay(m.viewPurgeConfirm()))
	}

	if m.state == RemoveConfirmState {
		return m.wrapView(m.renderModalWithOverlay(m.viewRemoveConfirm()))
	}

	if m.state == UpdateAvailableState && m.UpdateInfo != nil {
		modal := components.ConfirmationModal{
			Title:       "\u2b06 Update Available",
			Message:     fmt.Sprintf("A new version of Surge is available: %s", m.UpdateInfo.LatestVersion),
			Detail:      fmt.Sprintf("Current: %s", m.UpdateInfo.CurrentVersion),
			Keys:        m.keys.Update,
			Help:        m.help,
			BorderColor: colors.Cyan(),
		}
		// Resolve dynamic dimensions
		w, _ := GetDynamicModalDimensions(m.width, m.height, 50, 8, 60, 0)
		modal.Width = w
		h := 12 // typical height for update prompt
		_, modal.Height = GetDynamicModalDimensions(m.width, m.height, 50, 8, w, h)

		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == URLUpdateState {
		modal := components.AddDownloadModal{
			Title:           "Refresh URL",
			Inputs:          []textinput.Model{m.urlUpdateInput},
			Labels:          []string{"New URL:"},
			FocusedInput:    0,
			BrowseHintIndex: -1, // No browse hint needed
			Help:            m.help,
			HelpKeys:        m.keys.Input,
			BorderColor:     colors.Pink(),
		}
		// Resolve dynamic dimensions
		w, _ := GetDynamicModalDimensions(m.width, m.height, 46, 6, 80, 0)
		modal.Width = w
		h := lipgloss.Height(modal.View()) + BoxStyle.GetVerticalFrameSize()
		_, modal.Height = GetDynamicModalDimensions(m.width, m.height, 46, 6, w, h)

		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	if m.state == HelpModalState {
		w, h := GetDynamicModalDimensions(m.width, m.height, 40, 10, PopupWidth, 22)
		modal := components.HelpModal{
			Title:       "Keyboard Shortcuts",
			HelpKeys:    m.keys.Dashboard,
			Help:        m.help,
			BorderColor: colors.Cyan(),
			Width:       w,
			Height:      h,
		}
		box := modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
		return m.wrapView(m.renderModalWithOverlay(box))
	}

	// === MAIN DASHBOARD LAYOUT ===
	layout := CalculateDashboardLayout(m.width, m.height)

	// Footer - keybindings on left, speed/limit/version on bottom-right
	helpText := lipgloss.NewStyle().PaddingLeft(2).Render(m.help.View(m.keys.Dashboard))

	// --- Right-side footer: speed │ limit │ version ---
	dimSep := lipgloss.NewStyle().Foreground(colors.Gray()).Render(" \u2502 ")

	// Global speed indicator
	speedBps := m.cachedTotalSpeed
	speedGlyph := lipgloss.NewStyle().Foreground(colors.Cyan()).Render("\u2B07")
	var speedVal string
	if speedBps <= 0 {
		speedVal = lipgloss.NewStyle().Foreground(colors.Gray()).Render("0 B/s")
	} else {
		speedVal = lipgloss.NewStyle().Foreground(colors.LightGray()).Render(utils.FormatSpeed(float64(speedBps)))
	}
	speedChunk := lipgloss.JoinHorizontal(lipgloss.Center, speedGlyph, " ", speedVal)

	// Global rate limit indicator
	limitGlyph := lipgloss.NewStyle().Foreground(colors.Pink()).Render("\u26A1")
	var limitVal string
	if m.Settings != nil && m.Settings.Network.GlobalRateLimit != nil {
		if rate, err := utils.ParseRateLimitValue(m.Settings.Network.GlobalRateLimit.Value); err == nil && rate > 0 {
			limitVal = lipgloss.NewStyle().Foreground(colors.LightGray()).Render(utils.FormatRateLimit(rate))
		}
	}
	var limitChunk string
	if limitVal != "" {
		limitChunk = lipgloss.JoinHorizontal(lipgloss.Center, limitGlyph, " ", limitVal)
	} else {
		limitChunk = lipgloss.JoinHorizontal(lipgloss.Center, limitGlyph, " ", lipgloss.NewStyle().Foreground(colors.Gray()).Render("\u221E"))
	}

	// Auto-shutdown indicator
	powerColor := colors.Gray()
	if m.isAutoShutdownEnabled() {
		powerColor = colors.Orange()
	}
	powerChunk := lipgloss.NewStyle().
		Foreground(powerColor).
		Width(3).
		Align(lipgloss.Center).
		Render("\u23fb")

	// Version indicator
	versionBlue := colors.ThemeColor("#005cc5", "#58a6ff")
	versionChunk := lipgloss.NewStyle().Foreground(versionBlue).Render(fmt.Sprintf("v%s", m.CurrentVersion))

	rightFooter := lipgloss.NewStyle().PaddingRight(2).Render(lipgloss.JoinHorizontal(lipgloss.Center,
		speedChunk,
		dimSep,
		limitChunk,
		dimSep,
		powerChunk,
		dimSep,
		versionChunk,
	))

	// Hide help text at very narrow widths - right footer is more important
	var footerContent string
	rightFooterWidth := lipgloss.Width(rightFooter)
	if layout.AvailableWidth < 60 {
		footerContent = rightFooter
	} else {
		leftFooterWidth := layout.AvailableWidth - rightFooterWidth
		if leftFooterWidth < 0 {
			leftFooterWidth = 0
		}
		footerContent = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(leftFooterWidth).Render(helpText),
			rightFooter,
		)
	}
	footer := footerContent

	// Pre-calculate data needed for sub-renders
	stats := m.ComputeViewStats()
	selected := m.GetSelectedDownload()

	var bitmap []byte
	var bitmapWidth int
	var totalSize, chunkSize int64
	var chunkProgress []int64
	if selected != nil && selected.state != nil {
		bitmap, bitmapWidth, totalSize, chunkSize, chunkProgress = selected.state.GetBitmap()
	}

	// Pre-compute details content to avoid double-computation and width mismatches
	var detailContent string
	detailWidth := layout.RightWidth
	if layout.HideRightColumn {
		detailWidth = layout.LeftWidth
	}
	if selected != nil {
		detailContent = renderFocusedDetails(selected, detailWidth-components.BorderFrameWidth, m.spinner.View())
	} else {
		detailContent = renderEmptyMessage(detailWidth-components.BorderFrameWidth, layout.DetailHeight-components.BorderFrameHeight, "No download selected")
	}

	// Render Components
	logoColumn := m.renderHeaderBox(layout.LogoWidth, layout.HeaderHeight)
	logBox := m.renderLogBox(layout.LogWidth, layout.HeaderHeight)
	headerBox := lipgloss.JoinHorizontal(lipgloss.Top, logoColumn, logBox)

	listBox := m.renderDownloadsBox(layout.LeftWidth, layout.ListHeight, stats)

	// Right column
	var rightColumn string
	if !layout.HideRightColumn {
		// Show chunk map only if we have actual data to visualize
		hasChunks := len(bitmap) > 0 && bitmapWidth > 0
		showActualChunkMap := layout.ShowChunkMap && hasChunks && selected != nil && !selected.done

		// Measure whether the detail content actually fits in the allocated
		// DetailHeight. If it doesn't, the chunk map would cause details to
		// be clipped - so give the chunk map's space back to details.
		if showActualChunkMap {
			detailInnerH := layout.DetailHeight - components.BorderFrameHeight
			if detailInnerH < 1 {
				detailInnerH = 1
			}
			contentH := lipgloss.Height(detailContent)
			if contentH > detailInnerH {
				// Detail content is taller than what's allocated; reclaim
				// chunk map space so nothing gets cut off.
				showActualChunkMap = false
			}
		}

		// If we reserved space for chunk map but aren't showing it, give it to details
		if !showActualChunkMap && layout.ShowChunkMap {
			layout.DetailHeight += layout.ChunkMapHeight
		}

		var graphBox string
		showGraph := layout.GraphHeight >= layout.MinGraphHeight
		if showGraph && m.Settings != nil {
			showGraph = config.Resolve[bool](m.Settings.General.ShowSpeedGraph)
		}
		if showGraph {
			graphBox = m.renderGraphBox(layout.RightWidth, layout.GraphHeight, stats)
		} else if layout.GraphHeight > 0 {
			layout.DetailHeight += layout.GraphHeight
		}
		detailBox := renderBtopBox("", PaneTitleStyle.Render(" File Details "), detailContent, layout.RightWidth, layout.DetailHeight, colors.Gray())

		var rightParts []string
		if showGraph {
			rightParts = append(rightParts, graphBox)
		}
		rightParts = append(rightParts, detailBox)

		if showActualChunkMap {
			chunkBox := m.renderChunkMapBox(layout.RightWidth, layout.ChunkMapHeight, selected, bitmap, bitmapWidth, totalSize, chunkSize, chunkProgress)
			rightParts = append(rightParts, chunkBox)
		}
		rightColumn = lipgloss.JoinVertical(lipgloss.Left, rightParts...)
	}

	// Assembly
	var body string
	if layout.HideRightColumn {
		if layout.VerticalLayout {
			detailBox := renderBtopBox("", PaneTitleStyle.Render(" File Details "), detailContent, layout.LeftWidth, layout.DetailHeight, colors.Gray())
			body = lipgloss.JoinVertical(lipgloss.Left, headerBox, listBox, detailBox)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left, headerBox, listBox)
		}
	} else {
		leftColumn := lipgloss.JoinVertical(lipgloss.Left, headerBox, listBox)
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, rightColumn)
	}

	body = lipgloss.NewStyle().
		Width(layout.AvailableWidth).
		Height(layout.AvailableHeight).
		MaxWidth(layout.AvailableWidth).
		MaxHeight(layout.AvailableHeight).
		Render(body)

	fullView := lipgloss.JoinVertical(lipgloss.Left, body, footer)
	// Place content into available space, then wrap with WindowStyle margins
	return m.wrapView(lipgloss.Place(layout.AvailableWidth, m.height, lipgloss.Center, lipgloss.Top, fullView))
}

// Helper to render the detailed info pane
func renderFocusedDetails(d *DownloadModel, w int, spinnerView string) string {
	pct := 0.0
	if d.Total > 0 {
		pct = float64(d.Downloaded) / float64(d.Total)
	}

	// Consistent content width for centering
	contentWidth := w - (components.BorderFrameWidth * 2)
	if contentWidth < 0 {
		contentWidth = 0
	}

	// Separator Style
	divider := lipgloss.NewStyle().
		Foreground(colors.Gray()).
		Width(contentWidth).
		Render("\n" + strings.Repeat("\u2500", contentWidth) + "\n")

	// Padding Style for sections
	sectionStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Padding(0, 1)

	// --- 1. Status Section ---
	statusStr := getDownloadStatus(d, spinnerView)
	statusStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colors.Gray()).
		Width(contentWidth).
		Align(lipgloss.Center)

	statusBox := statusStyle.Render(statusStr)

	// --- 2. File Information Section ---
	displayFilename := d.Filename
	if displayFilename == "" || displayFilename == "Queued" {
		displayFilename = d.URL
	}

	displayPath := d.Destination
	if displayPath == "" {
		displayPath = d.URL
	}

	// Calculate inner width accounting for sectionStyle padding (0, 1)
	innerWidth := contentWidth - components.BorderFrameWidth
	if innerWidth < 0 {
		innerWidth = 0
	}
	valueWidth := innerWidth - 12
	if valueWidth < 5 {
		valueWidth = 5 // Minimum reasonable width
	}

	fileInfoContent := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, StatsLabelStyle.Render("URL: "), StatsValueStyle.Width(valueWidth).MaxWidth(valueWidth).Render(utils.TruncateTwoLines(d.URL, valueWidth))),
		lipgloss.JoinHorizontal(lipgloss.Top, StatsLabelStyle.Render("File: "), StatsValueStyle.Width(valueWidth).MaxWidth(valueWidth).Render(utils.TruncateTwoLines(displayFilename, valueWidth))),
		lipgloss.JoinHorizontal(lipgloss.Top, StatsLabelStyle.Render("Path: "), StatsValueStyle.Width(valueWidth).MaxWidth(valueWidth).Render(utils.TruncateTwoLines(displayPath, valueWidth))),
		lipgloss.JoinHorizontal(lipgloss.Top, StatsLabelStyle.Render("ID:   "), lipgloss.NewStyle().Foreground(colors.LightGray()).Width(valueWidth).MaxWidth(valueWidth).Render(utils.WrapText(d.ID, valueWidth))),
	)
	fileSection := sectionStyle.Render(fileInfoContent)

	// --- 3. Progress Section ---
	labelStr := "Progress: "
	progLabelStyle := lipgloss.NewStyle().Foreground(colors.Cyan())

	var progContent string
	if contentWidth > 45 { // Enough space for "Progress: " (10) + some bar + padding
		// Horizontal layout: Progress: [████████      ]
		maxProgWidth := contentWidth - lipgloss.Width(labelStr) - components.SingleLineHeight
		if maxProgWidth < 10 {
			maxProgWidth = 10
		}
		d.progress.SetWidth(maxProgWidth)
		progView := d.progress.ViewAs(pct)
		progContent = lipgloss.JoinHorizontal(lipgloss.Center, progLabelStyle.Render(labelStr), progView)
	} else {
		// Vertical layout for narrow terminals:
		// Progress:
		// [███████]
		maxProgWidth := contentWidth
		if maxProgWidth < 10 {
			maxProgWidth = 10 // Still clamp to a readable minimum, but we'll allow wrapping if term is REALLY tiny
		}
		// If contentWidth is actually smaller than 10, we must NOT exceed it to avoid "broken" look
		if maxProgWidth > contentWidth && contentWidth > 5 {
			maxProgWidth = contentWidth
		}

		d.progress.SetWidth(maxProgWidth)
		progView := d.progress.ViewAs(pct)

		centeredLabel := lipgloss.NewStyle().Width(contentWidth).Align(lipgloss.Center).Render(progLabelStyle.Render(labelStr))
		centeredBar := lipgloss.NewStyle().Width(contentWidth).Align(lipgloss.Center).Render(progView)
		progContent = lipgloss.JoinVertical(lipgloss.Left, centeredLabel, centeredBar)
	}

	progSection := lipgloss.NewStyle().Width(contentWidth).Render(progContent)

	// --- 4. Stats Grid Section ---
	var speedStr, etaStr, sizeStr, timeStr string
	// TUI owns elapsed time: compute from StartTime for active downloads,
	// use frozen d.Elapsed for completed downloads.
	var elapsed time.Duration
	if d.done {
		elapsed = d.Elapsed
	} else if d.Elapsed > 0 {
		elapsed = d.Elapsed
	} else if !d.StartTime.IsZero() {
		elapsed = time.Since(d.StartTime)
	}

	// Size
	if d.done {
		sizeStr = utils.FormatBytes(d.Total)
	} else {
		sizeStr = fmt.Sprintf("%s / %s", utils.FormatBytes(d.Downloaded), utils.FormatBytes(d.Total))
	}

	// Speed & ETA
	if d.done {
		if elapsed.Seconds() >= 1 {
			avgSpeed := float64(d.Total) / float64(int(elapsed.Seconds()))
			speedStr = fmt.Sprintf("%s (Avg)", utils.FormatSpeed(avgSpeed))
		} else if d.Speed > 0 {
			speedStr = fmt.Sprintf("%s (Avg)", utils.FormatSpeed(d.Speed))
		} else if elapsed.Seconds() > 0 {
			avgSpeed := float64(d.Total) / elapsed.Seconds()
			speedStr = fmt.Sprintf("%s (Avg)", utils.FormatSpeed(avgSpeed))
		} else {
			speedStr = "N/A"
		}
		etaStr = "Done"
	} else if d.resuming {
		speedStr = "Resuming..."
		etaStr = "..."
	} else if d.rateLimited {
		speedStr = "Rate limited, retrying..."
		etaStr = "..."
	} else if d.paused || d.Speed == 0 {
		speedStr = "Paused"
		if d.RateLimitSet && d.RateLimit > 0 {
			speedStr += fmt.Sprintf(" (Limit: %s)", utils.FormatRateLimit(d.RateLimit))
		} else if d.RateLimitSet {
			speedStr += " (Limit: \u221E)"
		}
		etaStr = "\u221e"
	} else {
		speedStr = utils.FormatSpeed(d.Speed)
		if d.RateLimitSet && d.RateLimit > 0 {
			speedStr += fmt.Sprintf(" (Limit: %s)", utils.FormatRateLimit(d.RateLimit))
		} else if d.RateLimitSet {
			speedStr += " (Limit: \u221E)"
		}
		if d.Speed > 0 && d.Total > 0 && d.lastETA > 0 {
			etaStr = formatDurationForUI(d.lastETA)
		} else {
			etaStr = "\u221e"
		}
	}

	timeStr = formatDurationForUI(elapsed)

	// Stats Layout
	colWidth := (contentWidth - (components.BorderFrameWidth * 2)) / 2
	leftColItems := []string{
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Width(7).Render("Size:"), StatsValueStyle.Render(sizeStr)),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Width(7).Render("Speed:"), StatsValueStyle.Render(speedStr)),
	}
	isActive := !d.done && !d.paused && !d.pausing && d.Speed > 0
	if isActive {
		conns := d.Connections
		if conns == 0 {
			conns = 1 // Single-connection download (range requests not supported)
		}
		connStr := fmt.Sprintf("%d", conns)
		leftColItems = append(leftColItems, lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Width(7).Render("Conns:"), StatsValueStyle.Render(connStr)))
	}
	leftCol := lipgloss.JoinVertical(lipgloss.Left, leftColItems...)
	rightCol := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Width(7).Render("Time:"), StatsValueStyle.Render(timeStr)),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Width(7).Render("ETA:"), StatsValueStyle.Render(etaStr)),
	)

	statsContent := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(colWidth).Render(leftCol),
		lipgloss.NewStyle().Width(colWidth).Render(rightCol),
	)
	statsSection := sectionStyle.Render(statsContent)

	// --- 5. Mirrors Section ---
	var mirrorSection string
	if d.state != nil && len(d.state.GetMirrors()) > 0 {
		activeCount := 0
		errorCount := 0
		total := len(d.state.GetMirrors())
		for _, m := range d.state.GetMirrors() {
			if m.Active {
				activeCount++
			}
			if m.Error {
				errorCount++
			}
		}
		// More prominent Mirrors display
		mirrorLabel := StatsLabelStyle.Render("Mirrors")
		mirrorStats := lipgloss.NewStyle().Foreground(colors.LightGray()).Render(fmt.Sprintf("%d Active / %d Total (%d Errors)", activeCount, total, errorCount))

		mirrorSection = sectionStyle.Render(lipgloss.JoinVertical(lipgloss.Left, mirrorLabel, mirrorStats))
	}

	// --- 6. Error Section ---
	var errorSection string
	if d.err != nil {
		errorSection = sectionStyle.
			Render(lipgloss.NewStyle().Foreground(colors.StateError()).Render("Error: " + d.err.Error()))
	}

	// Combine with Dividers
	// Use explicit calls to insert divider only where needed
	var parts []string

	parts = append(parts, statusBox)
	parts = append(parts, fileSection)
	parts = append(parts, divider)
	parts = append(parts, progSection)
	parts = append(parts, divider)
	parts = append(parts, statsSection)

	if mirrorSection != "" {
		parts = append(parts, divider)
		parts = append(parts, mirrorSection)
	}

	if errorSection != "" {
		parts = append(parts, divider)
		parts = append(parts, errorSection)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Padding(1, 2). // Outer padding
		Render(content)
}

func getDownloadStatus(d *DownloadModel, spinnerView string) string {
	if d.pausing {
		return lipgloss.NewStyle().Foreground(colors.StatePaused()).Render(spinnerView + " Pausing...")
	}
	if d.resuming {
		return lipgloss.NewStyle().Foreground(colors.StateDownloading()).Render(spinnerView + " Resuming...")
	}
	status := components.DetermineStatus(d.done, d.paused, d.err != nil, d.started, d.resuming)
	return status.RenderWithSpinner(spinnerView)
}

// calcTotalSpeedBps calculates the sum of all active downloads' speed in bytes per second.
func (m RootModel) calcTotalSpeedBps() int64 {
	total := int64(0)
	for _, d := range m.downloads {
		// Skip completed downloads
		if d.done {
			continue
		}
		total += int64(d.Speed)
	}
	return total
}

func (m RootModel) ComputeViewStats() ViewStats {
	var stats ViewStats
	for _, d := range m.downloads {
		if d.done {
			stats.DownloadedCount++
		} else if !d.paused && !d.pausing && (d.Speed > 0 || d.Connections > 0 || d.resuming) {
			stats.ActiveCount++
		} else {
			stats.QueuedCount++
		}
		stats.TotalDownloaded += d.Downloaded
	}
	return stats
}

func (m RootModel) renderTabs(activeTab, activeCount, queuedCount, doneCount int) string {
	tabs := []components.Tab{
		{Label: "Queued", Count: queuedCount, Pinned: m.pinnedTab == TabQueued},
		{Label: "Active", Count: activeCount, Pinned: m.pinnedTab == TabActive},
		{Label: "Done", Count: doneCount, Pinned: m.pinnedTab == TabDone},
	}
	return components.RenderTabBar(tabs, activeTab, ActiveTabStyle, TabStyle)
}

func (m RootModel) viewQuitConfirm() string {
	stats := m.ComputeViewStats()
	detail := ""
	if stats.ActiveCount > 0 {
		detail = fmt.Sprintf("%d active download(s) will be paused", stats.ActiveCount)
	}
	w, h := GetDynamicModalDimensions(m.width, m.height, 40, 8, 60, 10)
	modal := components.ConfirmationModal{
		Title:            "Quit Surge",
		Message:          "Are you sure you want to quit?",
		Detail:           detail,
		Keys:             m.keys.QuitConfirm,
		Help:             m.help,
		BorderColor:      colors.Pink(),
		ButtonColor:      colors.Pink(),
		Width:            w,
		Height:           h,
		ShowYesNoButtons: true,
		YesNoFocused:     m.quitConfirmFocused,
		YesLabel:         "Yep!",
		NoLabel:          "Nope",
	}
	return modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
}

func (m RootModel) viewRestartConfirm() string {
	w, h := GetDynamicModalDimensions(m.width, m.height, 40, 8, 60, 10)
	modal := components.ConfirmationModal{
		Title:            "Restart Required",
		Message:          "Settings saved!",
		Detail:           "Restart now to take effect?",
		Keys:             m.keys.QuitConfirm,
		Help:             m.help,
		BorderColor:      colors.Orange(),
		ButtonColor:      colors.Orange(),
		Width:            w,
		Height:           h,
		ShowYesNoButtons: true,
		YesNoFocused:     m.quitConfirmFocused,
		YesLabel:         "Yes",
		NoLabel:          "No",
	}
	return modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
}

func (m RootModel) viewPurgeConfirm() string {
	filename := ""
	if d := m.FindDownloadByID(m.purgeTargetID); d != nil {
		filename = d.Filename
	}

	if filename == "" {
		filename = "this download"
	} else {
		filename = utils.Truncate(filename, 30)
	}

	modal := components.ConfirmationModal{
		Title:            "Purge Download",
		Message:          "Permanently delete this download?",
		Detail:           fmt.Sprintf("File: %s\nThis will also remove the downloaded file(s) from disk.", filename),
		Keys:             m.keys.QuitConfirm, // QuitConfirm works as a general yes/no
		Help:             m.help,
		BorderColor:      colors.Red(),
		ShowYesNoButtons: true,
		YesNoFocused:     m.quitConfirmFocused,
		YesLabel:         "Yes",
		NoLabel:          "No",
	}

	w, h := GetDynamicModalDimensions(m.width, m.height, 46, 8, 60, 12)
	modal.Width = w
	modal.Height = h

	return modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
}

func (m RootModel) viewRemoveConfirm() string {
	filename := ""
	if d := m.FindDownloadByID(m.removeTargetID); d != nil {
		filename = d.Filename
		if filename == "" || filename == "Queued" {
			filename = d.URL
		}
	}

	if filename == "" {
		filename = "this download"
	} else {
		filename = utils.Truncate(filename, 30)
	}

	modal := components.ConfirmationModal{
		Title:            "Remove Download",
		Message:          "Remove this download?",
		Detail:           fmt.Sprintf("File: %s\nThis paused or active download may lose progress data.", filename),
		Keys:             m.keys.QuitConfirm, // QuitConfirm works as a general yes/no
		Help:             m.help,
		BorderColor:      colors.Orange(),
		ButtonColor:      colors.Orange(),
		ShowYesNoButtons: true,
		YesNoFocused:     m.quitConfirmFocused,
		YesLabel:         "Yes",
		NoLabel:          "No",
	}

	w, h := GetDynamicModalDimensions(m.width, m.height, 46, 8, 60, 12)
	modal.Width = w
	modal.Height = h

	return modal.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
}

// renderBtopBox creates a btop-style box with title embedded in the top border
// Supports left and right titles (e.g., search on left, pane name on right)
// Accepts pre-styled title strings
// Example: ╭─ 🔍 Search... ─────────── Downloads ─╮
// Delegates to components.RenderBtopBox for the actual rendering
var renderBtopBox = components.RenderBtopBox
