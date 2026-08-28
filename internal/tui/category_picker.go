package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
)

func (m RootModel) categoryPickerOptions() []string {
	return append(append([]string{""}, config.CategoryNames(m.Settings.Categories.Categories)...), "Uncategorized")
}

func (m RootModel) categoryFilterPickerCursor() int {
	for i, option := range m.categoryPickerOptions() {
		if option == m.categoryFilter {
			return i
		}
	}
	return 0
}

func (m RootModel) updateCategoryPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	options := m.categoryPickerOptions()
	if key.Matches(msg, m.keys.Settings.Close) {
		m.state = DashboardState
		return m, nil
	}
	if key.Matches(msg, m.keys.Settings.Up) {
		m.categoryPickerCursor = (m.categoryPickerCursor - 1 + len(options)) % len(options)
		return m, nil
	}
	if key.Matches(msg, m.keys.Settings.Down) {
		m.categoryPickerCursor = (m.categoryPickerCursor + 1) % len(options)
		return m, nil
	}
	if key.Matches(msg, m.keys.Settings.Edit) {
		m.categoryFilter = options[m.categoryPickerCursor]
		label := m.categoryFilter
		if label == "" {
			label = "All"
		}
		m.addLogEntry(LogStyleStarted.Render("Filter: " + label))
		m.UpdateListItems()
		m.state = DashboardState
	}
	return m, nil
}

func (m RootModel) viewCategoryPicker() string {
	options := m.categoryPickerOptions()
	items := make([]components.ListInputItem, len(options))
	for i, option := range options {
		label, value := option, ""
		switch option {
		case "":
			label, value = "All Downloads", "Show every download"
		case "Uncategorized":
			value = "No matching category"
		default:
			for _, category := range m.Settings.Categories.Categories {
				if category.Name == option {
					value = category.Description
					break
				}
			}
		}
		items[i] = components.ListInputItem{Label: label, Value: value}
	}
	w, h := GetDynamicModalDimensions(m.width, m.height, 40, 10, 65, 22)
	return components.ListInputModal{
		Title:       "Filter by Category",
		Subtitle:    "\u2191/\u2193 choose \u2022 enter apply \u2022 esc cancel",
		Items:       items,
		Cursor:      m.categoryPickerCursor,
		BorderColor: colors.Magenta(),
		Width:       w,
		Height:      h,
	}.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
}
