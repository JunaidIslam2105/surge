package tui

import "github.com/charmbracelet/lipgloss"

var (
	// === Palette ===
	// Vibrant "Cyberpunk" Neon Colors
	ColorNeonPurple = lipgloss.Color("#bd93f9")
	ColorNeonPink   = lipgloss.Color("#ff79c6")
	ColorNeonCyan   = lipgloss.Color("#8be9fd")
	ColorDarkGray   = lipgloss.Color("#282a36") // Background
	ColorGray       = lipgloss.Color("#44475a") // Borders
	ColorLightGray  = lipgloss.Color("#a9b1d6") // Brighter text for secondary info
	ColorWhite      = lipgloss.Color("#f8f8f2")

	// Semantic State Colors
	ColorStateError       = lipgloss.Color("#ff5555") // 🔴 Red - Error/Stopped
	ColorStatePaused      = lipgloss.Color("#ffb86c") // 🟡 Orange - Paused/Queued
	ColorStateDownloading = lipgloss.Color("#50fa7b") // 🟢 Green - Downloading
	ColorStateDone        = lipgloss.Color("#bd93f9") // 🔵 Purple - Completed

	// === Layout Styles ===

	// The main box surrounding everything (optional, depending on terminal size)
	AppStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("0")). // Transparent/Default
			Foreground(ColorWhite)

	// Standard pane border
	PaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorGray).
			Padding(0, 1)

	// Focus style for the active pane
	ActivePaneStyle = PaneStyle.
			BorderForeground(ColorNeonPink)

	// === Specific Component Styles ===

	// 1. The "SURGE" Header
	LogoStyle = lipgloss.NewStyle().
			Foreground(ColorNeonPurple).
			Bold(true).
			MarginBottom(1)

	// 2. The Speed Graph (Top Right)
	GraphStyle = PaneStyle.
			BorderForeground(ColorNeonCyan)

	// 3. The Download List (Bottom Left)
	ListStyle = ActivePaneStyle // Usually focused by default

	// 4. The Detail View (Bottom Right)
	DetailStyle = PaneStyle

	// === Text Styles ===

	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Bold(true).
			MarginBottom(1)

	// Helper for bold titles inside panes
	PaneTitleStyle = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Bold(true)

	TabStyle = lipgloss.NewStyle().
			Foreground(ColorLightGray).
			Padding(0, 1)

	ActiveTabStyle = lipgloss.NewStyle().
			Foreground(ColorNeonPink).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorNeonPink).
			Padding(0, 1).
			Bold(true)

	StatsLabelStyle = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Width(12)

	StatsValueStyle = lipgloss.NewStyle().
			Foreground(ColorNeonPink).
			Bold(true)

	// Progress Bar Colors
	ProgressStart = "#ff79c6" // Pink
	ProgressEnd   = "#bd93f9" // Purple

	// Log Entry Styles
	LogStyleStarted = lipgloss.NewStyle().
			Foreground(ColorStateDownloading)

	LogStyleComplete = lipgloss.NewStyle().
				Foreground(ColorStateDone)

	LogStyleError = lipgloss.NewStyle().
			Foreground(ColorStateError)

	LogStylePaused = lipgloss.NewStyle().
			Foreground(ColorStatePaused)
)
