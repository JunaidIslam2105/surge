package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/scheduler"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/tui"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// Version information - set via ldflags during build
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		// Override with build info if ldflags didn't inject a version.
		if (Version == "dev" || Version == "") && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = strings.TrimPrefix(info.Main.Version, "v")
		}

		if Commit == "" || Commit == "unknown" {
			if rev := buildInfoSetting(info.Settings, "vcs.revision"); rev != "" {
				Commit = rev
			}
		}
	}
}

func buildInfoSetting(settings []debug.BuildSetting, key string) string {
	for _, setting := range settings {
		if setting.Key == key {
			return strings.TrimSpace(setting.Value)
		}
	}
	return ""
}

// activeDownloads tracks in-flight downloads for headless/server exit logic.
var activeDownloads int32

// pendingEnqueue tracks the number of pending batch enqueues to avoid premature exit
var pendingEnqueue int32

var (
	globalHost  string
	globalToken string
)

// Globals for Unified Backend
var (
	GlobalPool              *scheduler.Scheduler
	GlobalProgressCh        chan types.DownloadEvent
	GlobalService           service.DownloadService
	GlobalLifecycleCleanup  func()
	serverProgram           *tea.Program
	startupIntegrityMessage string
	globalSettings          *config.Settings
	GlobalLifecycle         *orchestrator.LifecycleManager
	globalLifecycleMu       sync.Mutex
	globalEnqueueCtx        context.Context
	globalEnqueueCancel     context.CancelFunc
	globalEnqueueMu         sync.Mutex
)

// buildActiveDownloadChecker bridges the lifecycle manager and the worker pool.
// LifecycleManager has no direct reference to the pool, so we inject this closure
// at construction time to let it detect file-name collisions with in-flight downloads.
func buildActiveDownloadChecker(getAll func() []types.DownloadRecord) orchestrator.IsNameActiveFunc {
	if getAll == nil {
		return nil
	}

	return func(dir, name string) bool {
		dir = utils.EnsureAbsPath(strings.TrimSpace(dir))
		name = strings.TrimSpace(name)
		if dir == "" || name == "" {
			return false
		}

		for _, cfg := range getAll() {
			existingName := strings.TrimSpace(cfg.Filename)
			existingDir := strings.TrimSpace(cfg.OutputPath)
			if cfg.DestPath != "" {
				existingDir = filepath.Dir(cfg.DestPath)
				if existingName == "" {
					existingName = filepath.Base(cfg.DestPath)
				}
			}
			if ps := progress.CfgProgress(&cfg); ps != nil {
				if stateName := strings.TrimSpace(ps.GetFilename()); stateName != "" {
					existingName = stateName
				}
				if stateDestPath := strings.TrimSpace(ps.GetDestPath()); stateDestPath != "" {
					existingDir = filepath.Dir(stateDestPath)
					if existingName == "" {
						existingName = filepath.Base(stateDestPath)
					}
				}
			}
			if existingDir == "" || existingName == "" {
				continue
			}
			if utils.EnsureAbsPath(existingDir) == dir && existingName == name {
				return true
			}
		}
		return false
	}
}

func newLocalLifecycleManager(pool *scheduler.Scheduler, eventBus *orchestrator.EventBus, settings *config.Settings, getAll func() []types.DownloadRecord) *orchestrator.LifecycleManager {
	return orchestrator.NewLifecycleManager(pool, eventBus, settings, buildActiveDownloadChecker(getAll))
}

func startLifecycleEventWorker(service service.DownloadService, mgr *orchestrator.LifecycleManager) (func(), error) {
	if service == nil || mgr == nil {
		return nil, nil
	}

	managerStream, managerCleanup, err := service.StreamEvents(context.Background())
	if err != nil {
		return nil, err
	}
	go mgr.StartEventWorker(managerStream)
	return managerCleanup, nil
}

func currentLifecycle() *orchestrator.LifecycleManager {
	globalLifecycleMu.Lock()
	defer globalLifecycleMu.Unlock()
	return GlobalLifecycle
}

func resetGlobalEnqueueContext() {
	globalEnqueueMu.Lock()
	defer globalEnqueueMu.Unlock()
	if globalEnqueueCancel != nil {
		globalEnqueueCancel()
	}
	globalEnqueueCtx, globalEnqueueCancel = context.WithCancel(context.Background())
}

func ensureEnqueueContextLocked() {
	if globalEnqueueCtx == nil || globalEnqueueCancel == nil {
		globalEnqueueCtx, globalEnqueueCancel = context.WithCancel(context.Background())
	}
}

func currentEnqueueContext() context.Context {
	globalEnqueueMu.Lock()
	defer globalEnqueueMu.Unlock()
	ensureEnqueueContextLocked()
	return globalEnqueueCtx
}

func currentEnqueueCancel() context.CancelFunc {
	globalEnqueueMu.Lock()
	defer globalEnqueueMu.Unlock()
	ensureEnqueueContextLocked()
	return globalEnqueueCancel
}

func cancelGlobalEnqueue() {
	globalEnqueueMu.Lock()
	cancel := globalEnqueueCancel
	globalEnqueueMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func takeLifecycleCleanup() func() {
	globalLifecycleMu.Lock()
	defer globalLifecycleMu.Unlock()
	cleanup := GlobalLifecycleCleanup
	GlobalLifecycleCleanup = nil
	return cleanup
}

func setLifecycleCleanupForTest(fn func()) {
	globalLifecycleMu.Lock()
	GlobalLifecycleCleanup = fn
	globalLifecycleMu.Unlock()
}

func currentPoolConfigs() []types.DownloadRecord {
	if GlobalPool == nil {
		return nil
	}
	return GlobalPool.GetAll()
}

func lifecycleForLocalService(service service.DownloadService) (*orchestrator.LifecycleManager, error) {
	lifecycle := currentLifecycle()
	if service == nil || GlobalService == nil || service != GlobalService {
		return lifecycle, nil
	}
	return ensureLocalLifecycle(GlobalService, currentPoolConfigs)
}

func ensureGlobalLocalServiceAndLifecycle() error {
	if GlobalService == nil {
		eventBus := orchestrator.NewEventBus()
		lifecycle := newLocalLifecycleManager(GlobalPool, eventBus, globalSettings, currentPoolConfigs)
		localService := service.NewLocalDownloadService(lifecycle)

		globalLifecycleMu.Lock()
		GlobalLifecycle = lifecycle
		GlobalService = localService
		globalLifecycleMu.Unlock()

		cleanup, err := startLifecycleEventWorker(localService, lifecycle)
		if err != nil {
			return err
		}
		globalLifecycleMu.Lock()
		GlobalLifecycleCleanup = cleanup
		globalLifecycleMu.Unlock()
	} else {
		_, err := ensureLocalLifecycle(GlobalService, currentPoolConfigs)
		return err
	}
	return nil
}

func publishSystemLog(message string) {
	if GlobalService != nil {
		_ = GlobalService.Publish(types.DownloadEvent{
			Type: types.EventSystem, Message: message})
		return
	}
	fmt.Fprintln(os.Stderr, message)
}

func recordPreflightDownloadError(url, outPath string, err error) {
	if err == nil || strings.TrimSpace(url) == "" {
		return
	}

	filename := strings.TrimSpace(orchestrator.InferFilenameFromURL(url))
	destPath := ""
	if filename != "" && strings.TrimSpace(outPath) != "" {
		destPath = filepath.Join(outPath, filename)
	}

	entry := types.DownloadRecord{
		ID:       uuid.New().String(),
		URL:      url,
		URLHash:  store.URLHash(url),
		DestPath: destPath,
		Filename: filename,
		Status:   "error",
	}
	if addErr := store.AddToMasterList(entry); addErr != nil {
		utils.Debug("Failed to persist preflight download error for %s: %v", url, addErr)
	}
	if GlobalService != nil {
		_ = GlobalService.Publish(types.DownloadEvent{
			Type:       types.EventError,
			DownloadID: entry.ID,
			Filename:   filename,
			DestPath:   destPath,
			Err:        err,
		})
	}
}

func ensureLocalLifecycle(service service.DownloadService, getAll func() []types.DownloadRecord) (*orchestrator.LifecycleManager, error) {
	globalLifecycleMu.Lock()
	defer globalLifecycleMu.Unlock()

	if GlobalLifecycle == nil {
		eventBus := orchestrator.NewEventBus()
		GlobalLifecycle = newLocalLifecycleManager(GlobalPool, eventBus, globalSettings, getAll)
	}
	if GlobalLifecycleCleanup == nil {
		cleanup, err := startLifecycleEventWorker(service, GlobalLifecycle)
		if err != nil {
			return nil, err
		}
		GlobalLifecycleCleanup = cleanup
	}
	return GlobalLifecycle, nil
}

func isExplicitOutputPath(outPath, defaultDir string) bool {
	return utils.EnsureAbsPath(strings.TrimSpace(outPath)) != utils.EnsureAbsPath(strings.TrimSpace(defaultDir))
}

type rootRunOptions struct {
	portFlag     int
	portSet      bool
	batchFile    string
	outputDir    string
	noResume     bool
	exitWhenDone bool
	noServer     bool
}

func readRootRunOptions(cmd *cobra.Command) rootRunOptions {
	portFlag, _ := cmd.Flags().GetInt("port")
	batchFile, _ := cmd.Flags().GetString("batch")
	outputDir, _ := cmd.Flags().GetString("output")
	noResume, _ := cmd.Flags().GetBool("no-resume")
	exitWhenDone, _ := cmd.Flags().GetBool("exit-when-done")
	noServer, _ := cmd.Flags().GetBool("no-server")

	return rootRunOptions{
		portFlag:     portFlag,
		portSet:      cmd.Flags().Changed("port"),
		batchFile:    batchFile,
		outputDir:    outputDir,
		noResume:     noResume,
		exitWhenDone: exitWhenDone,
		noServer:     noServer,
	}
}

func maybeRunRemoteTUI(cmd *cobra.Command, args []string) (bool, error) {
	hostTarget := resolveHostTarget()
	if hostTarget == "" {
		return false, nil
	}

	if len(args) > 0 {
		return false, fmt.Errorf("URLs cannot be passed when using --host. Use 'surge add <url>' after connecting")
	}

	if err := connectAndRunTUI(cmd, hostTarget); err != nil {
		return false, err
	}
	return true, nil
}

func acquireRootInstanceLock() (func(), error) {
	isMaster, err := AcquireLock()
	if err != nil {
		return nil, fmt.Errorf("error acquiring lock: %w", err)
	}

	if !isMaster {
		return nil, fmt.Errorf("surge is already running. Use 'surge add <url>' to add a download to the active instance")
	}

	return func() {
		if err := ReleaseLock(); err != nil {
			utils.Debug("Error releasing lock: %v", err)
		}
	}, nil
}

func initializeRootLocalRuntime() error {
	if err := initializeGlobalState(); err != nil {
		return err
	}
	resetGlobalEnqueueContext()

	startupIntegrityMessage = runStartupIntegrityCheck()

	if err := ensureGlobalLocalServiceAndLifecycle(); err != nil {
		return fmt.Errorf("error creating lifecycle event stream: %w", err)
	}

	return nil
}

func startRootHTTPServer(opts rootRunOptions) (int, func(), error) {
	port, listener, err := bindServerListener(opts.portFlag)
	if err != nil {
		return 0, nil, err
	}

	saveActivePort(port)
	go startHTTPServer(listener, port, opts.outputDir, GlobalService, "")

	return port, func() {
		removeActivePort()
	}, nil
}

func maybeStartRootHTTPServer(opts rootRunOptions) (int, func(), error) {
	if opts.noServer {
		if opts.portSet {
			return 0, nil, fmt.Errorf("--port cannot be used with --no-server")
		}
		return 0, func() {}, nil
	}
	return startRootHTTPServer(opts)
}

func queueInitialRootDownloads(args []string, opts rootRunOptions) {
	atomic.AddInt32(&pendingEnqueue, 1)
	go func() {
		defer atomic.AddInt32(&pendingEnqueue, -1)
		var urls []string
		urls = append(urls, args...)

		if opts.batchFile != "" {
			fileURLs, err := utils.ReadURLsFromFile(opts.batchFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading batch file: %v\n", err)
			} else {
				urls = append(urls, fileURLs...)
			}
		}

		if len(urls) > 0 {
			resolvedOutputDir := resolveClientOutputPath(opts.outputDir)
			processDownloads(urls, resolvedOutputDir, 0) // 0 port = internal direct add
		}
	}()
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:           "surge [url]...",
	Short:         "Blazing fast TUI download manager built in Go for power users",
	Long:          `Surge is a blazing fast TUI download manager built in Go for power users. Find more info here: https://github.com/SurgeDM/Surge`,
	Version:       Version,
	Args:          cobra.ArbitraryArgs,
	SilenceErrors: true, //errors are printed in main.go this prevents double printing
	SilenceUsage:  true, // prevent usage text from being printed on every error
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if reset, _ := cmd.Flags().GetBool("reset-settings"); reset {
			err1 := utils.RemoveFile(config.GetSettingsPath())
			err2 := utils.RemoveFile(config.GetKeyMapConfigPath())
			if err1 != nil || err2 != nil {
				fmt.Printf("Error resetting settings: %v, %v\n", err1, err2)
			} else {
				fmt.Println("Settings and keybindings have been reset to defaults.")
			}
		}

		if GlobalService != nil {
			_ = GlobalService.Shutdown()
			globalLifecycleMu.Lock()
			GlobalService = nil
			GlobalLifecycle = nil
			globalLifecycleMu.Unlock()
		}
		if GlobalPool != nil {
			GlobalPool.GracefulShutdown()
		}

		if cleanup := takeLifecycleCleanup(); cleanup != nil {
			cleanup()
		}
		globalHTTPServerMu.Lock()
		srv := globalHTTPServer
		globalHTTPServer = nil
		globalHTTPServerMu.Unlock()
		if srv != nil {
			_ = srv.Close()
		}

		GlobalProgressCh = make(chan types.DownloadEvent, 100)
		globalSettings = getSettings()
		globalSettings.Network.TLSCAFile = globalTLSCAFile
		globalSettings.Network.TLSInsecure = globalInsecureTLS
		GlobalPool = scheduler.New(GlobalProgressCh, config.Resolve[int](globalSettings.Network.MaxConcurrentDownloads))
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if ranRemote, err := maybeRunRemoteTUI(cmd, args); err != nil {
			return err
		} else if ranRemote {
			return nil
		}

		if checkSystemServiceRunning() {
			return fmt.Errorf("system service is already running. Use 'surge connect' to interact with it, or stop the service first")
		}

		if len(args) > 0 {
			var validFound bool
			for _, arg := range args {
				urlArg, _ := ParseURLArg(arg)
				if urlArg == "" {
					continue
				}
				if _, err := ValidateAndNormalizeURL(urlArg); err != nil {
					cmd.PrintErrf("Warning: skipping invalid URL %q: %v\n", urlArg, err)
					continue
				}
				validFound = true
			}
			if !validFound {
				return fmt.Errorf("no valid URLs provided in arguments")
			}
		}

		releaseLock, err := acquireRootInstanceLock()
		if err != nil {
			return err
		}
		defer releaseLock()

		savePID()
		defer removePID()

		if err := initializeRootLocalRuntime(); err != nil {
			return err
		}

		opts := readRootRunOptions(cmd)
		port, cleanup, err := maybeStartRootHTTPServer(opts)
		if err != nil {
			return err
		}
		defer cleanup()

		queueInitialRootDownloads(args, opts)
		return startTUI(port, opts.exitWhenDone, opts.noResume)
	},
}

// startTUI initializes and runs the TUI program
func startTUI(port int, exitWhenDone bool, noResume bool) error {
	tui.InitializeTUI()
	// Initialize TUI
	// GlobalService and GlobalProgressCh are already initialized in PersistentPreRun or Run

	m := tui.InitialRootModel(port, Version, GlobalService, currentLifecycle(), globalSettings, noResume, Commit)
	m = m.WithEnqueueContext(currentEnqueueContext(), currentEnqueueCancel())

	configureServiceUI(&m)

	m.ServerHost = serverBindHost
	if m.ServerHost == "" {
		m.ServerHost = "127.0.0.1"
	}
	m.IsRemote = false

	p := tea.NewProgram(m)
	serverProgram = p // Save reference for HTTP handler

	// Get event stream from service
	stream, cleanup, err := GlobalService.StreamEvents(context.Background())
	if err != nil {
		_ = executeGlobalShutdown("tui: stream init failed")
		return fmt.Errorf("error getting event stream: %w", err)
	}
	defer cleanup()

	// Background listener for progress events
	go func() {
		for msg := range stream {
			p.Send(msg)
		}
	}()

	if startupIntegrityMessage != "" && GlobalService != nil {
		_ = GlobalService.Publish(types.DownloadEvent{
			Type:    types.EventSystem,
			Message: startupIntegrityMessage,
		})
		startupIntegrityMessage = ""
	}

	// Exit-when-done checker for TUI
	if exitWhenDone {
		go func() {
			// Wait a bit for initial downloads to be queued
			time.Sleep(3 * time.Second)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if atomic.LoadInt32(&pendingEnqueue) == 0 && GlobalPool != nil && GlobalPool.ActiveCount() == 0 {
					// Send quit message to TUI
					p.Send(tea.Quit())
					return
				}
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigChan)

	stopSignalListener := make(chan struct{})
	defer close(stopSignalListener)

	go func() {
		select {
		case sig := <-sigChan:
			_ = executeGlobalShutdown(fmt.Sprintf("tui signal: %s", sig))
			p.Send(tea.Quit())
		case <-stopSignalListener:
			return
		}
	}()

	// Run TUI
	finalModel, err := p.Run()
	if err != nil {
		_ = executeGlobalShutdown("tui: p.Run failed")
		return fmt.Errorf("error running program: %w", err)
	}
	_ = executeGlobalShutdown("tui: program exited")

	// Check if restart was requested (e.g. from settings changed)
	if m, ok := finalModel.(tui.RootModel); ok && m.RestartRequested {
		return performRestart()
	}

	return nil
}

func performRestart() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %w", err)
	}

	if runtime.GOOS == "windows" {
		_ = ReleaseLock()
	}

	return utils.Run(executable, os.Args, os.Environ())
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalHost, "host", "", "Server host to connect/control (or set SURGE_HOST), e.g. 127.0.0.1:1700")
	rootCmd.PersistentFlags().StringVar(&globalToken, "token", "", "Bearer token (or set SURGE_TOKEN)")
	rootCmd.PersistentFlags().BoolVar(&globalInsecureHTTP, "insecure-http", false, "Allow plain HTTP for non-loopback remote targets")
	rootCmd.PersistentFlags().BoolVar(&globalInsecureTLS, "insecure-tls", false, "Skip TLS certificate verification for remote targets")
	rootCmd.PersistentFlags().StringVar(&globalTLSCAFile, "tls-ca-file", "", "PEM bundle to trust for remote HTTPS targets")
	rootCmd.Flags().StringP("batch", "b", "", "File containing URLs to download (one per line)")
	rootCmd.Flags().IntP("port", "p", 0, "Port to listen on (default: 8080 or first available)")
	rootCmd.Flags().StringP("output", "o", "", "Output directory (defaults to current working directory)")
	rootCmd.Flags().Bool("no-resume", false, "Do not auto-resume paused downloads on startup")
	rootCmd.Flags().Bool("exit-when-done", false, "Exit when all downloads complete")
	rootCmd.Flags().Bool("no-server", false, "Do not start the HTTP API server (CLI subcommands will not work)")
	rootCmd.Flags().Bool("reset-settings", false, "Reset settings and keybindings to defaults on startup")
	rootCmd.SetVersionTemplate("Surge v{{.Version}}\n")
	rootCmd.Version = Version
}
