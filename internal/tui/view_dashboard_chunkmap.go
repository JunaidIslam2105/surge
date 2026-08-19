package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
)

// chunkMapRenderCache memoizes the rendered chunk map box so the per-block
// recompute (visual chunk downsample + lipgloss.Render per block) only runs
// when the underlying bitmap actually changed, mirroring GraphRenderer.
type chunkMapRenderCache struct {
	selectedID string
	version    uint64
	paused     bool
	width      int
	height     int
	render     string
}

// renderChunkMapBox returns the visual chunk map layout inside a btop box.
func (m *RootModel) renderChunkMapBox(width, height int, selected *DownloadModel, bitmapVersion uint64, bitmap []byte, bitmapWidth int, totalSize, chunkSize int64, chunkProgress []int64) string {
	// Lazy-allocate: View() has a value receiver so inline fields would be
	// discarded each frame; the cache must live behind a pointer like
	// graphRenderer to survive between View() calls.
	if m.chunkMapCache == nil {
		m.chunkMapCache = &chunkMapRenderCache{}
	}
	key := chunkMapRenderCache{
		selectedID: selected.ID,
		version:    bitmapVersion,
		paused:     selected.paused,
		width:      width,
		height:     height,
	}
	// Compare key fields only: the cached render string is not part of the
	// identity (a fresh key always has an empty render).
	c := m.chunkMapCache
	if c.selectedID == key.selectedID &&
		c.version == key.version &&
		c.paused == key.paused &&
		c.width == key.width &&
		c.height == key.height &&
		c.render != "" {
		return c.render
	}

	contentWidth := width - components.BorderFrameWidth
	contentHeight := height - components.BorderFrameHeight

	if contentWidth < 0 {
		contentWidth = 0
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	var innerContent string
	if len(bitmap) == 0 || bitmapWidth == 0 {
		innerContent = renderEmptyMessage(contentWidth, contentHeight, "Chunk visualization not available")
	} else {
		targetRows := contentHeight
		if targetRows < 3 {
			targetRows = 3
		}
		if targetRows > 5 {
			targetRows = 5 // Maximum 5 rows for compact look
		}

		chunkMapPadding := lipgloss.NewStyle().Padding(0, 2)
		chunkMapContentWidth := contentWidth - chunkMapPadding.GetHorizontalFrameSize()
		if chunkMapContentWidth < 4 {
			chunkMapContentWidth = 4
		}

		paused := false
		if selected != nil {
			paused = selected.paused
		}

		chunkMap := components.NewChunkMapModel(bitmap, bitmapWidth, chunkMapContentWidth, targetRows, paused, totalSize, chunkSize, chunkProgress)
		chunkContentWrapper := chunkMapPadding.Render(chunkMap.View())

		innerContent = lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Top, chunkContentWrapper)
	}

	render := renderBtopBox("", PaneTitleStyle.Render(" Chunk Map "), innerContent, width, height, colors.Gray())
	key.render = render
	*m.chunkMapCache = key
	return render
}
