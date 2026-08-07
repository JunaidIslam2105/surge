package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/utils"
)

// renderGraphBox returns the network activity sparkline box layout.
func (m *RootModel) renderGraphBox(width, height int, stats ViewStats) string {
	if width < 1 || height < 1 {
		return ""
	}

	contentHeight := height - components.BorderFrameHeight

	if contentHeight < 1 {
		contentHeight = 1
	}

	// Calculate Available Height for the Graph
	// Let's leave 2 lines for top/bottom padding as previous design.
	graphContentHeight := contentHeight - InternalPaddingHeight
	if graphContentHeight < 3 {
		graphContentHeight = 3
	}

	// Determine if we should hide stats box
	hideGraphStats := width < MinGraphStatsWidth

	// Get the last data points for the graph
	var graphData []float64
	if len(m.SpeedHistory) > GraphHistoryPoints {
		graphData = m.SpeedHistory[len(m.SpeedHistory)-GraphHistoryPoints:]
	} else {
		graphData = m.SpeedHistory
	}

	// Determine Max Speed for scaling
	currentSpeedBps := float64(m.cachedTotalSpeed)
	topSpeedBps := 0.0
	for _, s := range m.SpeedHistory {
		if s > topSpeedBps {
			topSpeedBps = s
		}
	}
	if currentSpeedBps > topSpeedBps {
		topSpeedBps = currentSpeedBps
	}

	maxSpeed := 0.0
	for _, v := range graphData {
		if v > maxSpeed {
			maxSpeed = v
		}
	}

	if maxSpeed == 0 {
		maxSpeed = 1000000.0 // Default scale for empty graph
	} else {
		// Add headroom
		maxSpeed = maxSpeed * GraphHeadroom
		if maxSpeed < 1000000.0 {
			maxSpeed = 1000000.0
		}
		mb := maxSpeed / 1000000.0
		if mb >= 5 {
			mb = float64(int((mb+4.99)/5) * 5)
		} else {
			mb = float64(int(mb + 0.99))
		}
		maxSpeed = mb * 1000000.0
	}

	buildAxisLines := func(h int, axisStyle lipgloss.Style) []string {
		label := func(v float64) string {
			if v <= 0 {
				return "0 MiB/s"
			}
			return utils.FormatRateLimit(int64(v))
		}

		axisLines := make([]string, h)
		for i := range axisLines {
			axisLines[i] = axisStyle.Render("")
		}

		type axisMark struct {
			num int
			den int
		}

		marks := []axisMark{
			{num: 1, den: 1},
			{num: 1, den: 2},
			{num: 0, den: 1},
		}
		if h >= 9 {
			marks = []axisMark{
				{num: 1, den: 1},
				{num: 4, den: 5},
				{num: 3, den: 5},
				{num: 2, den: 5},
				{num: 1, den: 5},
				{num: 0, den: 1},
			}
		}

		for _, mark := range marks {
			row := 0
			if h > 1 {
				row = ((mark.den-mark.num)*(h-1) + mark.den/2) / mark.den
			}
			value := maxSpeed * float64(mark.num) / float64(mark.den)
			axisLines[row] = axisStyle.Render(label(value))
		}

		return axisLines
	}

	var graphWithAxis string

	if hideGraphStats {
		// No stats box - graph gets almost full width
		graphAreaWidth, axisWidth := GetGraphAreaDimensions(width, true)
		graphVisual := m.graphRenderer.Render(graphData, graphAreaWidth, graphContentHeight, maxSpeed, m.isResizing())

		axisStyle := lipgloss.NewStyle().Width(axisWidth).Foreground(colors.Cyan()).Align(lipgloss.Right)
		axisLines := buildAxisLines(graphContentHeight, axisStyle)
		axisColumn := lipgloss.NewStyle().
			Height(graphContentHeight).
			Align(lipgloss.Right).
			Render(strings.Join(axisLines, "\n"))

		graphWithAxis = lipgloss.JoinHorizontal(lipgloss.Top, graphVisual, axisColumn)
	} else {
		// Get current speed and calculate total downloaded
		currentSpeed := 0.0
		if len(m.SpeedHistory) > 0 {
			currentSpeed = m.SpeedHistory[len(m.SpeedHistory)-1]
		}

		speedMbps := currentSpeed * 8 / 1000000.0
		topMbps := topSpeedBps * 8 / 1000000.0

		valueStyle := lipgloss.NewStyle().Foreground(colors.Cyan()).Bold(true)
		labelStyleStats := lipgloss.NewStyle().Foreground(colors.LightGray())
		dimStyle := lipgloss.NewStyle().Foreground(colors.Gray())

		speedStr := "0 MiB/s"
		if currentSpeed > 0 {
			speedStr = utils.FormatRateLimit(int64(currentSpeed))
		}
		topStr := "0 MiB/s"
		if topSpeedBps > 0 {
			topStr = utils.FormatRateLimit(int64(topSpeedBps))
		}

		statsContent := lipgloss.JoinVertical(lipgloss.Left,
			fmt.Sprintf("%s %s", valueStyle.Render("\u25bc"), valueStyle.Render(speedStr)),
			dimStyle.Render(fmt.Sprintf("  (%.0f Mbps)", speedMbps)),
			"",
			fmt.Sprintf("%s %s", labelStyleStats.Render("Top:"), valueStyle.Render(topStr)),
			dimStyle.Render(fmt.Sprintf("  (%.0f Mbps)", topMbps)),
			"",
			fmt.Sprintf("%s %s", labelStyleStats.Render("Total:"), valueStyle.Render(utils.FormatBytes(stats.TotalDownloaded))),
		)

		statsBoxStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(colors.Gray()).
			Padding(0, 1). // 1 padding left and right for breathing room
			Width(GraphStatsWidth).
			Height(graphContentHeight)
		statsBox := statsBoxStyle.Render(statsContent)

		graphAreaWidth, axisWidth := GetGraphAreaDimensions(width, false)
		graphVisual := m.graphRenderer.Render(graphData, graphAreaWidth, graphContentHeight, maxSpeed, m.isResizing())

		axisStyle := lipgloss.NewStyle().Width(axisWidth).Foreground(colors.Cyan()).Align(lipgloss.Right)
		axisLines := buildAxisLines(graphContentHeight, axisStyle)
		axisColumn := lipgloss.NewStyle().
			Height(graphContentHeight).
			Align(lipgloss.Right).
			Render(strings.Join(axisLines, "\n"))

		graphWithAxis = lipgloss.JoinHorizontal(lipgloss.Top, statsBox, graphVisual, axisColumn)
	}

	innerContent := lipgloss.JoinVertical(lipgloss.Left, "", graphWithAxis, "")
	return renderBtopBox(PaneTitleStyle.Render(" Network Activity "), "", innerContent, width, height, colors.Cyan())
}
