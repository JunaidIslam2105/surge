package tui

import (
	"fmt"
	"io"

	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/utils"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// DownloadItem implements list.Item interface for downloads
type DownloadItem struct {
	download    *DownloadModel
	spinnerView string
}

func (i DownloadItem) Title() string {
	if i.download.Filename == "" || i.download.Filename == "Queued" {
		return i.download.URL
	}
	return i.download.Filename
}

func (i DownloadItem) Description() string {
	d := i.download

	// Get styled status using the shared component
	var styledStatus string
	if d.pausing {
		styledStatus = lipgloss.NewStyle().Foreground(colors.StatePaused()).Render(i.spinnerView + " Pausing...")
	} else if d.resuming {
		styledStatus = lipgloss.NewStyle().Foreground(colors.StateDownloading()).Render(i.spinnerView + " Resuming...")
	} else {
		status := components.DetermineStatus(d.done, d.paused, d.err != nil, d.started, d.resuming)
		styledStatus = status.RenderWithSpinner(i.spinnerView)
	}

	// Build progress info
	pct := 0.0
	if d.Total > 0 {
		pct = float64(d.Downloaded) / float64(d.Total) * 100
	}

	// Format: "⬇ Downloading • 45% • 2.5 MB/s • 50 MB / 100 MB"
	sizeInfo := fmt.Sprintf("%s / %s",
		utils.FormatBytes(d.Downloaded),
		utils.FormatBytes(d.Total))

	speedInfo := ""
	if d.Speed > 0 {
		speedInfo = fmt.Sprintf(" \u2022 %s", utils.FormatSpeed(d.Speed))
		if d.RateLimitSet && d.RateLimit > 0 {
			speedInfo += fmt.Sprintf(" (Limit: %s)", utils.FormatRateLimit(d.RateLimit))
		} else if d.RateLimitSet {
			speedInfo += " (Limit: \u221E)"
		}
	}

	return fmt.Sprintf("%s \u2022 %.0f%%%s \u2022 %s", styledStatus, pct, speedInfo, sizeInfo)
}

func (i DownloadItem) FilterValue() string {
	if i.download.Filename == "" || i.download.Filename == "Queued" {
		return i.download.URL
	}
	return i.download.Filename
}

// Custom delegate for rendering download items
type downloadDelegate struct {
	keys           *delegateKeyMap
	baseTitleStyle lipgloss.Style
	baseDescStyle  lipgloss.Style
	selTitleStyle  lipgloss.Style
	selDescStyle   lipgloss.Style
	prefixNormal   string
	prefixSelected string
}

type delegateKeyMap struct {
	pause  key.Binding
	delete key.Binding
}

func newDelegateKeyMap() *delegateKeyMap {
	return &delegateKeyMap{
		pause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pause/resume"),
		),
		delete: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "delete"),
		),
	}
}

func newDownloadDelegate() downloadDelegate {
	baseTitle := lipgloss.NewStyle().Foreground(colors.White()).Bold(true)
	baseDesc := lipgloss.NewStyle().Foreground(colors.LightGray())

	selTitle := lipgloss.NewStyle().Foreground(colors.Pink()).Bold(true)
	selDesc := lipgloss.NewStyle().Foreground(colors.Cyan())

	return downloadDelegate{
		keys:           newDelegateKeyMap(),
		baseTitleStyle: baseTitle,
		baseDescStyle:  baseDesc,
		selTitleStyle:  selTitle,
		selDescStyle:   selDesc,
		prefixNormal:   "  ",
		prefixSelected: lipgloss.NewStyle().Foreground(colors.Pink()).Render("\u258c "),
	}
}

func (d downloadDelegate) Height() int  { return 2 }
func (d downloadDelegate) Spacing() int { return 1 }

func (d downloadDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d downloadDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(DownloadItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	var titleStyle, descStyle lipgloss.Style
	var prefix string
	if isSelected {
		titleStyle = d.selTitleStyle
		descStyle = d.selDescStyle
		prefix = d.prefixSelected
	} else {
		titleStyle = d.baseTitleStyle
		descStyle = d.baseDescStyle
		prefix = d.prefixNormal
	}

	// Measure the prefix so we can subtract it from the allowed content width.
	prefixWidth := lipgloss.Width(prefix)

	// Compute the content width available for title/description.
	// We subtract the border frame allowance AND the prefix so the total
	// rendered line (prefix + content) never exceeds m.Width().
	availableWidth := m.Width() - prefixWidth - (components.BorderFrameWidth * 2)
	if availableWidth < 1 {
		availableWidth = 1
	}

	title := utils.TruncateMiddle(i.Title(), availableWidth)
	description := utils.Truncate(i.Description(), availableWidth)

	// Render lines
	line1 := prefix + titleStyle.Render(title)
	line2 := prefix + descStyle.Render(description)

	_, _ = fmt.Fprintf(w, "%s\n%s", line1, line2)
}

// ShortHelp returns keybindings to show in the mini help view
func (d downloadDelegate) ShortHelp() []key.Binding {
	return []key.Binding{d.keys.pause, d.keys.delete}
}

// FullHelp returns keybindings for the expanded help view
func (d downloadDelegate) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{d.keys.pause, d.keys.delete},
	}
}

// NewDownloadList creates a new list.Model configured for downloads
func NewDownloadList(width, height int) list.Model {
	delegate := newDownloadDelegate()

	l := list.New([]list.Item{}, delegate, width, height)
	l.SetShowTitle(false) // Tab bar already shows the category
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false) // Dashboard search owns filtering and focus.
	l.SetShowHelp(false)
	l.SetShowPagination(true)

	applyListTheme(&l)

	return l
}

func applyListTheme(l *list.Model) {
	if l == nil {
		return
	}

	l.SetDelegate(newDownloadDelegate())
	l.SetStatusBarItemName("download", "downloads")

	l.Styles.Title = lipgloss.NewStyle().
		Foreground(colors.Pink()).
		Bold(true).
		Padding(0, 1)

	l.Styles.Filter.Focused.Prompt = lipgloss.NewStyle().Foreground(colors.Cyan())
	l.Styles.Filter.Blurred.Prompt = lipgloss.NewStyle().Foreground(colors.Cyan())
	l.Styles.Filter.Cursor.Color = colors.Pink()

	l.Styles.NoItems = lipgloss.NewStyle().
		Foreground(colors.Cyan()).
		Padding(2, 0)
}

// UpdateListItems updates the list with filtered downloads based on active tab
func (m *RootModel) UpdateListItems() {
	if m.list.Width() == 0 {
		return
	}

	// If the user manually switched tabs, don't try to preserve/follow selection
	if m.ManualTabSwitch {
		m.ManualTabSwitch = false
		filtered := m.getFilteredDownloads()
		items := make([]list.Item, len(filtered))
		sv := m.spinner.View()
		for i, d := range filtered {
			items[i] = DownloadItem{download: d, spinnerView: sv}
		}
		m.list.SetItems(items)
		// Reset cursor to top when manually switching tabs (standard behavior)
		m.list.Select(0)
		return
	}

	// Capture currently selected ID if we don't have a forced one
	targetID := m.SelectedDownloadID
	if targetID == "" {
		if d := m.GetSelectedDownload(); d != nil {
			targetID = d.ID
		}
	}

	filtered := m.getFilteredDownloads()
	items := make([]list.Item, len(filtered))
	sv := m.spinner.View()
	for i, d := range filtered {
		items[i] = DownloadItem{download: d, spinnerView: sv}
	}
	m.list.SetItems(items)

	// Restore selection
	found := false
	if targetID != "" {
		for i, item := range items {
			if di, ok := item.(DownloadItem); ok {
				if di.download.ID == targetID {
					m.list.Select(i)
					found = true
					break
				}
			}
		}

		// If we wanted to select something but it's not here, it might be in another tab
		if !found {
			// Find the download globally
			for _, d := range m.downloads {
				if d.ID == targetID {
					var newTab int
					if d.done {
						newTab = TabDone
					} else if !d.paused && !d.pausing && (d.Speed > 0 || d.Connections > 0 || d.resuming || d.started) {
						newTab = TabActive
					} else {
						newTab = TabQueued
					}

					// If it belongs to a different tab, switch to it (unless current tab is pinned)
					if m.pinnedTab == -1 && newTab != -1 && newTab != m.activeTab {
						m.activeTab = newTab

						// Force selection for the recursive call
						m.SelectedDownloadID = targetID

						// Recurse to update list for the new tab
						m.UpdateListItems()
						return
					}
					break
				}
			}
		}
	}

	// If the previously selected item was removed, ensure the cursor doesn't fall out of bounds
	if !found && len(items) > 0 {
		if m.list.Index() >= len(items) {
			m.list.Select(len(items) - 1)
		}
		if m.list.Index() < 0 {
			m.list.Select(0)
		}
	}

	// Reset forced selection
	m.SelectedDownloadID = ""
}

// GetSelectedDownload returns the currently selected download from the list
func (m *RootModel) GetSelectedDownload() *DownloadModel {
	if item := m.list.SelectedItem(); item != nil {
		if di, ok := item.(DownloadItem); ok {
			return di.download
		}
	}
	return nil
}
