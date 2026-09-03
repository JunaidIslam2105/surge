package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
)

func (m RootModel) categoryPickerOptions() []string {
	if m.categoryPickerAssign {
		return append([]string{"Uncategorized"}, config.CategoryNames(m.Settings.Categories.Categories)...)
	}
	return append([]string{"", "Uncategorized"}, config.CategoryNames(m.Settings.Categories.Categories)...)
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
		m.categoryPickerTarget = ""
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
		selected := options[m.categoryPickerCursor]
		if m.categoryPickerAssign {
			if d := m.FindDownloadByID(m.categoryPickerTarget); d != nil {
				category := selected
				if category == "Uncategorized" {
					category = ""
				}
				d.categoryOverride = &category
				label := selected
				m.addLogEntry(LogStyleStarted.Render("Category: " + label))
				m.UpdateListItems()
			}
			m.categoryPickerTarget = ""
			m.state = DashboardState
			return m, nil
		}
		m.categoryFilter = selected
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
	contentRows := len(options)
	for i, option := range options {
		label, value := option, ""
		switch option {
		case "":
			label, value = "All Downloads", "Show every download"
		case "Uncategorized":
			if !m.categoryPickerAssign {
				value = "No matching category"
			}
		default:
			for _, category := range m.Settings.Categories.Categories {
				if category.Name == option {
					value = category.Description
					break
				}
			}
		}
		items[i] = components.ListInputItem{Label: label, Value: value}
		if value != "" {
			contentRows++
		}
	}
	// Two border rows, two content-padding rows, and a hint plus its margin.
	w, h := GetDynamicModalDimensions(m.width, m.height, 36, 8, 52, contentRows+6)
	title := "Filter by Category"
	if m.categoryPickerAssign {
		title = "Assign Category"
	}
	subtitle := m.keys.Settings.Up.Help().Key + "/" + m.keys.Settings.Down.Help().Key +
		" navigate   " + m.keys.Settings.Edit.Help().Key + " select   " + m.keys.Settings.Close.Help().Key + " close"
	return components.ListInputModal{
		Title:              title,
		Subtitle:           subtitle,
		PlainSubtitle:      true,
		Items:              items,
		Cursor:             m.categoryPickerCursor,
		BorderColor:        colors.Magenta(),
		InactiveLabelColor: colors.LightGray(),
		Width:              w,
		Height:             h,
		Compact:            true,
	}.RenderWithBtopBox(renderBtopBox, PaneTitleStyle)
}
