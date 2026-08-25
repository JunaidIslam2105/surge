package tui

import (
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
)

type logBoxRenderKey struct {
	width, height int
	version       uint64
	focused       bool
}

// renderLogBox returns the full Activity Log box with borders and title.
func (m *RootModel) renderLogBox(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}

	if m.logBoxCache == nil {
		m.logBoxCache = &renderCache[logBoxRenderKey]{}
	}
	key := logBoxRenderKey{width: width, height: height, version: m.logRenderVersion, focused: m.logFocused}
	if render, ok := m.logBoxCache.Get(key); ok {
		return render
	}

	var innerContent string
	if len(m.logEntries) == 0 {
		innerContent = renderEmptyMessage(width-components.BorderFrameWidth, height-components.BorderFrameHeight, "Activity log is empty")
	} else {
		innerContent = m.logViewport.View()
	}

	logBorderColor := colors.Gray()
	if m.logFocused {
		logBorderColor = colors.Pink()
	}

	render := renderBtopBox(PaneTitleStyle.Render(" Activity Log "), "", innerContent, width, height, logBorderColor)
	return m.logBoxCache.Set(key, render)
}
