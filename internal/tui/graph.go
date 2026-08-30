package tui

import (
	"image/color"
	"strings"

	"github.com/SurgeDM/Surge/internal/tui/colors"

	"charm.land/lipgloss/v2"
)

func graphColors() []color.Color {
	return colors.GraphColors()
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

	// Fingerprint of the inputs that produced lastRender, so a no-op frame
	// (graph data only updates every GraphUpdateInterval) skips the whole
	// rebuild + RLE pass instead of re-rendering identical output.
	lastData   []float64
	lastWidth  int
	lastHeight int
	lastMax    float64
}

func NewGraphRenderer() *GraphRenderer {
	return &GraphRenderer{
		gridStyle: lipgloss.NewStyle().Foreground(colors.Gray()),
	}
}

func (g *GraphRenderer) InvalidateCache() {
	g.baseGrid = nil
	g.lastRender = ""
	g.lastData = nil
	g.gridStyle = lipgloss.NewStyle().Foreground(colors.Gray())
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
		// Calculate how far up this row is as a percentage (0.0 to 1.0)
		// y=0 is the bottom row.
		pct := float64(y) / float64(height)
		if height > 1 {
			pct = float64(y) / float64(height-1)
		}

		// Apply the non-linear thresholds:
		// 0% - 10%:  index 0
		// 10% - 30%: index 1
		// 30% - 60%: index 2
		// 60% - 100%: index 3
		var colorIdx int
		if pct <= 0.10 {
			colorIdx = 0
		} else if pct <= 0.30 {
			colorIdx = 1
		} else if pct <= 0.60 {
			colorIdx = 2
		} else {
			colorIdx = 3
		}
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

	effectiveMaxVal := maxVal
	if len(data) > 0 && effectiveMaxVal <= 0 {
		effectiveMaxVal = 1
	}

	// Fast path: input fingerprint unchanged since the last render. The
	// speed history only advances every GraphUpdateInterval, but View() is
	// called far more often (spinner ticks), so this skips the per-block
	// buffer work and RLE pass on every no-change frame.
	if g.lastRender != "" && g.lastWidth == width && g.lastHeight == height && g.lastMax == effectiveMaxVal && sameFloat64s(g.lastData, data) {
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
			pct := val / effectiveMaxVal
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

	// Record the fingerprint for the next frame's fast-path check.
	g.lastData = append(g.lastData[:0], data...)
	g.lastWidth = width
	g.lastHeight = height
	g.lastMax = effectiveMaxVal
	return g.lastRender
}

// sameFloat64s reports whether a and b have equal length and values.
func sameFloat64s(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
