package tui

import "github.com/SurgeDM/Surge/internal/tui/components"

// GetHeaderHeight returns the appropriate header height based on terminal height
func GetHeaderHeight(termHeight int) int {
	if termHeight < ShortTermHeightThreshold {
		return HeaderHeightMin
	}
	return HeaderHeightMax
}

// GetMinGraphHeight returns the minimum graph height based on terminal height
func GetMinGraphHeight(termHeight int) int {
	if termHeight < ShortTermHeightThreshold {
		return MinGraphHeightShort
	}
	return MinGraphHeight
}

// GetSettingsDimensions calculates dimensions for settings/category modals
func GetSettingsDimensions(termWidth, termHeight int) (int, int) {
	width := int(float64(termWidth) * SettingsWidthRatio)
	if width < MinSettingsWidth {
		width = MinSettingsWidth
	}
	if width > MaxSettingsWidth {
		width = MaxSettingsWidth
	}

	maxWidth := termWidth - (WindowStyle.GetHorizontalFrameSize() * 2)
	if maxWidth < 1 {
		maxWidth = 1
	}
	if width > maxWidth {
		width = maxWidth
	}

	height := DefaultSettingsHeight
	maxHeight := termHeight - WindowStyle.GetVerticalFrameSize() - ModalHeightPadding
	if maxHeight < 1 {
		maxHeight = 1
	}
	if height > maxHeight {
		height = maxHeight
	}

	return width, height
}

// GetListWidth calculates the list width based on available width
func GetListWidth(availableWidth int) int {
	leftWidth := int(float64(availableWidth) * ListWidthRatio)

	// Determine right column viability
	rightWidth := availableWidth - leftWidth
	if rightWidth < MinRightColumnWidth {
		return availableWidth
	}
	return leftWidth
}

// IsShortTerminal returns true if the terminal height is below the threshold
func IsShortTerminal(height int) bool {
	return height < ShortTermHeightThreshold
}

// GetGraphAreaDimensions calculates dimensions for the graph area
func GetGraphAreaDimensions(rightWidth int, isStatsHidden bool) (int, int) {
	axisWidth := GraphAxisWidth
	innerWidth := rightWidth - BoxStyle.GetHorizontalFrameSize()

	if isStatsHidden {
		// Graph takes almost full width, minus axis. Use small buffer for safety
		graphAreaWidth := innerWidth - axisWidth - 2
		if graphAreaWidth < 10 {
			graphAreaWidth = 10
		}
		return graphAreaWidth, axisWidth
	}

	// Graph takes remaining width after stats box
	graphAreaWidth := innerWidth - GraphStatsWidth - axisWidth - 2
	if graphAreaWidth < 10 {
		graphAreaWidth = 10
	}
	return graphAreaWidth, axisWidth
}

// CalculateTwoColumnWidths calculates the distribution of widths for a two-column modal layout.
func CalculateTwoColumnWidths(modalWidth, preferredLeft, minRight int) (int, int) {
	horizontalPadding := ModalPaddingStyle.GetHorizontalFrameSize() * 2

	leftWidth := preferredLeft
	if modalWidth-leftWidth-horizontalPadding < minRight {
		leftWidth = modalWidth - minRight - horizontalPadding
	}
	if leftWidth < 16 {
		leftWidth = 16
	}

	rightWidth := modalWidth - leftWidth - horizontalPadding
	if rightWidth < minRight {
		rightWidth = minRight
		if modalWidth-rightWidth-horizontalPadding > 16 {
			leftWidth = modalWidth - rightWidth - horizontalPadding
		}
	}

	return leftWidth, rightWidth
}

// GetDynamicModalDimensions calculates safe dimensions for a modal based on content and terminal size.
func GetDynamicModalDimensions(termW, termH, minW, minH, prefW, contentH int) (int, int) {
	w := prefW
	// Ensure width doesn't exceed terminal
	maxW := termW - (components.BorderFrameWidth * 2)
	if maxW < minW {
		maxW = minW
	}
	if w > maxW {
		w = maxW
	}
	// Final safety check against absolute terminal bounds
	if termW > 0 && w > termW {
		w = termW
	}

	h := contentH
	// Ensure height doesn't exceed terminal
	maxH := termH - components.BorderFrameHeight
	if maxH < minH {
		maxH = minH
	}
	if h > maxH {
		h = maxH
	}
	// Final safety check against absolute terminal bounds
	if termH > 0 && h > termH {
		h = termH
	}

	return w, h
}

// DashboardLayout holds all calculated dimensions for the dashboard components.
type DashboardLayout struct {
	AvailableWidth  int
	AvailableHeight int

	// Columns
	LeftWidth  int
	RightWidth int

	// Header
	LogoWidth    int
	LogWidth     int
	HeaderHeight int

	// Download List
	ListWidth    int
	ListHeight   int
	TabBarHeight int

	// Right Column components
	GraphHeight     int
	MinGraphHeight  int
	DetailHeight    int
	ChunkMapHeight  int
	ShowChunkMap    bool
	HideRightColumn bool
	VerticalLayout  bool
}

// CalculateDashboardLayout computes the layout mapping for all dashboard components based on terminal size.
func CalculateDashboardLayout(termW, termH int) DashboardLayout {
	l := DashboardLayout{}

	l.AvailableWidth = termW - WindowStyle.GetHorizontalFrameSize()
	l.AvailableHeight = termH - WindowStyle.GetVerticalFrameSize() - FooterHeight // Account for 1-line footer
	l.ShowChunkMap = l.AvailableHeight >= MinChunkMapVisibleH

	if l.AvailableWidth < 0 {
		l.AvailableWidth = 0
	}
	if l.AvailableHeight < 0 {
		l.AvailableHeight = 0
	}

	// 1. Column widths
	l.LeftWidth = GetListWidth(l.AvailableWidth)
	l.RightWidth = l.AvailableWidth - l.LeftWidth
	l.HideRightColumn = l.RightWidth < MinRightColumnWidth

	if l.HideRightColumn {
		l.LeftWidth = l.AvailableWidth
	}

	// 2. Header Dimensions
	l.HeaderHeight = GetHeaderHeight(l.AvailableHeight)
	l.LogoWidth = int(float64(l.LeftWidth) * LogoWidthRatio)
	if l.LogoWidth < 4 {
		l.LogoWidth = 4
	}
	l.LogWidth = l.LeftWidth - l.LogoWidth
	if l.LogWidth < 4 {
		l.LogWidth = 4
	}

	// 3. Right Column Heights
	if !l.HideRightColumn {
		l.MinGraphHeight = GetMinGraphHeight(l.AvailableHeight)
		targetGraphH := int(float64(l.AvailableHeight) * GraphTargetHeightRatio)
		if targetGraphH < l.MinGraphHeight {
			targetGraphH = l.MinGraphHeight
		}

		// Compute heights assuming chunk map may be shown.
		// The actual decision to render the chunk map is made dynamically
		// in View() by measuring the rendered detail content against the
		// allocated DetailHeight. This avoids any hardcoded height guesses.

		if l.ShowChunkMap {
			l.ChunkMapHeight = 5 + components.BorderFrameHeight // 5 rows of content + 2 for borders
			l.GraphHeight = targetGraphH
			l.DetailHeight = l.AvailableHeight - l.GraphHeight - l.ChunkMapHeight
		} else {
			l.GraphHeight = targetGraphH
			l.DetailHeight = l.AvailableHeight - l.GraphHeight
		}
	}

	// 4. Download List Dimensions
	l.ListHeight = l.AvailableHeight - l.HeaderHeight
	l.ListWidth = l.LeftWidth

	// Handle Vertical Layout for narrow but tall terminals
	// Only show details if we can maintain at least 10 lines for the downloads list
	remainingH := l.AvailableHeight - l.HeaderHeight
	if l.HideRightColumn && remainingH >= 20 {
		l.VerticalLayout = true
		l.ListHeight = remainingH / 2
		l.DetailHeight = remainingH - l.ListHeight
	}

	if l.ListHeight < 4 {
		l.ListHeight = 4
	}
	l.TabBarHeight = 1 // standard height

	return l
}
