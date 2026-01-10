package tui

import (
	"fmt"
	"strings"
	"time"

	"surge/internal/utils"

	"github.com/charmbracelet/lipgloss"
)

// Define the Layout Ratios
const (
	ListWidthRatio = 0.6 // List takes 60% width
)

func (m RootModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// === Handle Modal States First ===
	// These overlays sit on top of the dashboard or replace it

	if m.state == InputState {
		labelStyle := lipgloss.NewStyle().Width(10).Foreground(ColorLightGray)
		// Centered popup - compact layout
		hintStyle := lipgloss.NewStyle().MarginLeft(1).Foreground(ColorLightGray) // Secondary
		if m.focusedInput == 1 {
			hintStyle = lipgloss.NewStyle().MarginLeft(1).Foreground(ColorNeonPink) // Highlighted
		}
		pathLine := lipgloss.JoinHorizontal(lipgloss.Left,
			labelStyle.Render("Path:"),
			m.inputs[1].View(),
			hintStyle.Render("[Tab] Browse"),
		)

		// Content layout - removing TitleStyle Render and adding spacers
		content := lipgloss.JoinVertical(lipgloss.Left,
			"", // Top spacer
			lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("URL:"), m.inputs[0].View()),
			"", // Spacer
			pathLine,
			"", // Spacer
			lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("Filename:"), m.inputs[2].View()),
			"", // Bottom spacer
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[Enter] Start  [Esc] Cancel"),
		)

		// Apply padding to the content before boxing it
		paddedContent := lipgloss.NewStyle().Padding(0, 2).Render(content)

		box := renderBtopBox("Add Download", paddedContent, 80, 9, ColorNeonPink, false)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	if m.state == FilePickerState {
		pickerContent := lipgloss.JoinVertical(lipgloss.Left,
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render(m.filepicker.CurrentDirectory),
			"",
			m.filepicker.View(),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[.] Select Here  [H] Downloads  [Enter] Open  [Esc] Cancel"),
		)

		paddedContent := lipgloss.NewStyle().Padding(0, 2).Render(pickerContent)

		box := renderBtopBox("Select Directory", paddedContent, 80, 20, ColorNeonPink, false)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	if m.state == DuplicateWarningState {
		warningContent := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonPink).Bold(true).Render("⚠ DUPLICATE DETECTED"),
			"",
			lipgloss.NewStyle().Foreground(ColorNeonPurple).Bold(true).Render(truncateString(m.duplicateInfo, 50)),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[C] Continue  [F] Focus Existing  [X] Cancel"),
		)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(ColorNeonPink).
				Padding(1, 3).
				Render(warningContent),
		)
	}

	// === MAIN DASHBOARD LAYOUT ===

	availableHeight := m.height - 2 // Margin
	availableWidth := m.width - 4   // Margin

	// Top Row Height (Logo + Graph)
	topHeight := 9

	// Bottom Row Height (List + Details)
	bottomHeight := availableHeight - topHeight - 1
	if bottomHeight < 10 {
		bottomHeight = 10
	} // Min height

	// Column Widths
	leftWidth := int(float64(availableWidth) * ListWidthRatio)
	rightWidth := availableWidth - leftWidth - 2 // -2 for spacing

	// --- SECTION 1: HEADER & LOGO (Top Left) ---
	logoText := `
 ██████  ██    ██ ██████   ██████  ███████ 
██       ██    ██ ██   ██ ██       ██      
███████  ██    ██ ██████  ██   ███ █████   
     ██  ██    ██ ██   ██ ██    ██ ██      
███████   ██████  ██   ██  ██████  ███████`

	// Calculate stats for tab bar
	active, queued, downloaded := m.CalculateStats()

	// Render logo without borders - clean look
	headerBox := lipgloss.NewStyle().
		Width(leftWidth).
		Height(topHeight).
		Padding(1, 2).
		Render(LogoStyle.Render(logoText))

	// --- SECTION 2: SPEED GRAPH (Top Right) ---
	// Calculate dimensions - compact axis for cleaner look
	axisWidth := 6
	// Account for borders and margin
	graphContentWidth := rightWidth - axisWidth - 3
	if graphContentWidth < 10 {
		graphContentWidth = 10
	}

	// Determine Max Speed for the Axis
	maxSpeed := 1.0 // Prevent divide by zero
	for _, v := range m.SpeedHistory {
		if v > maxSpeed {
			maxSpeed = v
		}
	}
	// Add a little headroom (10%) so the graph doesn't always hit the ceiling
	maxSpeed = maxSpeed * 1.1

	// Calculate Available Height for the Graph
	// topHeight (9) - Borders (2) - Title/Spacer lines (2)
	// Title/Speed takes 1 line, Spacer takes 1 line.
	graphHeight := topHeight - 4
	if graphHeight < 1 {
		graphHeight = 1
	}

	// Render the Graph (Multi-line)
	graphVisual := renderMultiLineGraph(m.SpeedHistory, graphContentWidth, graphHeight, maxSpeed, ColorNeonPink)

	// Create the Axis (Left side) - compact labels
	axisStyle := lipgloss.NewStyle().Width(axisWidth).Foreground(ColorGray).Align(lipgloss.Right)

	// Create Axis Labels - compact format without "MB/s" suffix
	labelTop := axisStyle.Render(fmt.Sprintf("%.1f", maxSpeed))
	labelMid := axisStyle.Render(fmt.Sprintf("%.1f", maxSpeed/2))
	labelBot := axisStyle.Render("0")

	// Build the axis column to match graphHeight exactly
	var axisColumn string

	if graphHeight >= 5 {
		// If we have enough space, show Top, Middle, Bottom
		// Distribute spaces evenly
		spacesTotal := graphHeight - 3 // 3 labels
		spaceTop := spacesTotal / 2
		spaceBot := spacesTotal - spaceTop

		axisColumn = lipgloss.JoinVertical(lipgloss.Right,
			labelTop,
			strings.Repeat("\n", spaceTop),
			labelMid,
			strings.Repeat("\n", spaceBot),
			labelBot,
		)
	} else {
		// Compact mode: just Top and Bottom
		spaces := graphHeight - 2
		if spaces < 0 {
			spaces = 0
		}
		axisColumn = lipgloss.JoinVertical(lipgloss.Right,
			labelTop,
			strings.Repeat("\n", spaces),
			labelBot,
		)
	}

	// Combine Axis and Graph
	fullGraphRow := lipgloss.JoinHorizontal(lipgloss.Top,
		axisColumn,
		lipgloss.NewStyle().MarginLeft(1).Render(graphVisual),
	)

	// Get current speed for the title/overlay
	currentSpeed := 0.0
	if len(m.SpeedHistory) > 0 {
		currentSpeed = m.SpeedHistory[len(m.SpeedHistory)-1]
	}
	currentSpeedStr := fmt.Sprintf("Current: %.2f MB/s", currentSpeed)

	// Final Assembly for the box
	speedContent := lipgloss.JoinVertical(lipgloss.Right,
		lipgloss.NewStyle().Foreground(ColorNeonPink).Bold(true).Render(currentSpeedStr),
		"", // Spacer line
		fullGraphRow,
	)

	graphBox := renderBtopBox("Network Activity", speedContent, rightWidth, topHeight, ColorNeonCyan, false)

	// --- SECTION 3: DOWNLOAD LIST (Bottom Left) ---
	// Tab Bar
	tabBar := renderTabs(m.activeTab, active, queued, downloaded)

	// Render the bubbles list or centered empty message
	var listContent string
	if len(m.list.Items()) == 0 {
		// FIX: Reduced width (leftWidth-8) to account for padding (4) and borders (2) + safety
		// preventing the "floating bits" wrap-around artifact.
		listContentHeight := bottomHeight - 6
		listContent = lipgloss.Place(leftWidth-8, listContentHeight, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Render("No downloads"))
	} else {
		listContent = m.list.View()
	}

	listInner := lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		listContent,
	))
	listBox := renderBtopBox("Downloads", listInner, leftWidth, bottomHeight, ColorNeonPink, true)

	// --- SECTION 4: DETAILS PANE (Bottom Right) ---
	var detailContent string
	if d := m.GetSelectedDownload(); d != nil {
		detailContent = renderFocusedDetails(d, rightWidth-4)
	} else {
		detailContent = lipgloss.Place(rightWidth-4, bottomHeight-4, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Render("No Download Selected"))
	}

	detailBox := renderBtopBox("File Details", detailContent, rightWidth, bottomHeight, ColorGray, true)

	// --- ASSEMBLY ---

	// Top Row
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, headerBox, graphBox)

	// Bottom Row
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, listBox, detailBox)

	// Footer - show notification if active, otherwise show keybindings
	var footer string
	if m.notification != "" {
		footer = lipgloss.Place(m.width, 1, lipgloss.Center, lipgloss.Center,
			NotificationStyle.Render(m.notification))
	} else {
		footer = lipgloss.NewStyle().Foreground(ColorLightGray).Padding(0, 1).Render(" [Q/W/E] Tabs  [A] Add  [P] Pause  [X] Delete  [/] Filter  [Ctrl+Q] Quit")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		topRow,
		bottomRow,
		footer,
	)
}

// Helper to render the detailed info pane
func renderFocusedDetails(d *DownloadModel, w int) string {
	pct := 0.0
	if d.Total > 0 {
		pct = float64(d.Downloaded) / float64(d.Total)
	}

	// Progress bar with margins
	progressWidth := w - 12
	if progressWidth < 20 {
		progressWidth = 20
	}
	d.progress.Width = progressWidth
	progView := d.progress.ViewAs(pct)
	// pctStr was previously used for explicit percentage display

	// Consistent content width for centering
	contentWidth := w - 6

	// Section divider
	divider := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render(strings.Repeat("─", contentWidth))

	// File info section - compact layout
	fileInfo := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Filename:"), StatsValueStyle.Render(truncateString(d.Filename, contentWidth-14))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Status:"), StatsValueStyle.Render(getDownloadStatus(d))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Size:"), StatsValueStyle.Render(fmt.Sprintf("%s / %s", utils.ConvertBytesToHumanReadable(d.Downloaded), utils.ConvertBytesToHumanReadable(d.Total)))),
	)

	// Progress section - compact
	progressLabel := lipgloss.NewStyle().
		Foreground(ColorNeonCyan).
		Bold(true).
		Render("PROGRESS")
	progressSection := lipgloss.JoinVertical(lipgloss.Left,
		progressLabel,
		lipgloss.NewStyle().MarginLeft(1).Render(progView),
	)

	// Stats section
	statsSection := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Speed:"), StatsValueStyle.Render(fmt.Sprintf("%.2f MB/s", d.Speed/Megabyte))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Conns:"), StatsValueStyle.Render(fmt.Sprintf("%d", d.Connections))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Elapsed:"), StatsValueStyle.Render(d.Elapsed.Round(time.Second).String())),
	)

	// URL section
	urlSection := lipgloss.JoinHorizontal(lipgloss.Left,
		StatsLabelStyle.Render("URL:"),
		lipgloss.NewStyle().Foreground(ColorLightGray).Render(truncateString(d.URL, contentWidth-14)),
	)

	// Combine all sections - dense layout with dividers
	content := lipgloss.JoinVertical(lipgloss.Left,
		fileInfo,
		divider,
		progressSection,
		divider,
		statsSection,
		divider,
		urlSection,
	)

	// Wrap in a container with reduced padding
	return lipgloss.NewStyle().
		Padding(0, 2).
		Render(content)
}

func getDownloadStatus(d *DownloadModel) string {
	style := lipgloss.NewStyle()

	switch {
	case d.err != nil:
		return style.Foreground(ColorStateError).Render("✖ Error")
	case d.done:
		return style.Foreground(ColorStateDone).Render("✔ Completed")
	case d.paused:
		return style.Foreground(ColorStatePaused).Render("⏸ Paused")
	case d.Speed == 0 && d.Downloaded == 0:
		return style.Foreground(ColorStatePaused).Render("o Queued")
	default:
		return style.Foreground(ColorStateDownloading).Render("⬇ Downloading")
	}
}

func renderMultiLineGraph(data []float64, width, height int, maxVal float64, color lipgloss.Color) string {
	if width < 1 || height < 1 {
		return ""
	}

	// Styles
	gridStyle := lipgloss.NewStyle().Foreground(ColorGray) // Faint color for lines
	barStyle := lipgloss.NewStyle().Foreground(color)      // Bright color for data

	// 1. Prepare the canvas with a Grid
	rows := make([][]string, height)
	for i := range rows {
		rows[i] = make([]string, width)
		for j := range rows[i] {
			// Draw horizontal lines on every other row (or every row if you prefer)
			// Using "╌" gives a nice technical dashed look. "─" is a solid line.
			if i%2 == 0 {
				rows[i][j] = gridStyle.Render("╌")
			} else {
				rows[i][j] = " "
			}
		}
	}

	// 2. Slice data to fit width
	var visibleData []float64
	if len(data) > width {
		visibleData = data[len(data)-width:]
	} else {
		// If not enough data, we process what we have.
		// We do NOT pad with 0s here, because we want the grid
		// to show through on the left side.
		visibleData = data
	}

	// Block characters
	blocks := []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

	// 3. Draw Data Columns
	// We calculate the offset so data fills from the RIGHT
	offset := width - len(visibleData)

	for x, val := range visibleData {
		// Actual X position on the canvas
		canvasX := offset + x

		if val < 0 {
			val = 0
		}

		// Calculate height in "sub-blocks"
		pct := val / maxVal
		if pct > 1.0 {
			pct = 1.0
		}
		totalSubBlocks := pct * float64(height) * 8.0

		// Fill rows from bottom up
		for y := 0; y < height; y++ {
			rowIndex := height - 1 - y // 0 is top, height-1 is bottom

			// Calculate block value for this specific row
			rowValue := totalSubBlocks - float64(y*8)

			var char string
			if rowValue <= 0 {
				// No data for this height? Keep the grid background!
				continue
			} else if rowValue >= 8 {
				char = "█"
			} else {
				char = blocks[int(rowValue)]
			}

			// Overwrite the grid with the data bar
			rows[rowIndex][canvasX] = barStyle.Render(char)
		}
	}

	// 4. Join rows
	var s strings.Builder
	for i, row := range rows {
		s.WriteString(strings.Join(row, ""))
		if i < height-1 {
			s.WriteRune('\n')
		}
	}

	return s.String()
}

func (m RootModel) calcTotalSpeed() float64 {
	total := 0.0
	for _, d := range m.downloads {
		// Skip completed downloads
		if d.done {
			continue
		}
		total += d.Speed
	}
	return total / Megabyte
}

func (m RootModel) CalculateStats() (active, queued, downloaded int) {
	for _, d := range m.downloads {
		if d.done {
			downloaded++
		} else if d.Speed > 0 {
			active++
		} else {
			queued++
		}
	}
	return
}

func truncateString(s string, i int) string {
	runes := []rune(s)
	if len(runes) > i {
		return string(runes[:i]) + "..."
	}
	return s
}

func renderTabs(activeTab, activeCount, queuedCount, doneCount int) string {
	tabs := []struct {
		Label string
		Count int
	}{
		{"Queued", queuedCount},
		{"Active", activeCount},
		{"Done", doneCount},
	}
	var rendered []string
	for i, t := range tabs {
		var style lipgloss.Style
		if i == activeTab {
			style = ActiveTabStyle
		} else {
			style = TabStyle
		}
		label := fmt.Sprintf("%s (%d)", t.Label, t.Count)
		rendered = append(rendered, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// renderBtopBox creates a btop-style box with title embedded in the top border
// titleRight: if true, title appears on the right side; if false, title appears on the left
// Example (left):  ╭─ TITLE ─────────────────────────────────╮
// Example (right): ╭─────────────────────────────────── TITLE ─╮
func renderBtopBox(title string, content string, width, height int, borderColor lipgloss.Color, titleRight bool) string {
	// Border characters
	const (
		topLeft     = "╭"
		topRight    = "╮"
		bottomLeft  = "╰"
		bottomRight = "╯"
		horizontal  = "─"
		vertical    = "│"
	)

	innerWidth := width - 2 // Account for left and right borders
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Build top border with embedded title
	titleText := fmt.Sprintf(" %s ", title)
	titleLen := len(titleText)
	remainingWidth := innerWidth - titleLen - 1 // -1 for the dash after topLeft
	if remainingWidth < 0 {
		remainingWidth = 0
	}

	var topBorder string
	if titleRight {
		// Title on the right: ╭─────────────────────────────────── TITLE ─╮
		topBorder = lipgloss.NewStyle().Foreground(borderColor).Render(topLeft+strings.Repeat(horizontal, remainingWidth)) +
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true).Render(titleText) +
			lipgloss.NewStyle().Foreground(borderColor).Render(horizontal+topRight)
	} else {
		// Title on the left: ╭─ TITLE ─────────────────────────────────╮
		topBorder = lipgloss.NewStyle().Foreground(borderColor).Render(topLeft+horizontal) +
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true).Render(titleText) +
			lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat(horizontal, remainingWidth)) +
			lipgloss.NewStyle().Foreground(borderColor).Render(topRight)
	}

	// Build bottom border: ╰───────────────────╯
	bottomBorder := lipgloss.NewStyle().Foreground(borderColor).Render(
		bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight,
	)

	// Style for vertical borders
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Wrap content lines with vertical borders
	contentLines := strings.Split(content, "\n")
	innerHeight := height - 2 // Account for top and bottom borders

	var wrappedLines []string
	for i := 0; i < innerHeight; i++ {
		var line string
		if i < len(contentLines) {
			line = contentLines[i]
		} else {
			line = ""
		}
		// Pad or truncate line to fit innerWidth
		lineWidth := lipgloss.Width(line)
		if lineWidth < innerWidth {
			line = line + strings.Repeat(" ", innerWidth-lineWidth)
		} else if lineWidth > innerWidth {
			// Truncate (simplified - just take first innerWidth chars)
			runes := []rune(line)
			if len(runes) > innerWidth {
				line = string(runes[:innerWidth])
			}
		}
		wrappedLines = append(wrappedLines, borderStyle.Render(vertical)+line+borderStyle.Render(vertical))
	}

	// Combine all parts
	return lipgloss.JoinVertical(lipgloss.Left,
		topBorder,
		strings.Join(wrappedLines, "\n"),
		bottomBorder,
	)
}
