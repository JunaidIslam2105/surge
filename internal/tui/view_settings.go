package tui

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/utils"

	"charm.land/lipgloss/v2"
)

// viewSettings renders the Btop-style settings page
func (m RootModel) viewSettings() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	width, height := GetSettingsDimensions(m.width, m.height)
	if width < MinSettingsWidth || height < 10 { // Special threshold for settings rendering floor
		content := lipgloss.NewStyle().
			Padding(DefaultPaddingY, DefaultPaddingX*2).
			Foreground(colors.LightGray()).
			Render("Terminal too small for settings view")
		box := renderBtopBox(PaneTitleStyle.Render(" Settings "), "", content, width, height, colors.Magenta())
		return m.renderModalWithOverlay(box)
	}

	categories := config.CategoryOrder()
	if len(categories) == 0 {
		content := lipgloss.NewStyle().
			Padding(1, 2).
			Foreground(colors.LightGray()).
			Render("No settings categories available")
		box := renderBtopBox(PaneTitleStyle.Render(" Settings "), "", content, width, height, colors.Magenta())
		return m.renderModalWithOverlay(box)
	}

	activeTab := m.SettingsActiveTab
	if activeTab < 0 {
		activeTab = 0
	}
	if activeTab >= len(categories) {
		activeTab = len(categories) - 1
	}

	currentCategory := categories[activeTab]
	settingsMeta := m.buildSettingsMetaForCategory(currentCategory)
	if len(settingsMeta) == 0 {
		content := lipgloss.NewStyle().
			Padding(1, 2).
			Foreground(colors.LightGray()).
			Render("No settings available in this category")
		box := renderBtopBox(PaneTitleStyle.Render(" Settings "), "", content, width, height, colors.Magenta())
		return m.renderModalWithOverlay(box)
	}

	selectedRow := m.SettingsSelectedRow
	if selectedRow < 0 {
		selectedRow = 0
	}
	if selectedRow >= len(settingsMeta) {
		selectedRow = len(settingsMeta) - 1
	}

	settingsValues := m.getSettingsValues(currentCategory)
	tabBar := m.renderSettingsTabBar(categories, activeTab, width-(ProgressBarWidthOffset+HeaderWidthOffset))
	helpText := m.renderSettingsHelp(width - (ProgressBarWidthOffset + HeaderWidthOffset))

	innerHeight := height - BoxStyle.GetVerticalFrameSize()
	tabBarHeight := lipgloss.Height(tabBar)
	helpHeight := lipgloss.Height(helpText)

	errorLine := ""
	errorHeight := 0
	if m.settingsError != "" {
		// Use MaxWidth to prevent horizontal overflow from long error messages
		errorLine = lipgloss.NewStyle().
			Foreground(colors.StateError()).
			Bold(true).
			Padding(0, 2).
			MaxWidth(width - 6).
			Render("\u2716 " + m.settingsError)
		errorHeight = lipgloss.Height(errorLine)
	}

	// Calculate gaps. We want:
	// tabBar
	// <gap>
	// errorLine (if present)
	// <gap if error present>
	// content
	// <padding flex space>
	// <gap before help if space allows>
	// helpText

	fixedOverhead := tabBarHeight + helpHeight + 1 // 1 for the gap after tab bar
	if errorHeight > 0 {
		fixedOverhead += errorHeight + 1 // another gap after error
	}

	bodyHeight := innerHeight - fixedOverhead
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	var content string
	if width >= 72 && bodyHeight >= 8 {
		content = m.renderSettingsTwoColumn(settingsMeta, selectedRow, settingsValues, width, bodyHeight)
	} else {
		content = m.renderSettingsCompact(settingsMeta, selectedRow, settingsValues, width, bodyHeight)
	}

	contentHeight := lipgloss.Height(content)
	usedHeight := fixedOverhead + contentHeight

	paddingLines := innerHeight - usedHeight
	if paddingLines < 0 {
		paddingLines = 0
	}

	parts := []string{tabBar, ""} // tabBar and first gap
	if errorLine != "" {
		parts = append(parts, errorLine, "") // errorLine and second gap
	}
	parts = append(parts, content)

	// Add flexible padding to push help text to bottom
	for i := 0; i < paddingLines; i++ {
		parts = append(parts, "")
	}
	parts = append(parts, helpText)

	fullContent := lipgloss.JoinVertical(lipgloss.Left, parts...)

	box := renderBtopBox(PaneTitleStyle.Render(" Settings "), "", fullContent, width, height, colors.Magenta())
	return m.renderModalWithOverlay(box)
}

func shortSettingsCategoryLabel(label string) string {
	switch label {
	case "General":
		return "Gen"
	case "Network":
		return "Net"
	case "Performance":
		return "Perf"
	case "Categories":
		return "Cats"
	case "Extension":
		return "Ext"
	default:
		return label
	}
}

func (m RootModel) renderSettingsTabBar(categories []string, activeTab int, maxWidth int) string {
	if maxWidth < 1 {
		maxWidth = 1
	}

	makeTabs := func(useShort bool) []components.Tab {
		tabs := make([]components.Tab, 0, len(categories))
		for _, cat := range categories {
			label := cat
			if useShort {
				label = shortSettingsCategoryLabel(cat)
			}
			tabs = append(tabs, components.Tab{Label: label, Count: -1})
		}
		return tabs
	}

	var settingsActiveTab lipgloss.Style
	if m.SettingsFocusedPane == 0 {
		settingsActiveTab = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(colors.Magenta()).
			Bold(true)
	} else {
		settingsActiveTab = lipgloss.NewStyle().Foreground(colors.Magenta())
	}

	tryBars := []string{
		components.RenderNumberedTabBar(makeTabs(false), activeTab, settingsActiveTab, TabStyle),
		components.RenderTabBar(makeTabs(false), activeTab, settingsActiveTab, TabStyle),
		components.RenderTabBar(makeTabs(true), activeTab, settingsActiveTab, TabStyle),
	}

	for _, candidate := range tryBars {
		if lipgloss.Width(candidate) <= maxWidth {
			return lipgloss.NewStyle().Width(maxWidth).Align(lipgloss.Center).Render(candidate)
		}
	}

	fallback := fmt.Sprintf("[%d/%d] %s", activeTab+1, len(categories), categories[activeTab])
	fallbackStyle := lipgloss.NewStyle().Foreground(colors.Gray())
	if m.SettingsFocusedPane == 0 {
		fallbackStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(colors.Magenta()).
			Bold(true)
	}
	return fallbackStyle.
		Width(maxWidth).
		Align(lipgloss.Center).
		Render(fallback)
}

func (m RootModel) renderSettingsHelp(width int) string {
	if width < 1 {
		width = 1
	}

	helpText := m.help.View(m.keys.Settings)
	if width < 60 {
		helpText = "esc: save/close  tab: next tab  enter: edit"
	}
	if width < 40 {
		helpText = "esc close | enter edit"
	}

	return lipgloss.NewStyle().
		Foreground(colors.Gray()).
		Width(width).
		Align(lipgloss.Center).
		Render(helpText)
}

func formatSettingsBlock(content string, width, rows int) string {
	if width < 1 {
		width = 1
	}
	if rows < 1 {
		rows = 1
	}

	lines := strings.Split(content, "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}

	for i := range lines {
		lines[i] = lipgloss.NewStyle().Width(width).MaxWidth(width).Render(lines[i])
	}

	return strings.Join(lines, "\n")
}

func (m RootModel) renderSettingsListViewport(settingsMeta []config.SettingMeta, selectedRow, rows, innerWidth int) string {
	if rows < 1 {
		rows = 1
	}
	if innerWidth < 1 {
		innerWidth = 1
	}

	if len(settingsMeta) == 0 {
		return formatSettingsBlock("(No settings)", innerWidth, rows)
	}

	if selectedRow < 0 {
		selectedRow = 0
	}
	if selectedRow >= len(settingsMeta) {
		selectedRow = len(settingsMeta) - 1
	}

	start := selectedRow - (rows / 2)
	if start < 0 {
		start = 0
	}
	maxStart := len(settingsMeta) - rows
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}

	lines := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		idx := start + i
		if idx >= len(settingsMeta) {
			lines = append(lines, "")
			continue
		}

		meta := settingsMeta[idx]
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(colors.LightGray())
		if idx == selectedRow {
			if m.SettingsFocusedPane == 1 {
				prefix = "\u25b8 "
				style = lipgloss.NewStyle().Foreground(colors.Cyan()).Bold(true)
			} else {
				prefix = "  "
				style = lipgloss.NewStyle().Foreground(colors.Gray()).Bold(true)
			}
		}

		if meta.Key == "max_global_connections" {
			style = lipgloss.NewStyle().Foreground(colors.ThemeColor("#aaaaaa", "238"))
			if idx == selectedRow {
				if m.SettingsFocusedPane == 1 {
					prefix = "# "
					style = lipgloss.NewStyle().Foreground(colors.Gray())
				} else {
					prefix = "  "
					style = lipgloss.NewStyle().Foreground(colors.ThemeColor("#777777", "236"))
				}
			}
		}

		label := meta.Label
		maxLabelLen := innerWidth - len(prefix)
		if maxLabelLen < 0 {
			maxLabelLen = 0
		}

		// Truncate to avoid line wrapping which breaks parent height constraints
		label = utils.TruncateMiddle(label, maxLabelLen)

		lines = append(lines, style.Width(innerWidth).MaxWidth(innerWidth).Render(prefix+label))
	}

	return strings.Join(lines, "\n")
}

func (m RootModel) renderSettingsDetailBlock(settingsMeta []config.SettingMeta, selectedRow int, settingsValues map[string]interface{}, innerWidth, rows int) string {
	if innerWidth < 1 {
		innerWidth = 1
	}
	if rows < 1 {
		rows = 1
	}
	if len(settingsMeta) == 0 || selectedRow < 0 || selectedRow >= len(settingsMeta) {
		return formatSettingsBlock("No setting selected", innerWidth, rows)
	}

	meta := settingsMeta[selectedRow]
	value := settingsValues[meta.Key]
	unit := m.getSettingUnit()
	unitStyle := lipgloss.NewStyle().Foreground(colors.Gray())

	valueLabel := "Value: "
	var valueStr string
	if m.SettingsIsEditing {
		if meta.Type == config.TypeCustomCategory || meta.Type == config.TypeCustomCategoryAdd {
			valueStr = lipgloss.JoinVertical(lipgloss.Left,
				m.renderCategoryInputLine("Name:", 0, innerWidth),
				m.renderCategoryInputLine("Desc:", 1, innerWidth),
				m.renderCategoryInputLine("Regex:", 2, innerWidth),
				m.renderCategoryInputLine("Path:", 3, innerWidth),
			)
			valueLabel = ""
		} else {
			valueStr = m.SettingsInput.View() + unitStyle.Render(unit)
		}
	} else {
		switch meta.Type {
		case config.TypeAuthToken:
			token := GetAuthToken()
			if token == "" {
				valueStr = lipgloss.NewStyle().Foreground(colors.Gray()).Render("(Not generated yet)")
			} else {
				if m.ExtensionTokenCopied {
					valueStr = lipgloss.NewStyle().Foreground(colors.StateDownloading()).Bold(true).Render("Copied!")
				} else {
					displayToken := token
					if len(token) > 16 {
						displayToken = token[:8] + "..." + token[len(token)-8:]
					}
					valueStr = displayToken + lipgloss.NewStyle().Foreground(colors.Gray()).Render(" [Enter to Copy]")
				}
			}
		case config.TypeLink:
			valueStr = lipgloss.NewStyle().Foreground(colors.Cyan()).Render("Open [Enter]")
			valueLabel = "Action: "
		case config.TypeCustomCategoryAdd:
			valueStr = lipgloss.NewStyle().Foreground(colors.Cyan()).Render("Create Category [Enter]")
			valueLabel = "Action: "
		case config.TypeCustomCategory:
			cat, ok := value.(config.Category)
			if !ok {
				break
			}
			labelStyle := lipgloss.NewStyle().Foreground(colors.LightGray()).Width(8)
			valueStr = lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render("Name:"), utils.TruncateMiddle(cat.Name, innerWidth-20)),
				lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render("Desc:"), utils.TruncateMiddle(cat.Description, innerWidth-20)),
				lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render("Regex:"), utils.TruncateMiddle(cat.Pattern, innerWidth-20)),
				lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render("Path:"), utils.TruncateMiddle(cat.Path, innerWidth-20)),
			)
			valueLabel = ""
		default:
			valueStr = formatSettingValueForEdit(value, meta.Type, meta.Key, true)
			if valueStr != "\u221E" {
				valueStr += unitStyle.Render(unit)
			}
			if meta.Key == "max_global_connections" {
				valueStr += " (Ignored)"
			}
		}
	}

	if (meta.Key == "default_download_dir" || meta.Key == "theme_path") && !m.SettingsIsEditing {
		valueLabel = "[Tab] Browse: "
	}

	valueLabelStyle := lipgloss.NewStyle().Foreground(colors.LightGray()).Bold(true)
	valueContentStyle := lipgloss.NewStyle().Foreground(colors.White())

	labelRendered := valueLabelStyle.Render(valueLabel)
	availableValueWidth := innerWidth - lipgloss.Width(labelRendered)
	if availableValueWidth < 5 {
		availableValueWidth = 5
	}

	var valueDisplay string
	if meta.Type == config.TypeCustomCategory || meta.Type == config.TypeCustomCategoryAdd {
		valueDisplay = lipgloss.JoinHorizontal(lipgloss.Top,
			labelRendered,
			valueContentStyle.Render(valueStr),
		)
	} else {
		valueDisplay = lipgloss.JoinHorizontal(lipgloss.Top,
			labelRendered,
			valueContentStyle.Render(utils.TruncateTwoLines(valueStr, availableValueWidth)),
		)
	}
	valueDisplay = lipgloss.NewStyle().Width(innerWidth).MaxWidth(innerWidth).Render(valueDisplay)

	divider := lipgloss.NewStyle().Foreground(colors.Gray()).Render(strings.Repeat("\u2500", innerWidth))

	desc := meta.Description
	if meta.RequiresRestart {
		restartNotice := lipgloss.NewStyle().
			Foreground(colors.Orange()).
			Bold(true).
			Render("\u21ba Requires Restart")
		desc = restartNotice + "\n" + desc
	}

	wrappedDesc := utils.WrapText(desc, innerWidth)
	descDisplay := lipgloss.NewStyle().
		Foreground(colors.LightGray()).
		Width(innerWidth).
		MaxWidth(innerWidth).
		Render(wrappedDesc)

	titleStyle := lipgloss.NewStyle().Foreground(colors.Magenta()).Bold(true)
	titleDisplay := titleStyle.Width(innerWidth).MaxWidth(innerWidth).Render(meta.Label)

	detail := lipgloss.JoinVertical(lipgloss.Left,
		titleDisplay,
		"",
		valueDisplay,
		"",
		divider,
		"",
		descDisplay,
	)

	return formatSettingsBlock(detail, innerWidth, rows)
}

func calculateSettingsPaneWidths(modalWidth int) (leftWidth, rightWidth int) {
	leftWidth, rightWidth = CalculateTwoColumnWidths(modalWidth, 32, 22)
	innerWidthForModal := modalWidth - BoxStyle.GetHorizontalFrameSize()
	if leftWidth > 0 && innerWidthForModal > leftWidth+1 {
		rightWidth = innerWidthForModal - leftWidth - 1
	}
	return leftWidth, rightWidth
}

func (m RootModel) renderSettingsTwoColumn(settingsMeta []config.SettingMeta, selectedRow int, settingsValues map[string]interface{}, modalWidth, bodyHeight int) string {
	leftWidth, rightWidth := calculateSettingsPaneWidths(modalWidth)

	if leftWidth < 12 || rightWidth < 14 {
		return m.renderSettingsCompact(settingsMeta, selectedRow, settingsValues, modalWidth, bodyHeight)
	}

	// Account for both border and internal padding
	listRows := bodyHeight - BoxStyle.GetVerticalFrameSize() - InternalPaddingHeight
	if listRows < 1 {
		listRows = 1
	}
	listContent := m.renderSettingsListViewport(settingsMeta, selectedRow, listRows, leftWidth-(BoxStyle.GetHorizontalFrameSize()*2))
	listBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colors.Gray()).
		Width(leftWidth).
		Padding(1, 1).
		Render(listContent)

	if m.SettingsIsEditing {
		m.updateSettingsInputWidthForViewport()
	}

	rightBoxStyle := lipgloss.NewStyle().Width(rightWidth).Padding(1, 2)
	rightRows := bodyHeight - rightBoxStyle.GetVerticalFrameSize()
	if rightRows < 1 {
		rightRows = 1
	}
	rightContent := m.renderSettingsDetailBlock(settingsMeta, selectedRow, settingsValues, rightWidth-rightBoxStyle.GetHorizontalFrameSize(), rightRows)
	rightBox := rightBoxStyle.Render(rightContent)

	dividerHeight := max(lipgloss.Height(listBox), lipgloss.Height(rightBox))
	if dividerHeight < 1 {
		dividerHeight = 1
	}
	divider := lipgloss.NewStyle().Foreground(colors.Gray()).Render(strings.Repeat("\u2502\n", dividerHeight-1) + "\u2502")

	content := lipgloss.JoinHorizontal(lipgloss.Top, listBox, divider, rightBox)
	return formatSettingsBlock(content, modalWidth-BoxStyle.GetHorizontalFrameSize(), bodyHeight)
}

func (m RootModel) renderSettingsCompact(settingsMeta []config.SettingMeta, selectedRow int, settingsValues map[string]interface{}, modalWidth, bodyHeight int) string {
	innerWidth := modalWidth - BoxStyle.GetHorizontalFrameSize()
	if innerWidth < 1 {
		innerWidth = 1
	}

	if m.SettingsIsEditing {
		m.updateSettingsInputWidthForViewport()
	}

	listRows := bodyHeight / 2
	if listRows < 1 {
		listRows = 1
	}

	detailRows := bodyHeight - listRows - DividerHeight // line for the divider line
	if detailRows < 1 {
		detailRows = 1
		listRows = bodyHeight - detailRows
		if listRows < 1 {
			listRows = 1
		}
	}

	list := m.renderSettingsListViewport(settingsMeta, selectedRow, listRows, innerWidth)
	detail := m.renderSettingsDetailBlock(settingsMeta, selectedRow, settingsValues, innerWidth, detailRows)
	divider := lipgloss.NewStyle().Foreground(colors.Gray()).Render(strings.Repeat("\u2500", innerWidth))

	content := lipgloss.JoinVertical(lipgloss.Left,
		list,
		divider,
		detail,
	)

	return formatSettingsBlock(content, innerWidth, bodyHeight)
}

func (m *RootModel) normalizeSettingsSelection() {
	categories := config.CategoryOrder()
	if len(categories) == 0 {
		m.SettingsActiveTab = 0
		m.SettingsSelectedRow = 0
		if m.SettingsIsEditing {
			m.SettingsIsEditing = false
			m.SettingsInput.Blur()
		}
		return
	}

	if m.SettingsActiveTab < 0 {
		m.SettingsActiveTab = 0
	}
	if m.SettingsActiveTab >= len(categories) {
		m.SettingsActiveTab = len(categories) - 1
	}

	settingsList := m.buildSettingsMetaForCategory(categories[m.SettingsActiveTab])
	if len(settingsList) == 0 {
		m.SettingsSelectedRow = 0
		if m.SettingsIsEditing {
			m.SettingsIsEditing = false
			m.SettingsInput.Blur()
		}
		return
	}

	if m.SettingsSelectedRow < 0 {
		m.SettingsSelectedRow = 0
	}
	if m.SettingsSelectedRow >= len(settingsList) {
		m.SettingsSelectedRow = len(settingsList) - 1
	}
}

func (m *RootModel) updateSettingsInputWidthForViewport() {
	modalWidth, _ := GetSettingsDimensions(m.width, m.height)
	var targetWidth int
	var catInputWidth int
	if modalWidth >= 72 {
		_, rightWidth := calculateSettingsPaneWidths(modalWidth)

		targetWidth = rightWidth - 10   // Fixed offset for labels
		catInputWidth = rightWidth - 12 // rightWidth - 4 (padding) - 8 (label width)
	} else {
		targetWidth = modalWidth - 16   // Fixed offset for labels
		catInputWidth = modalWidth - 10 // modalWidth - 2 (padding) - 8 (label width)
	}

	if targetWidth < MinSettingsInputW {
		targetWidth = MinSettingsInputW
	}
	if targetWidth > MaxSettingsInputW {
		targetWidth = MaxSettingsInputW
	}

	if catInputWidth < MinSettingsInputW {
		catInputWidth = MinSettingsInputW
	}
	if catInputWidth > MaxSettingsInputW {
		catInputWidth = MaxSettingsInputW
	}

	m.SettingsInput.SetWidth(targetWidth)
	for i := range m.catMgrInputs {
		m.catMgrInputs[i].SetWidth(catInputWidth)
	}
}

// getSettingsValues returns a map of setting key -> value for a category
func (m RootModel) getSettingsValues(category string) map[string]interface{} {
	values := make(map[string]interface{})
	if m.Settings == nil {
		return values
	}

	for _, cat := range m.Settings.CategoriesList {
		if cat.Name == category {
			for _, set := range cat.Settings {
				values[set.Key] = set.Value
			}
			break
		}
	}

	if category == "Categories" {
		for i, cat := range m.Settings.Categories.Categories {
			values[fmt.Sprintf("category_%d", i)] = cat
		}
	}

	return values
}

// setSettingValue sets a setting value from string input
func (m *RootModel) setSettingValue(category, key, value string) error {
	setting := m.Settings.FindSetting(category, key)
	if setting == nil {
		return nil
	}

	// Special logic for Theme to trigger app re-rendering internally
	if key == "theme" {
		var theme int
		valLower := strings.ToLower(value)
		switch valLower {
		case "system", "adaptive", "0":
			theme = config.ThemeAdaptive
		case "light", "1":
			theme = config.ThemeLight
		case "dark", "2":
			theme = config.ThemeDark
		default:
			if v, err := strconv.Atoi(value); err == nil && v >= 0 && v <= 2 {
				theme = v
			} else {
				return nil // Invalid
			}
		}
		setting.Value = theme
		m.ApplyTheme(theme, config.Resolve[string](m.Settings.General.ThemePath))
		return nil
	}
	if key == "theme_path" {
		setting.Value = value
		// Re-apply the current theme mode but with the brand new path
		m.ApplyTheme(config.Resolve[int](m.Settings.General.Theme), value)
		return nil
	}

	// Generic Parsing and Application
	var parsedVal any

	switch setting.Type {
	case config.TypeBool:
		// Typically toggled unless explicitly typed out
		if value == "" {
			if key == "auto_start" {
				if m.ToggleServiceFunc == nil {
					return fmt.Errorf("service management is not available on this platform")
				}
				newVal := !config.Resolve[bool](setting)
				if err := m.ToggleServiceFunc(newVal); err != nil {
					return fmt.Errorf("failed to update service: %w", err)
				}
				parsedVal = newVal
			} else {
				parsedVal = !config.Resolve[bool](setting)
			}
		} else {
			b, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid boolean")
			}
			parsedVal = b
		}
	case config.TypeString, config.TypeAuthToken, config.TypeLink:
		if key == "global_rate_limit" || key == "default_download_rate_limit" {
			if _, err := strconv.ParseFloat(value, 64); err == nil {
				value += " MB/s"
			}
			if bps, err := utils.ParseRateLimitValue(value); err == nil {
				value = utils.FormatRateLimit(bps)
			}
		}
		parsedVal = value
	case "int":
		if key == "worker_buffer_size" {
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("invalid number")
			}
			parsedVal = int(v * float64(utils.KiB))
		} else {
			v, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid number")
			}
			parsedVal = v
		}
	case "int64":
		// Handle KB/MB scaling gracefully if specified
		if key == "min_chunk_size" {
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("invalid number")
			}
			parsedVal = int64(v * float64(utils.MiB))
		} else {
			v, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid number")
			}
			parsedVal = v
		}
	case config.TypeDuration:
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			value += "s"
		}
		v, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration")
		}
		parsedVal = v
	case config.TypeFloat64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid number")
		}
		parsedVal = v
	default:
		parsedVal = value
	}

	if err := setting.Validate(parsedVal); err != nil {
		return err
	}
	setting.Value = parsedVal
	if key == "auto_shutdown_after_downloads" {
		m.applyAutoShutdownSettingChange()
	}
	return nil
}

func (m *RootModel) persistSettings() error {
	if err := config.SaveSettings(m.Settings); err != nil {
		return err
	}
	if reloader, ok := m.Service.(interface {
		ReloadSettings(settings *config.Settings) error
	}); ok {
		if err := reloader.ReloadSettings(m.Settings); err != nil {
			return err
		}
	}
	if m.Orchestrator != nil {
		m.Orchestrator.ApplySettings(m.Settings)
	}
	return nil
}

// getCurrentSettingKey returns the key of the currently selected setting
func (m RootModel) getCurrentSettingKey() string {
	meta := m.getCurrentSettingMeta()
	if meta != nil {
		return meta.Key
	}
	return ""
}

func (m RootModel) getCurrentSettingMeta() *config.SettingMeta {
	categories := config.CategoryOrder()
	if m.SettingsActiveTab < 0 || m.SettingsActiveTab >= len(categories) {
		return nil
	}

	activeCategory := categories[m.SettingsActiveTab]
	settingsList := m.buildSettingsMetaForCategory(activeCategory)
	if m.SettingsSelectedRow < 0 || m.SettingsSelectedRow >= len(settingsList) {
		return nil
	}
	return &settingsList[m.SettingsSelectedRow]
}

// buildSettingsMetaForCategory retrieves and dynamically augments metadata
func (m RootModel) buildSettingsMetaForCategory(category string) []config.SettingMeta {
	settingsMap := config.GetSettingsMetadata()
	settingsList, ok := settingsMap[category]
	if !ok {
		return nil
	}

	if category == "Categories" {
		baseMeta := make([]config.SettingMeta, len(settingsList))
		copy(baseMeta, settingsList)
		settingsList = baseMeta
		if m.Settings != nil {
			for i, cat := range m.Settings.Categories.Categories {
				settingsList = append(settingsList, config.SettingMeta{
					Key:   fmt.Sprintf("category_%d", i),
					Label: fmt.Sprintf("[%s]", cat.Name),
					Type:  config.TypeCustomCategory,
				})
			}
		}
		settingsList = append(settingsList, config.SettingMeta{
			Key:   "add_category",
			Label: "+ Add Category...",
			Type:  config.TypeCustomCategoryAdd,
		})
	}

	return settingsList
}

// getCurrentSettingType returns the type of the currently selected setting
func (m RootModel) getCurrentSettingType() config.SettingType {
	meta := m.getCurrentSettingMeta()
	if meta != nil {
		return meta.Type
	}
	return config.TypeString
}

// getSettingsCount returns the number of settings in the current category
func (m RootModel) getSettingsCount() int {
	categories := config.CategoryOrder()
	if m.SettingsActiveTab >= 0 && m.SettingsActiveTab < len(categories) {
		activeCategory := categories[m.SettingsActiveTab]
		settingsList := m.buildSettingsMetaForCategory(activeCategory)
		return len(settingsList)
	}
	return 0
}

// getSettingUnit returns the unit suffix for the currently selected setting
func (m RootModel) getSettingUnit() string {
	key := m.getCurrentSettingKey()
	switch key {
	case "min_chunk_size":
		return " MB"
	case "worker_buffer_size":
		return " KB"
	case "dial_hedge_count":
		return " conns"
	case "max_task_retries":
		return " retries"
	case "slow_worker_grace_period", "stall_timeout":
		return " seconds"
	case "slow_worker_threshold", "speed_ema_alpha":
		return " (0.0-1.0)"
	case "global_rate_limit", "default_download_rate_limit":
		return " MB/s"
	default:
		return ""
	}
}

// formatSettingValueForEdit returns a plain value without units for editing
func formatSettingValueForEdit(value interface{}, typ config.SettingType, key string, truncate bool) string {
	switch key {
	case "min_chunk_size":
		if v, ok := asFloat64(value); ok {
			mb := v / float64(utils.MiB)
			return fmt.Sprintf("%.1f", mb)
		}
	case "worker_buffer_size":
		if v, ok := asFloat64(value); ok {
			kb := v / float64(utils.KiB)
			return fmt.Sprintf("%.0f", kb)
		}
	case "slow_worker_grace_period", "stall_timeout":
		if v, ok := asFloat64(value); ok {
			// Values might be duration or pure float64 depending on decode/init paths.
			// The settings parse logic handles both, but for UI string we want raw seconds.
			var secs float64
			if _, isDuration := value.(time.Duration); isDuration {
				secs = time.Duration(v).Seconds()
			} else {
				secs = v
			}
			return fmt.Sprintf("%.0f", secs)
		}
	case "global_rate_limit", "default_download_rate_limit":
		if vStr, ok := value.(string); ok && vStr != "" {
			if parsed, err := utils.ParseRateLimitValue(vStr); err == nil {
				if parsed == 0 {
					return "\u221E"
				}
				mb := float64(parsed) / 1000000.0
				if mb == float64(int64(mb)) {
					return fmt.Sprintf("%.0f", mb)
				}
				return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", mb), "0"), ".")
			}
		}
	}

	if key == "theme" {
		if v, ok := asFloat64(value); ok {
			switch int(v) {
			case config.ThemeAdaptive:
				return "< System >"
			case config.ThemeLight:
				return "< Light >"
			case config.ThemeDark:
				return "< Dark >"
			}
		}
	}

	// Default: use standard format
	return formatSettingValue(value, typ, truncate)
}

// formatSettingValue formats a setting value for display
func formatSettingValue(value interface{}, typ config.SettingType, truncate bool) string {
	if value == nil {
		return "-"
	}

	switch typ {
	case config.TypeBool:
		if b, ok := value.(bool); ok {
			if b {
				return "True"
			}
			return "False"
		}
		if v, ok := asFloat64(value); ok {
			if v != 0 {
				return "True"
			}
			return "False"
		}
	case config.TypeDuration:
		if d, ok := value.(time.Duration); ok {
			return d.String()
		}
		if s, ok := value.(string); ok {
			if parsed, err := time.ParseDuration(s); err == nil {
				return parsed.String()
			}
		}
		if v, ok := asFloat64(value); ok {
			return time.Duration(v).String()
		}
	case config.TypeInt64:
		if v, ok := asFloat64(value); ok {
			return fmt.Sprintf("%d", int64(v))
		}
	case config.TypeInt:
		if v, ok := asFloat64(value); ok {
			return fmt.Sprintf("%d", int(v))
		}
	case config.TypeFloat64:
		if v, ok := asFloat64(value); ok {
			return fmt.Sprintf("%.2f", v)
		}
	case config.TypeString, config.TypeLink:
		if s, ok := value.(string); ok {
			if s == "" {
				return "(default)"
			}
			if truncate {
				return utils.TruncateMiddle(s, 30)
			}
			return s
		}
	case config.TypeAuthToken:
		return "********"
	}

	// Fallback using reflection for numeric types
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Float64:
		return fmt.Sprintf("%.2f", v.Float())
	default:
		return fmt.Sprintf("%v", value)
	}
}

// resetSettingToDefault resets a specific setting to its default value
func (m *RootModel) resetSettingToDefault(category, key string, defaults *config.Settings) error {
	if key == "auto_start" {
		if m.ToggleServiceFunc != nil && config.Resolve[bool](m.Settings.General.AutoStart) != config.Resolve[bool](defaults.General.AutoStart) {
			if err := m.ToggleServiceFunc(config.Resolve[bool](defaults.General.AutoStart)); err != nil {
				return fmt.Errorf("failed to update service: %w", err)
			}
		}
	}

	setting := m.Settings.FindSetting(category, key)
	defaultSetting := defaults.FindSetting(category, key)
	if setting != nil && defaultSetting != nil {
		setting.Value = defaultSetting.Value
	}
	if key == "auto_shutdown_after_downloads" {
		m.applyAutoShutdownSettingChange()
	}
	return nil
}

func (m RootModel) renderCategoryInputLine(label string, fieldIndex int, width int) string {
	labelStyle := lipgloss.NewStyle().Foreground(colors.LightGray()).Width(8)
	if m.catMgrEditField == fieldIndex {
		labelStyle = labelStyle.Foreground(colors.Cyan()).Bold(true)
	}

	inputView := m.catMgrInputs[fieldIndex].View()
	inputWidth := width - 8
	if inputWidth < 5 {
		inputWidth = 5
	}
	// Force the input view to fit within the allowed width
	inputStyle := lipgloss.NewStyle().Width(inputWidth).MaxWidth(inputWidth)

	return lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render(label), inputStyle.Render(inputView))
}
