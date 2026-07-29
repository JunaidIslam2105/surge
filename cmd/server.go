package cmd

import (
	"fmt"

	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/utils"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server [url]...",
	Short: "Manage the Surge background server (daemon)",
	Long:  `Run the Surge background server in headless mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return serverStartCmd.RunE(cmd, args)
	},
}

var isSystemServiceFlag bool

var serverStartCmd = &cobra.Command{
	Use:   "start [url]...",
	Short: "Start the Surge server in headless mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		if isSystemServiceFlag {
			activeServerMode = "service"
		}

		if checkSystemServiceRunning() && !isSystemServiceFlag {
			// Backward compatibility for older systemd/runit service files that used `surge server start`
			// instead of `surge service __run`. In these cases, the service manager thinks the service
			// is starting/running because we ARE the service.
			// We verify if another instance is actually alive by checking the PID file.
			sysPid := readPIDFile(config.GetSystemRuntimeDir())
			isAlive := false
			if sysPid > 0 && sysPid != os.Getpid() {
				if p, err := os.FindProcess(sysPid); err == nil {
					// Sending signal 0 checks if the process exists
					if err := p.Signal(syscall.Signal(0)); err == nil {
						isAlive = true
					}
				}
			}

			// Also check environment variables that strongly suggest we are the service manager
			isServiceEnv := os.Getenv("INVOCATION_ID") != "" // systemd

			if isAlive && !isServiceEnv {
				return fmt.Errorf("system service is already running. Use 'surge connect' to interact with it, or stop the service first")
			}

			// We are likely being started by the service manager via legacy args, so act as the system service
			isSystemServiceFlag = true
			activeServerMode = "service"
		}

		// Attempt to acquire lock before any global state initialization
		isMaster, err := AcquireLock()
		if err != nil {
			return fmt.Errorf("error acquiring lock: %w", err)
		}

		if !isMaster {
			return fmt.Errorf("surge server is already running")
		}
		defer func() {
			if err := ReleaseLock(); err != nil {
				utils.Debug("Error releasing lock: %v", err)
			}
		}()

		if err := initializeGlobalState(); err != nil {
			return err
		}

		msg := runStartupIntegrityCheck()
		if msg != "" {
			utils.Debug("%s", msg)
			fmt.Println(msg)
		}

		portFlag, _ := cmd.Flags().GetInt("port")
		batchFile, _ := cmd.Flags().GetString("batch")
		outputDir, _ := cmd.Flags().GetString("output")
		exitWhenDone, _ := cmd.Flags().GetBool("exit-when-done")
		noResume, _ := cmd.Flags().GetBool("no-resume")

		// Save current PID to file
		savePID()
		defer removePID()

		// Get token flag
		tokenFlag := resolveServerToken(cmd)
		return startServerLogic(cmd, args, portFlag, batchFile, outputDir, exitWhenDone, noResume, tokenFlag)
	},
}

var serverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running Surge server",
	Run: func(cmd *cobra.Command, args []string) {
		pid := readPID()
		if pid == 0 {
			fmt.Println("No running Surge server found (PID file missing).")
			return
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Printf("Error finding process: %v\n", err)
			return
		}

		// Try to send SIGTERM
		err = process.Signal(syscall.SIGTERM)
		if err != nil {
			fmt.Printf("Error stopping server: %v\n", err)
			return
		}

		fmt.Printf("Sent stop signal to process %d\n", pid)
	},
}

var serverStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the Surge server",
	Run: func(cmd *cobra.Command, args []string) {
		pid := readPID()
		if pid == 0 {
			fmt.Println("Surge server is NOT running.")
			return
		}

		// Check if process exists
		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Printf("Surge server is NOT running (Process %d not found).\n", pid)
			// Cleanup stale pid file?
			return
		}

		// Sending signal 0 to check existence
		err = process.Signal(syscall.Signal(0))
		if err != nil {
			fmt.Printf("Surge server is NOT running (Process %d dead).\n", pid)
			return
		}

		port := readActivePort()
		fmt.Printf("Surge server is running (PID: %d, Port: %d).\n", pid, port)
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(serverStartCmd)
	serverCmd.AddCommand(serverStopCmd)
	serverCmd.AddCommand(serverStatusCmd)

	serverCmd.PersistentFlags().StringP("batch", "b", "", "File containing URLs to download")
	serverCmd.PersistentFlags().IntP("port", "p", 0, "Port to listen on")
	serverCmd.PersistentFlags().StringP("output", "o", "", "Output directory (defaults to current working directory)")
	serverCmd.PersistentFlags().Bool("exit-when-done", false, "Exit when all downloads complete")
	serverCmd.PersistentFlags().Bool("no-resume", false, "Do not auto-resume paused downloads on startup")
	serverCmd.PersistentFlags().String("token", "", "Auth token for API clients (or set SURGE_TOKEN)")

	serverStartCmd.Flags().BoolVar(&isSystemServiceFlag, "is-system-service", false, "Internal flag for service manager")
	_ = serverStartCmd.Flags().MarkHidden("is-system-service")
}

func savePID() {
	pid := os.Getpid()
	if err := os.MkdirAll(resolveRuntimeDir(), 0o755); err != nil {
		utils.Debug("Error creating runtime directory for PID file: %v", err)
		return
	}
	pidFile := filepath.Join(resolveRuntimeDir(), "pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0o644); err != nil {
		utils.Debug("Error writing PID file: %v", err)
	}
}

func removePID() {
	pidFile := filepath.Join(resolveRuntimeDir(), "pid")
	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		utils.Debug("Error removing PID file: %v", err)
	}
}

func readPID() int {
	pidFile := filepath.Join(resolveRuntimeDir(), "pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(string(data))
	return pid
}

func startServerLogic(cmd *cobra.Command, args []string, portFlag int, batchFile string, outputDir string, exitWhenDone bool, noResume bool, tokenOverride string) error {
	port, listener, err := bindServerListener(portFlag)
	if err != nil {
		return err
	}
	resetGlobalEnqueueContext()

	if err := ensureGlobalLocalServiceAndLifecycle(); err != nil {
		return fmt.Errorf("error creating lifecycle event stream: %w", err)
	}

	saveActivePort(port)
	defer removeActivePort()

	go startHTTPServer(listener, port, outputDir, GlobalService, strings.TrimSpace(tokenOverride))

	queueInitialRootDownloads(args, rootRunOptions{
		batchFile: batchFile,
		outputDir: outputDir,
	})

	fmt.Printf("Surge %s running in server mode.\n", Version)
	host := serverBindHost
	fmt.Printf("Serving on %s:%d\n", host, port)
	fmt.Println("Press Ctrl+C to exit.")

	StartHeadlessConsumer(cmd.Root().Context(), GlobalService)

	// Auto-resume paused downloads (unless --no-resume)
	if !noResume {
		resumePausedDownloads()
	}

	if exitWhenDone {
		exitWhenDoneCh := make(chan struct{}, 1)
		go func() {
			time.Sleep(2 * time.Second)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if atomic.LoadInt32(&pendingEnqueue) == 0 && atomic.LoadInt32(&activeDownloads) == 0 {
					if GlobalPool != nil && GlobalPool.ActiveCount() == 0 {
						select {
						case exitWhenDoneCh <- struct{}{}:
						default:
						}
						return
					}
				}
			}
		}()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer signal.Stop(sigChan)

		select {
		case sig := <-sigChan:
			fmt.Printf("\nReceived %s. Shutting down...\n", sig)
			_ = executeGlobalShutdown(fmt.Sprintf("server signal: %s", sig))
		case <-cmd.Root().Context().Done():
			fmt.Printf("\nService stop requested. Shutting down...\n")
			_ = executeGlobalShutdown("server: service context cancelled")
		case <-exitWhenDoneCh:
			fmt.Println("All downloads finished. Exiting...")
			_ = executeGlobalShutdown("server: exit when done")
		}
		return nil
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigChan)

	select {
	case sig := <-sigChan:
		fmt.Printf("\nReceived %s. Shutting down...\n", sig)
		_ = executeGlobalShutdown(fmt.Sprintf("server signal: %s", sig))
	case <-cmd.Root().Context().Done():
		fmt.Printf("\nService stop requested. Shutting down...\n")
		_ = executeGlobalShutdown("server: service context cancelled")
	}
	return nil
}

func resolveServerToken(cmd *cobra.Command) string {
	var tokenFlag string
	if f := cmd.Flag("token"); f != nil {
		tokenFlag = f.Value.String()
	}
	if token := strings.TrimSpace(tokenFlag); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("SURGE_TOKEN"))
}
