package components

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	// BorderFrameHeight is the combined height of top and bottom borders (2)
	BorderFrameHeight = 2
	// BorderFrameWidth is the combined width of left and right borders (2)
	BorderFrameWidth = 2
	// BtopBoxOverheadHeight is the header + footer overhead (2)
	BtopBoxOverheadHeight = 2
	// SingleLineHeight is a standard single line height (1)
	SingleLineHeight = 1
)

// BoxRenderer is the function signature for rendering btop-style boxes
type BoxRenderer func(leftTitle, rightTitle, content string, width, height int, borderColor color.Color) string

// RenderBtopBox creates a btop-style box with title embedded in the top border.
// Supports left and right titles (e.g., search on left, pane name on right).
// Accepts pre-styled title strings.
// Example: ╭─ 🔍 Search... ─────────── Downloads ─╮
func RenderBtopBox(leftTitle, rightTitle string, content string, width, height int, borderColor color.Color) string {
	// Border characters
	const (
		topLeft     = "\u256d"
		topRight    = "\u256e"
		bottomLeft  = "\u2570"
		bottomRight = "\u256f"
		horizontal  = "\u2500"
		vertical    = "\u2502"
	)
	innerWidth := width - BorderFrameWidth
	if innerWidth < 1 {
		innerWidth = 1
	}

	leftTitleWidth := lipgloss.Width(leftTitle)
	rightTitleWidth := lipgloss.Width(rightTitle)

	// Truncate titles so they can never push the top border wider than innerWidth.
	// We always reserve at least one horizontal dash (the one after ╭) so the
	// border looks correct even on very narrow terminals.
	const minBorderDashes = 1
	maxTitleSpace := innerWidth - minBorderDashes
	if maxTitleSpace <= 0 {
		// No room for any title at this width - suppress both
		leftTitle = ""
		leftTitleWidth = 0
		rightTitle = ""
		rightTitleWidth = 0
	} else if leftTitleWidth+rightTitleWidth > maxTitleSpace {
		// Shorten left title first; if still too wide, also shorten right.
		half := maxTitleSpace / 2
		if leftTitleWidth > half {
			leftTitle = lipgloss.NewStyle().MaxWidth(half).Render(leftTitle)
			leftTitleWidth = lipgloss.Width(leftTitle)
		}
		if leftTitleWidth+rightTitleWidth > maxTitleSpace {
			rightRemaining := maxTitleSpace - leftTitleWidth
			if rightRemaining <= 0 {
				rightTitle = ""
				rightTitleWidth = 0
			} else {
				rightTitle = lipgloss.NewStyle().MaxWidth(rightRemaining).Render(rightTitle)
				rightTitleWidth = lipgloss.Width(rightTitle)
			}
		}
	}

	// Calculate remaining horizontal space for the border
	// Structure: ╭ + horizontal*? + leftTitle + horizontal*? + rightTitle + horizontal*? + ╮
	// Basic structure we want:
	// If leftTitle exists: ╭─ leftTitle ──...
	// If rightTitle exists: ...── rightTitle ─╮

	borderStyler := lipgloss.NewStyle().Foreground(borderColor)
	var topBorder string

	// Case 1: Both Titles
	if leftTitle != "" && rightTitle != "" {
		remainingWidth := innerWidth - leftTitleWidth - rightTitleWidth - lipgloss.Width(horizontal)
		if remainingWidth < 0 {
			remainingWidth = 0
		}

		topBorder = borderStyler.Render(topLeft+horizontal) +
			leftTitle +
			borderStyler.Render(strings.Repeat(horizontal, remainingWidth)) +
			rightTitle +
			borderStyler.Render(topRight)
	} else if leftTitle != "" {
		// Case 2: Only Left Title
		remainingWidth := innerWidth - leftTitleWidth - lipgloss.Width(horizontal)
		if remainingWidth < 0 {
			remainingWidth = 0
		}

		topBorder = borderStyler.Render(topLeft+horizontal) +
			leftTitle +
			borderStyler.Render(strings.Repeat(horizontal, remainingWidth)+topRight)
	} else if rightTitle != "" {
		// Case 3: Only Right Title
		remainingWidth := innerWidth - rightTitleWidth - lipgloss.Width(horizontal)
		if remainingWidth < 0 {
			remainingWidth = 0
		}

		topBorder = borderStyler.Render(topLeft+strings.Repeat(horizontal, remainingWidth)) +
			rightTitle +
			borderStyler.Render(horizontal+topRight)
	} else {
		// Case 4: No Title
		topBorder = borderStyler.Render(topLeft + strings.Repeat(horizontal, innerWidth) + topRight)
	}

	// Build bottom border: ╰───────────────────╯
	bottomBorder := borderStyler.Render(
		bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight,
	)

	// Wrap content lines with vertical borders
	contentLines := strings.Split(content, "\n")
	innerHeight := height - BorderFrameHeight // Account for top and bottom borders
	if innerHeight < 0 {
		innerHeight = 0
	}

	// Style for truncation
	truncStyle := lipgloss.NewStyle().MaxWidth(innerWidth)

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
			line = truncStyle.Render(line)
		}
		wrappedLines = append(wrappedLines, borderStyler.Render(vertical)+line+borderStyler.Render(vertical))
	}

	// Every component line is already padded to the exact box width above, so
	// joining directly avoids Lipgloss remeasuring and repadding each line.
	return strings.Join([]string{topBorder, strings.Join(wrappedLines, "\n"), bottomBorder}, "\n")
}
