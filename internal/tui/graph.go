package tui

import (
	"image/color"
	"strings"

	"github.com/SurgeDM/Surge/internal/tui/colors"

	"charm.land/lipgloss/v2"
)

func graphColors() []color.Color {
	return []color.Color{
		colors.ProgressStart(), // Bottom
		colors.Magenta(),
		colors.Pink(),
		colors.ProgressEnd(), // Top
	}
}

var graphBlocks = []string{" ", "\u2581", "\u2582", "\u2583", "\u2584", "\u2585", "\u2586", "\u2588"}

// GraphRenderer is a stateful, highly optimized component for rendering the network activity graph.
// It caches the background grid and style objects, and uses run-length encoding to minimize ANSI escape sequences.
// NOTE: This component is NOT safe for concurrent use.
type GraphRenderer struct {
	width, height int

	// Caches
	gridStyle lipgloss.Style
	rowStyles []lipgloss.Style

	// Base pristine grid (raw characters)
	baseGrid [][]string

	// Reusable buffers per frame
	charBuf  [][]string
	styleBuf [][]bool // false = grid style, true = block style (row color)

	lastRender string
}

func NewGraphRenderer() *GraphRenderer {
	return &GraphRenderer{
		gridStyle: lipgloss.NewStyle().Foreground(colors.Gray()),
	}
}

func (g *GraphRenderer) InvalidateCache() {
	g.baseGrid = nil
	g.lastRender = ""
}

func (g *GraphRenderer) resize(width, height int) {
	if g.width == width && g.height == height && g.baseGrid != nil {
		return
	}

	g.width = width
	g.height = height

	// 1. Build row styles
	gradient := graphColors()
	g.rowStyles = make([]lipgloss.Style, height)
	for y := 0; y < height; y++ {
		colorIdx := (y * len(gradient)) / height
		if colorIdx >= len(gradient) {
			colorIdx = len(gradient) - 1
		}
		g.rowStyles[y] = lipgloss.NewStyle().Foreground(gradient[colorIdx])
	}

	// 2. Build base grid
	g.baseGrid = make([][]string, height)
	for i := range g.baseGrid {
		g.baseGrid[i] = make([]string, width)
		for j := range g.baseGrid[i] {
			if i == height-1 {
				g.baseGrid[i][j] = "\u2500"
			} else if i%2 == 0 {
				g.baseGrid[i][j] = "\u254c"
			} else {
				g.baseGrid[i][j] = " "
			}
		}
	}

	// 3. Allocate buffers (old buffers will be garbage collected)
	g.charBuf = make([][]string, height)
	g.styleBuf = make([][]bool, height)
	for i := 0; i < height; i++ {
		g.charBuf[i] = make([]string, width)
		g.styleBuf[i] = make([]bool, width)
	}
}

// Render creates a multi-line bar graph with grid lines.
func (g *GraphRenderer) Render(data []float64, width, height int, maxVal float64, isResizing bool) string {
	if width < 1 || height < 1 {
		return ""
	}

	if isResizing && g.lastRender != "" {
		return g.lastRender
	}

	g.resize(width, height)

	// 1. Deep copy pristine grid and zero style buffer
	for i := 0; i < height; i++ {
		copy(g.charBuf[i], g.baseGrid[i])
		// Zeroing styleBuf (false = grid style)
		for j := 0; j < width; j++ {
			g.styleBuf[i][j] = false
		}
	}

	// 2. Map data
	if len(data) > 0 {
		// Bug fix: maxVal <= 0 causes NaN
		if maxVal <= 0 {
			maxVal = 1
		}

		// Bug fix: Downsample if data > width to prevent column loss
		var plotData []float64
		if len(data) > width {
			plotData = make([]float64, width)
			chunkSize := float64(len(data)) / float64(width)
			for i := 0; i < width; i++ {
				start := int(float64(i) * chunkSize)
				end := int(float64(i+1) * chunkSize)
				if i == width-1 {
					end = len(data) // Ensure tail data point is never dropped
				}
				if end > len(data) {
					end = len(data)
				}
				maxInChunk := 0.0
				for j := start; j < end; j++ {
					if data[j] > maxInChunk {
						maxInChunk = data[j]
					}
				}
				plotData[i] = maxInChunk
			}
		} else {
			plotData = data
		}

		colsPerPoint := float64(width) / float64(len(plotData))

		for i, val := range plotData {
			if val < 0 {
				val = 0
			}
			pct := val / maxVal
			if pct > 1.0 {
				pct = 1.0
			}
			totalSubBlocks := pct * float64(height) * 8.0

			startCol := int(float64(i) * colsPerPoint)
			endCol := int(float64(i+1) * colsPerPoint)
			if endCol > width {
				endCol = width
			}

			for col := startCol; col < endCol; col++ {
				for y := 0; y < height; y++ {
					rowIndex := height - 1 - y
					rowValue := totalSubBlocks - float64(y*8)

					var charIndex int
					if rowValue <= 0 {
						continue // Leave grid as is
					} else if rowValue >= 8 {
						charIndex = 7 // Full block
					} else {
						charIndex = int(rowValue)
					}

					if charIndex > 0 {
						g.charBuf[rowIndex][col] = graphBlocks[charIndex]
						g.styleBuf[rowIndex][col] = true // Mark as block style
					}
				}
			}
		}
	}

	// 3. RLE & String Building
	// Estimate 15 bytes per styled run (ansi code + char + ansi clear)
	var graphBuilder strings.Builder
	graphBuilder.Grow(width * height * 15)

	for i := 0; i < height; i++ {
		rowChars := g.charBuf[i]
		rowStyles := g.styleBuf[i]

		currentStr := rowChars[0]
		currentStyleBlock := rowStyles[0]
		runLen := 1

		for j := 1; j < width; j++ {
			if rowChars[j] == currentStr && rowStyles[j] == currentStyleBlock {
				runLen++
			} else {
				// Emit previous run
				if currentStyleBlock {
					graphBuilder.WriteString(g.rowStyles[height-1-i].Render(strings.Repeat(currentStr, runLen)))
				} else {
					graphBuilder.WriteString(g.gridStyle.Render(strings.Repeat(currentStr, runLen)))
				}

				currentStr = rowChars[j]
				currentStyleBlock = rowStyles[j]
				runLen = 1
			}
		}

		// Emit final run for this row
		if currentStyleBlock {
			graphBuilder.WriteString(g.rowStyles[height-1-i].Render(strings.Repeat(currentStr, runLen)))
		} else {
			graphBuilder.WriteString(g.gridStyle.Render(strings.Repeat(currentStr, runLen)))
		}

		if i < height-1 {
			graphBuilder.WriteRune('\n')
		}
	}

	g.lastRender = graphBuilder.String()
	return g.lastRender
}
