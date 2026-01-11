package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"surge/internal/downloader"
	"surge/internal/messages"
	"surge/internal/utils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

const progressChannelBuffer = 100

// runHeadless runs a download without TUI, printing progress to stderr
func runHeadless(ctx context.Context, url, outPath string, verbose bool, md5sum, sha256sum string) error {
	eventCh := make(chan tea.Msg, progressChannelBuffer)

	startTime := time.Now()
	var totalSize int64
	var lastProgress int64

	// Start download in background
	errCh := make(chan error, 1)
	go func() {
		err := downloader.Download(ctx, url, outPath, verbose, md5sum, sha256sum, eventCh, 1)
		errCh <- err
		close(eventCh)
	}()

	// Process events
	for msg := range eventCh {
		switch m := msg.(type) {
		case messages.DownloadStartedMsg:
			totalSize = m.Total
			fmt.Fprintf(os.Stderr, "Downloading: %s (%s)\n", m.Filename, utils.ConvertBytesToHumanReadable(totalSize))
		case messages.ProgressMsg:
			if totalSize > 0 {
				percent := m.Downloaded * 100 / totalSize
				lastPercent := lastProgress * 100 / totalSize
				if percent/10 > lastPercent/10 {
					speed := float64(m.Downloaded) / time.Since(startTime).Seconds() / (1024 * 1024)
					fmt.Fprintf(os.Stderr, "  %d%% (%s) - %.2f MB/s\n", percent,
						utils.ConvertBytesToHumanReadable(m.Downloaded), speed)
				}
				lastProgress = m.Downloaded
			}
		case messages.DownloadCompleteMsg:
			elapsed := time.Since(startTime)
			speed := float64(totalSize) / elapsed.Seconds() / (1024 * 1024)
			fmt.Fprintf(os.Stderr, "Complete: %s in %s (%.2f MB/s)\n",
				utils.ConvertBytesToHumanReadable(totalSize),
				elapsed.Round(time.Millisecond), speed)
		case messages.DownloadErrorMsg:
			return m.Err
		}
	}

	return <-errCh
}

// sendToServer sends a download request to a running surge server
func sendToServer(url, outPath string, port int) error {
	reqBody := DownloadRequest{
		URL:  url,
		Path: outPath,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d/download", port)
	resp, err := http.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error: %s - %s", resp.Status, string(body))
	}

	fmt.Printf("Download queued: %s\n", string(body))
	return nil
}

var getCmd = &cobra.Command{
	Use:   "get [url]",
	Short: "Download a file in headless mode or send to running server",
	Long: `Download a file from a URL without the TUI interface.

Use --headless for CLI-only downloads (useful for scripting).
Use --port to send the download to a running Surge instance.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := args[0]
		outPath, _ := cmd.Flags().GetString("output")
		verbose, _ := cmd.Flags().GetBool("verbose")
		md5sum, _ := cmd.Flags().GetString("md5")
		sha256sum, _ := cmd.Flags().GetString("sha256")
		port, _ := cmd.Flags().GetInt("port")

		if outPath == "" {
			outPath = "."
		}

		// Send to running server if port specified
		if port > 0 {
			if err := sendToServer(url, outPath, port); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Default: headless download
		ctx := context.Background()
		if err := runHeadless(ctx, url, outPath, verbose, md5sum, sha256sum); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	getCmd.Flags().StringP("output", "o", "", "output directory")
	getCmd.Flags().BoolP("verbose", "v", false, "verbose output")
	getCmd.Flags().String("md5", "", "MD5 checksum for verification")
	getCmd.Flags().String("sha256", "", "SHA256 checksum for verification")
	getCmd.Flags().IntP("port", "p", 0, "send to running surge server on this port")
}
