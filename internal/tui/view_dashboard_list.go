package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
)

type listBoxRenderKey struct {
	width, height                       int
	version                             uint64
	activeTab                           int
	activeCount, queuedCount, doneCount int
	searchActive                        bool
	searchQuery, searchView             string
	logFocused                          bool
}

// renderDownloadsBox generates the download list box with the top-left corner search bar string.
func (m *RootModel) renderDownloadsBox(width, height int, stats ViewStats) string {
	if width < 1 || height < 1 {
		return ""
	}
	if m.listBoxCache == nil {
		m.listBoxCache = &renderCache[listBoxRenderKey]{}
	}
	searchView := ""
	if m.searchActive {
		searchView = m.searchInput.View()
	}
	key := listBoxRenderKey{
		width: width, height: height, version: m.listRenderVersion, activeTab: m.activeTab,
		activeCount: stats.ActiveCount, queuedCount: stats.QueuedCount, doneCount: stats.DownloadedCount,
		searchActive: m.searchActive, searchQuery: m.searchQuery, searchView: searchView, logFocused: m.logFocused,
	}
	if render, ok := m.listBoxCache.Get(key); ok {
		return render
	}

	contentWidth := width - components.BorderFrameWidth
	contentHeight := height - components.BorderFrameHeight

	if contentWidth < 0 {
		contentWidth = 0
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	var leftTitle string
	// Tab Bar
	tabBar := m.renderTabs(m.activeTab, stats.ActiveCount, stats.QueuedCount, stats.DownloadedCount)

	// Search bar (shown when search is active or has a query)
	if m.searchActive || m.searchQuery != "" {
		searchIcon := lipgloss.NewStyle().Foreground(colors.Cyan()).Render("> ")
		var searchDisplay string
		if m.searchActive {
			searchDisplay = m.searchInput.View() +
				lipgloss.NewStyle().Foreground(colors.LightGray()).Render(" [esc] Exit")
		} else {
			// Show query with an edit hint.
			searchDisplay = lipgloss.NewStyle().Foreground(colors.Pink()).Render(m.searchQuery) +
				lipgloss.NewStyle().Foreground(colors.LightGray()).Render(" ["+m.keys.Dashboard.Search.Help().Key+"] Edit")
		}
		// Pad the search bar to look like a title block
		leftTitle = " " + lipgloss.JoinHorizontal(lipgloss.Left, searchIcon, searchDisplay) + " "
	}

	// Calculate precise internal sizing
	// padding is 1 top/bottom, 2 left/right
	listPadding := lipgloss.NewStyle().Padding(1, 2)
	padTopBottom := listPadding.GetVerticalFrameSize()   // 2
	padLeftRight := listPadding.GetHorizontalFrameSize() // 4

	// tabBar is effectively 1 line (plus padding below if any, usually 1 or 2 lines)
	tabBarHeight := lipgloss.Height(tabBar)

	// listContentHeight handles available space for the bubbletea list itself
	listContentHeight := contentHeight - padTopBottom - tabBarHeight - 1
	if listContentHeight < 1 {
		listContentHeight = 1
	}

	listContentWidth := contentWidth - padLeftRight
	if listContentWidth < 1 {
		listContentWidth = 1
	}

	var listContent string
	if len(m.list.Items()) == 0 {
		if m.searchQuery != "" {
			listContent = renderEmptyMessage(listContentWidth, listContentHeight, "No matching downloads")
		} else {
			listContent = renderEmptyMessage(listContentWidth, listContentHeight, "No downloads yet")
		}
	} else {
		listContent = m.list.View()
	}

	// Build list inner content - No search bar inside
	listInnerContent := lipgloss.JoinVertical(lipgloss.Left, tabBar, "", listContent)
	innerContent := listPadding.Render(listInnerContent)

	downloadsBorderColor := colors.Pink()
	if m.logFocused {
		downloadsBorderColor = colors.Gray()
	}

	rightTitle := PaneTitleStyle.Render(" Downloads ")

	render := renderBtopBox(leftTitle, rightTitle, innerContent, width, height, downloadsBorderColor)
	return m.listBoxCache.Set(key, render)
}
