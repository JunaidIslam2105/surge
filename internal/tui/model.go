package tui

import (
	"github.com/SurgeDM/Surge/internal/orchestrator"
	engineprogress "github.com/SurgeDM/Surge/internal/progress"

	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/power"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/tui/colors"
	"github.com/SurgeDM/Surge/internal/tui/components"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/version"
)

// InitializeTUI prepares global TUI state like styles and component caches.
// This should be called exactly once before any TUI elements are rendered.
func InitializeTUI() {
	InitializeStyles()
	components.InitializeStatusCache()
}

// IsTestMode is set by tests to avoid blocking calls to terminal interrogation
var IsTestMode bool

type UIState int // Defines UIState as int to be used in rootModel

const (
	DashboardState UIState = iota
	InputState
	DetailState
	FilePickerState
	DuplicateWarningState
	SearchState
	SettingsState
	ExtensionConfirmationState
	BatchFilePickerState
	BatchConfirmState
	UpdateAvailableState
	URLUpdateState
	QuitConfirmState
	RestartConfirmState
	HelpModalState
	BugReportTargetState
	BugReportSystemDetailsState
	BugReportLogPathState
	SpeedLimitsState
	PurgeConfirmState
	RemoveConfirmState
)

type FilePickerOrigin int

const (
	FilePickerOriginNone FilePickerOrigin = iota
	FilePickerOriginAdd
	FilePickerOriginSettings
	FilePickerOriginExtension
	FilePickerOriginTheme
	FilePickerOriginCategory
)

const (
	TabQueued = 0
	TabActive = 1
	TabDone   = 2
)

type DownloadModel struct {
	ID            string
	URL           string
	Filename      string
	FilenameLower string
	Destination   string // Full path to the destination file
	Total         int64
	Downloaded    int64
	Speed         float64
	Connections   int
	RateLimit     int64 // Speed limit in bytes/sec
	RateLimitSet  bool  // Whether RateLimit is an explicit per-download override

	StartTime time.Time
	Elapsed   time.Duration
	lastETA   time.Duration // EMA-smoothed ETA for UI stability

	progress progress.Model

	// Unified architecture: View Model updated by events
	// No direct state access or polling reporter
	state *engineprogress.DownloadProgress // Keep for now if needed for details view, but mostly passive

	done        bool
	started     bool // Engine has confirmed start
	err         error
	paused      bool
	pausing     bool // UI state: transitioning to pause
	resuming    bool // UI state: waiting for async resume
	rateLimited bool
}

type RootModel struct {
	downloads      []*DownloadModel
	width          int
	height         int
	state          UIState
	activeTab      int // 0=Queued, 1=Active, 2=Done
	pinnedTab      int // -1=None, 0=Queued, 1=Active, 2=Done
	inputs         []textinput.Model
	focusedInput   int
	purgeTargetID  string
	removeTargetID string
	// Service Interface
	// Core
	Service      service.DownloadService
	Orchestrator *orchestrator.LifecycleManager

	// File picker for directory selection
	filepicker             filepicker.Model
	filepickerOriginalPath string
	filepickerOrigin       FilePickerOrigin

	// Bubbles help component
	help help.Model

	// Bubbles list component for download listing
	list list.Model

	PWD string

	// Duplicate detection
	pendingURL           string // URL pending confirmation
	pendingPath          string // Path pending confirmation
	pendingIsDefaultPath bool
	pendingFilename      string   // Filename pending confirmation
	pendingMirrors       []string // Mirrors pending confirmation
	pendingHeaders       map[string]string
	pendingWorkers       int    // Per-task worker override pending confirmation
	pendingMinChunkSize  int64  // Per-task min chunk size override pending confirmation
	duplicateInfo        string // Info about the duplicate

	// Graph Data
	SpeedHistory           []float64 // Stores the last ~60 ticks of speed data
	lastSpeedHistoryUpdate time.Time // Last time SpeedHistory was updated (for 0.5s sampling)

	// Notification log system
	logViewport viewport.Model // Scrollable log viewport
	logEntries  []string       // Log entries for download events
	logFocused  bool           // Whether the log viewport is focused

	// Settings
	Settings              *config.Settings // Application settings
	SettingsBaseline      *config.Settings // Snapshot of settings when entering the settings view
	StartupConfigWarnings []string         // Config validation warnings to emit on first render
	SettingsActiveTab     int              // Active category tab (0-3)
	SettingsSelectedRow   int              // Selected setting within current tab
	SettingsFocusedPane   int              // Focused settings pane (0 = Tabs, 1 = List)
	SettingsIsEditing     bool             // Whether currently editing a value
	SettingsInput         textinput.Model  // Input for editing string/int values
	settingsError         string           // Current validation error in settings
	ExtensionTokenCopied  bool             // Flash message for "Token Copied!"

	// Speed Limits Modal
	speedLimitsCursor    int
	speedLimitsIsEditing bool
	speedLimitsError     string

	// Selection persistence
	SelectedDownloadID string // ID of the currently selected download
	ManualTabSwitch    bool   // Whether the last tab switch was manual

	// Search functionality
	searchInput  textinput.Model // Text input for search
	searchActive bool            // Whether search mode is active
	searchQuery  string          // Current search query

	// Batch import
	pendingBatchURLs         []string // URLs pending batch import
	pendingBatchRequests     []types.DownloadEvent
	pendingRequestQueue      []types.DownloadEvent
	pendingBatchRequestQueue []types.DownloadEvent
	batchFilePath            string // Path to the batch file

	// URL Refresh
	urlUpdateInput textinput.Model // Text input for updating URL

	// Category manager
	categoryFilter  string             // Dashboard filter ("" = all)
	catMgrEditField int                // 0=Name, 1=Description, 2=Pattern, 3=Path
	catMgrInputs    [4]textinput.Model // Inputs for Name, Description, Pattern, Path
	catMgrIsNew     bool               // Whether adding a new category
	// Quit confirm button focus (0 = Yep!, 1 = Nope)
	quitConfirmFocused int

	// Bug report flow context
	bugReportIncludeSystemInfo bool
	bugReportIncludeLatestLog  bool

	// Keybindings
	keys                *config.KeyMap
	lastKeyMapModTime   time.Time
	lastConfigCheckTime time.Time

	// Server port for display
	ServerPort int
	ServerHost string
	IsRemote   bool

	// Update check
	UpdateInfo     *version.UpdateInfo // Update information (nil if no update available)
	CurrentVersion string              // Current version of Surge
	CurrentCommit  string              // Current commit hash of Surge

	InitialDarkBackground bool // Captured at startup for "System" theme

	logoCache string // Cached logo with gradient applied

	enqueueCtx       context.Context
	cancelEnqueue    context.CancelFunc
	shuttingDown     bool
	RestartRequested bool // Flag to signal process re-exec after TUI shutdown

	autoShutdownArmed     bool
	autoShutdownTriggered bool
	autoShutdownRetrying  bool
	powerController       power.Controller
	powerInhibitorRelease power.ReleaseFunc

	ToggleServiceFunc func(bool) error

	spinner spinner.Model
}

// NewDownloadModel creates a new download model
func NewDownloadModel(id string, url string, filename string, total int64) *DownloadModel {
	// Create dummy state container for compatibility if needed
	state := engineprogress.New(id, total)
	return &DownloadModel{
		ID:            id,
		URL:           url,
		Filename:      filename,
		FilenameLower: strings.ToLower(filename),
		Total:         total,
		StartTime:     time.Now(),
		progress: progress.New(
			progress.WithSpringOptions(0.5, 0.1),
			progress.WithColors(colors.ProgressStart(), colors.ProgressEnd()),
			progress.WithScaled(true),
		),
		state: state,
	}
}

func InitialRootModel(serverPort int, currentVersion string, service service.DownloadService, orchestrator *orchestrator.LifecycleManager, settings *config.Settings, noResume bool, currentCommit ...string) RootModel {
	initialDarkBackground := true
	if !IsTestMode {
		initialDarkBackground = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	}
	commitValue := "unknown"
	if len(currentCommit) > 0 {
		if trimmed := strings.TrimSpace(currentCommit[0]); trimmed != "" {
			commitValue = trimmed
		}
	}

	if settings == nil {
		settings = config.DefaultSettings()
	}

	urlInput := textinput.New()
	urlInput.Placeholder = "https://example.com/file.zip"
	urlInput.Focus()
	urlInput.SetWidth(InputWidth)
	urlInput.Prompt = ""

	pathInput := textinput.New()
	pathInput.Placeholder = "."
	pathInput.SetWidth(InputWidth)
	pathInput.Prompt = ""
	pathInput.SetValue(".")

	filenameInput := textinput.New()
	filenameInput.Placeholder = "(auto-detect)"
	filenameInput.SetWidth(InputWidth)
	filenameInput.Prompt = ""

	mirrorsInput := textinput.New()
	mirrorsInput.Placeholder = "http://mirror1.com, http://mirror2.com"
	mirrorsInput.SetWidth(InputWidth)
	mirrorsInput.Prompt = ""

	pwd, _ := os.Getwd()

	homeDir, _ := os.UserHomeDir()
	downloadsDir := filepath.Join(homeDir, "Downloads")
	fp := filepicker.New()
	fp.CurrentDirectory = downloadsDir
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.ShowHidden = false
	fp.ShowSize = true
	fp.ShowPermissions = true
	fp.SetHeight(FilePickerHeight)
	applyFilepickerTheme(&fp)

	keys, errKeys := config.LoadKeyMap()
	if keys == nil {
		keys = config.DefaultKeyMap()
	}
	if errKeys != nil {
		keys.StartupWarnings = append(keys.StartupWarnings, fmt.Sprintf("Failed to load keymap: %v", errKeys))
	}

	var keyMapModTime time.Time
	if info, err := os.Stat(config.GetKeyMapConfigPath()); err == nil {
		keyMapModTime = info.ModTime()
	}

	// Capture any config warnings produced during load so Init() can surface
	// them in the activity log once the viewport is ready.
	var startupConfigWarnings []string
	if len(settings.StartupWarnings) > 0 {
		startupConfigWarnings = append(startupConfigWarnings, settings.StartupWarnings...)
	}
	if len(keys.StartupWarnings) > 0 {
		startupConfigWarnings = append(startupConfigWarnings, keys.StartupWarnings...)
	}

	// Override AutoResume if CLI flag provided
	if noResume {
		settings.General.AutoResume.Value = false
	}

	applyColorModeForTheme(config.Resolve[int](settings.General.Theme), config.Resolve[string](settings.General.ThemePath), initialDarkBackground)

	// Load paused downloads from master list (now uses global config directory)
	var downloads []*DownloadModel
	// Note: With Service abstraction, we might want to let the Service handle loading.
	// But LocalDownloadService's List() calls store.ListAllDownloads().
	// For TUI initialization, we should probably call Service.List() to populate the model.
	// However, Service.List() returns []DownloadStatus, which we need to convert to []*DownloadModel.

	// Let's use service.List() if available
	if service != nil {
		statuses, err := service.List()
		if err == nil {
			for _, s := range statuses {
				dm := NewDownloadModel(s.ID, s.URL, s.Filename, s.TotalSize)
				dm.Downloaded = s.Downloaded
				if s.DestPath != "" {
					dm.Destination = s.DestPath
				} else {
					dm.Destination = s.Filename // Fallback
				}
				// Status mapping
				switch s.Status {
				case "completed":
					dm.done = true
					dm.started = true
					dm.progress.SetPercent(1.0)
				case "error":
					dm.done = true
					dm.started = true
				case "pausing":
					dm.pausing = true
					dm.started = true
				case "paused":
					if config.Resolve[bool](settings.General.AutoResume) {
						dm.resuming = true
						dm.paused = true // Will update when resume event received
					} else {
						dm.paused = true
					}
					dm.started = true
				case "queued":
					// Always resume queued items
					dm.resuming = true
					dm.paused = true // Will update when resume event received
					dm.started = false
				case "downloading":
					dm.started = true
				}

				if s.TotalSize > 0 {
					dm.progress.SetPercent(s.Progress / 100.0)
				}
				if s.AvgSpeed > 0 {
					dm.Speed = s.AvgSpeed
				} else if s.Speed > 0 {
					dm.Speed = s.Speed
				}
				if s.Status == "completed" && s.TimeTaken > 0 {
					dm.Elapsed = time.Duration(s.TimeTaken) * time.Millisecond
				}
				dm.RateLimit = s.RateLimit
				dm.RateLimitSet = s.RateLimitSet

				downloads = append(downloads, dm)
			}
		}
	}

	// Initialize the download list
	downloadList := NewDownloadList(80, 20) // Default size, will be resized on WindowSizeMsg

	// Initialize help
	helpModel := help.New()
	helpModel.Styles.ShortKey = lipgloss.NewStyle().Foreground(colors.LightGray())
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().Foreground(colors.Gray())
	helpModel.Styles.FullKey = lipgloss.NewStyle().Foreground(colors.Pink())
	helpModel.Styles.FullDesc = lipgloss.NewStyle().Foreground(colors.LightGray())

	// Initialize settings input for editing
	settingsInput := textinput.New()
	settingsInput.SetWidth(40)
	settingsInput.Prompt = ""

	// Initialize search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Type to search..."
	searchInput.SetWidth(30)
	searchInput.Prompt = ""

	// Initialize URL update input
	urlUpdateInput := textinput.New()
	urlUpdateInput.Placeholder = "https://example.com/newlink.zip"
	urlUpdateInput.SetWidth(InputWidth)
	urlUpdateInput.Prompt = ""

	// Initialize Category Manager inputs
	catNameInput := textinput.New()
	catNameInput.Placeholder = "Videos"
	catNameInput.SetWidth(30)
	catNameInput.Prompt = ""

	catDescInput := textinput.New()
	catDescInput.Placeholder = "Video files (.mp4, .mkv)"
	catDescInput.SetWidth(50)
	catDescInput.Prompt = ""

	catPatternInput := textinput.New()
	catPatternInput.Placeholder = "(?i)\\.(mp4|mkv)$"
	catPatternInput.SetWidth(50)
	catPatternInput.Prompt = ""

	catPathInput := textinput.New()
	catPathInput.Placeholder = "/home/user/Videos"
	catPathInput.SetWidth(50)
	catPathInput.Prompt = ""

	enqueueCtx, cancelEnqueue := context.WithCancel(context.Background())

	// A single root-level spinner provides a shared animation frame for rendering,
	// avoiding the CPU and redraw overhead of independent per-item spinners on
	// large download lists.
	s := spinner.New()
	s.Spinner = spinner.MiniDot

	m := RootModel{
		downloads:             downloads,
		pinnedTab:             -1,
		inputs:                []textinput.Model{urlInput, mirrorsInput, pathInput, filenameInput},
		state:                 DashboardState,
		filepicker:            fp,
		help:                  helpModel,
		list:                  downloadList,
		Service:               service,
		Orchestrator:          orchestrator,
		PWD:                   pwd,
		Settings:              settings,
		StartupConfigWarnings: startupConfigWarnings,
		SettingsActiveTab:     0,
		SettingsSelectedRow:   0,
		SettingsFocusedPane:   1,
		SpeedHistory:          make([]float64, GraphHistoryPoints),                          // 60 points of history (30s at 0.5s interval)
		logViewport:           viewport.New(viewport.WithWidth(40), viewport.WithHeight(5)), // Default size, will be resized
		logEntries:            make([]string, 0),
		SettingsInput:         settingsInput,
		searchInput:           searchInput,
		urlUpdateInput:        urlUpdateInput,
		catMgrInputs:          [4]textinput.Model{catNameInput, catDescInput, catPatternInput, catPathInput},
		keys:                  keys,
		lastKeyMapModTime:     keyMapModTime,
		lastConfigCheckTime:   time.Now(),
		ServerPort:            serverPort,
		CurrentVersion:        currentVersion,
		CurrentCommit:         commitValue,
		InitialDarkBackground: initialDarkBackground,
		enqueueCtx:            enqueueCtx,
		cancelEnqueue:         cancelEnqueue,
		powerController:       power.NewController(),
		spinner:               s,
	}

	InitAuthToken() // Cache auth token for TUI to avoid per-frame disk I/O

	m.refreshThemeCaches()

	return m
}

// WithEnqueueContext lets callers bind model-initiated probes to a process-level
// shutdown context instead of the model's default standalone context.
func (m RootModel) WithEnqueueContext(ctx context.Context, cancel context.CancelFunc) RootModel {
	if ctx == nil {
		ctx = context.Background()
	}
	if cancel == nil {
		cancel = func() {}
	}
	m.enqueueCtx = ctx
	m.cancelEnqueue = cancel
	return m
}

type ViewStats struct {
	ActiveCount     int
	QueuedCount     int
	DownloadedCount int
	TotalDownloaded int64
}

func (m RootModel) Init() tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, m.spinner.Tick)

	// Trigger update check if not disabled in settings
	if !config.Resolve[bool](m.Settings.General.SkipUpdateCheck) {
		cmds = append(cmds, checkForUpdateCmd(m.CurrentVersion))
	}

	// Async resume of downloads
	var resumeIDs []string
	for _, d := range m.downloads {
		if d.resuming {
			resumeIDs = append(resumeIDs, d.ID)
		}
	}

	if len(resumeIDs) > 0 && m.Service != nil {
		cmds = append(cmds, func() tea.Msg {
			errs := m.Service.ResumeBatch(resumeIDs)

			// Dispatch individual messages for UI updates
			batch := make([]tea.Cmd, 0, len(resumeIDs))
			for i, id := range resumeIDs {
				err := errs[i]
				// Capture for closure
				currentID := id
				currentErr := err
				batch = append(batch, func() tea.Msg {
					return resumeResultMsg{id: currentID, err: currentErr}
				})
			}
			return tea.Batch(batch...)()
		})
	}

	// Emit any config warnings from startup into the activity log
	if len(m.StartupConfigWarnings) > 0 {
		warnings := m.StartupConfigWarnings
		cmds = append(cmds, func() tea.Msg {
			return startupConfigWarningMsg(warnings)
		})
	}

	if m.isAutoShutdownEnabled() {
		cmds = append(cmds, func() tea.Msg {
			return autoShutdownCheckMsg{}
		})
	}

	return tea.Batch(cmds...)
}

// FindDownloadByID finds a download by its ID
func (m *RootModel) FindDownloadByID(id string) *DownloadModel {
	for _, d := range m.downloads {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// Helper to get downloads for the current tab
func (m RootModel) getFilteredDownloads() []*DownloadModel {
	var filtered []*DownloadModel
	searchLower := strings.ToLower(m.searchQuery)

	for _, d := range m.downloads {
		// Apply tab filter first
		switch m.activeTab {
		case TabQueued:
			// Queued includes paused downloads and anything not currently active or done
			if d.done || (!d.paused && !d.pausing && (d.Speed > 0 || d.Connections > 0 || d.resuming || d.started)) {
				continue
			}
		case TabActive:
			// Active excludes paused downloads and anything without current activity
			if d.done || d.paused || d.pausing || (d.Speed == 0 && d.Connections == 0 && !d.resuming && !d.started) {
				continue
			}
		case TabDone:
			if !d.done {
				continue
			}
		}

		// Apply dashboard category filter.
		if m.categoryFilter != "" && m.Settings != nil && config.Resolve[bool](m.Settings.Categories.CategoryEnabled) {
			if !m.matchesCategoryFilter(d) {
				continue
			}
		}

		// Apply search filter if query is set
		if m.searchQuery != "" {
			if !strings.Contains(d.FilenameLower, searchLower) {
				continue
			}
		}

		filtered = append(filtered, d)
	}
	return filtered
}

func (m RootModel) matchesCategoryFilter(d *DownloadModel) bool {
	filter := m.categoryFilter
	if filter == "" {
		return true
	}

	filename := strings.TrimSpace(d.Filename)
	if filename == "" || filename == "Queued" {
		if d.Destination != "" {
			if destBase := strings.TrimSpace(filepath.Base(d.Destination)); strings.Contains(destBase, ".") {
				filename = destBase
			}
		}
	}
	if filename == "" || filename == "Queued" {
		filename = orchestrator.InferFilenameFromURL(d.URL)
	}

	cat, err := config.GetCategoryForFile(filename, m.Settings.Categories.Categories)
	if filter == "Uncategorized" {
		return err != nil || cat == nil
	}

	return err == nil && cat != nil && cat.Name == filter
}

// newFilepicker creates a fresh filepicker instance with consistent settings.
// This is necessary to avoid cursor desync issues that cause "index out of range"
// panics when navigating directories (especially on Windows).
// See: https://github.com/charmbracelet/bubbles/issues/864
func newFilepicker(currentDir string) filepicker.Model {
	fp := filepicker.New()
	fp.CurrentDirectory = currentDir
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.ShowHidden = false
	fp.ShowSize = true
	fp.ShowPermissions = true
	fp.SetHeight(FilePickerHeight)

	// Re-bind Select and Open to '.' per user preference.
	// We also keep 'right' for Open to allow directory navigation.
	fp.KeyMap.Select = key.NewBinding(key.WithKeys("."))
	fp.KeyMap.Open = key.NewBinding(key.WithKeys(".", "right"))
	// Keep ESC reserved for dismissing the modal; use left/backspace/h to go up.
	fp.KeyMap.Back = key.NewBinding(key.WithKeys("h", "backspace", "left"))

	applyFilepickerTheme(&fp)

	return fp
}

// ApplyTheme applies the selected theme mode
func (m *RootModel) ApplyTheme(mode int, path string) {
	applyColorModeForTheme(mode, path, m.InitialDarkBackground)
	m.refreshThemeCaches()
}

func applyColorModeForTheme(mode int, themePath string, initialDarkBackground bool) {
	isDark := initialDarkBackground

	switch mode {
	case config.ThemeLight:
		isDark = false
	case config.ThemeDark:
		isDark = true
	}

	colors.LoadTheme(themePath, isDark)
}

func (m *RootModel) refreshThemeCaches() {
	rebuildStyles()
	m.help.Styles.ShortKey = lipgloss.NewStyle().Foreground(colors.LightGray())
	m.help.Styles.ShortDesc = lipgloss.NewStyle().Foreground(colors.Gray())
	m.help.Styles.FullKey = lipgloss.NewStyle().Foreground(colors.Pink())
	m.help.Styles.FullDesc = lipgloss.NewStyle().Foreground(colors.LightGray())
	applyListTheme(&m.list)
	applyFilepickerTheme(&m.filepicker)
	m.logoCache = ""
	// Rebuild progress bar colors for all existing downloads so the gradient
	// matches the newly loaded palette rather than the one active at creation time.
	for _, d := range m.downloads {
		d.progress = progress.New(
			progress.WithSpringOptions(0.5, 0.1),
			progress.WithColors(colors.ProgressStart(), colors.ProgressEnd()),
			progress.WithScaled(true),
		)
	}
}

func applyFilepickerTheme(fp *filepicker.Model) {
	if fp == nil {
		return
	}

	fp.Styles.Cursor = lipgloss.NewStyle().Foreground(colors.Pink())
	fp.Styles.Symlink = lipgloss.NewStyle().Foreground(colors.Cyan())
	fp.Styles.Directory = lipgloss.NewStyle().Foreground(colors.Blue())
	fp.Styles.File = lipgloss.NewStyle().Foreground(colors.White())
	fp.Styles.DisabledFile = lipgloss.NewStyle().Foreground(colors.Gray())
	fp.Styles.Permission = lipgloss.NewStyle().Foreground(colors.Gray())
	fp.Styles.Selected = lipgloss.NewStyle().Foreground(colors.Pink()).Bold(true)
	fp.Styles.DisabledSelected = lipgloss.NewStyle().Foreground(colors.LightGray())
	fp.Styles.FileSize = lipgloss.NewStyle().Foreground(colors.Gray()).Width(7).Align(lipgloss.Right)
	fp.Styles.EmptyDirectory = lipgloss.NewStyle().Foreground(colors.Gray()).Padding(0, 2)
}
