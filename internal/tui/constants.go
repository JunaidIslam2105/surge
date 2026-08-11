package tui

import "time"

const (
	// === Layout Ratios ===
	ListWidthRatio         = 0.6  // Dashboard: List takes 60% width
	SettingsWidthRatio     = 0.72 // Modals: Settings/Category use 72% width
	LogoWidthRatio         = 0.45 // Header: Logo takes 45% of left column
	GraphTargetHeightRatio = 0.4  // Right Column: Graph target 40% height

	// === Thresholds and Minimums ===
	MinTermWidth             = 45
	MinTermHeight            = 12
	ShortTermHeightThreshold = 25 // Switch to compact header below this height

	MinSettingsWidth      = 64
	MaxSettingsWidth      = 130
	DefaultSettingsHeight = 26

	MinRightColumnWidth = 48 // Keep details visible at the common 120-column width
	MinGraphStatsWidth  = 50 // Show inline graph stats whenever right column is visible
	MinLogoWidth        = 40 // Hide ASCII logo if narrow

	MinGraphHeight      = 9
	MinGraphHeightShort = 5
	MinChunkMapVisibleH = 18 // Min term height to show chunk map

	// === Component Heights ===
	ModalHeightPadding = 4 // Bottom fallback padding for modals to avoid clipping
	HeaderHeightMax    = 11
	HeaderHeightMin    = 3
	FilePickerHeight   = 12

	// === Padding and Offsets ===
	DefaultPaddingX        = 1
	DefaultPaddingY        = 0
	PopupPaddingX          = 2
	PopupPaddingY          = 1
	PopupWidth             = 96 // Fits the complete three-column shortcut list on standard terminals
	HeaderWidthOffset      = 2
	ProgressBarWidthOffset = 4

	// === Layout Offsets (Clean Math) ===
	InternalPaddingHeight = 2 // Standard internal vertical padding
	FooterHeight          = 1 // Application-wide footer height (keybindings)
	DividerHeight         = 1 // Horizontal/Vertical divider line

	// === Graph Configuration ===
	GraphAxisWidth      = 10
	GraphStatsWidth     = 18
	GraphHeadroom       = 1.1                     // Scale max speed by 110% for visual headroom
	GraphUpdateInterval = 1000 * time.Millisecond // Interval at which the speed history is updated
	GraphHistoryPoints  = 60                      // 60 points * 1s = 60s of history

	// === Input Dimensions ===
	InputWidth        = 40
	MinSettingsInputW = 8
	MaxSettingsInputW = 48
)
