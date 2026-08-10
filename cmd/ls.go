package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"text/tabwriter"
	"time"

	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:     "ls [id]",
	Aliases: []string{"l"},
	Short:   "List downloads",
	Long:    `List all downloads from the running server or database. Optionally show details for a specific download by ID.`,
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initializeGlobalState(); err != nil {
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		watch, _ := cmd.Flags().GetBool("watch")

		baseURL, token, err := resolveAPIConnection(false)
		if err != nil {
			return err
		}

		// If ID provided, show details for that download
		if len(args) == 1 {
			return showDownloadDetails(args[0], jsonOutput, baseURL, token)
		}

		strictRemote := resolveHostTarget() != ""

		if watch {
			for {
				// Clear screen first for watch mode
				fmt.Print("\033[H\033[2J")
				if err := printDownloads(jsonOutput, baseURL, token, strictRemote); err != nil {
					return err
				}
				time.Sleep(1 * time.Second)
			}
		}
		return printDownloads(jsonOutput, baseURL, token, strictRemote)
	},
}

// downloadInfo is a unified structure for display
type downloadInfo struct {
	ID         string  `json:"id"`
	URL        string  `json:"url,omitempty"`
	Filename   string  `json:"filename"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	TotalSize  int64   `json:"total_size"`
	Downloaded int64   `json:"downloaded"`
	Speed      float64 `json:"speed,omitempty"`
}

func printDownloads(jsonOutput bool, baseURL string, token string, strictRemote bool) error {
	var downloads []downloadInfo

	// Try to get from running server first
	if baseURL != "" {
		serverDownloads, err := GetRemoteDownloads(baseURL, token)
		if err != nil {
			if strictRemote {
				return fmt.Errorf("error listing remote downloads: %w", err)
			}
		} else {
			for _, s := range serverDownloads {
				downloads = append(downloads, downloadInfo{
					ID:         s.ID,
					Filename:   s.Filename,
					Status:     s.Status,
					Progress:   s.Progress,
					TotalSize:  s.TotalSize,
					Downloaded: s.Downloaded,
					Speed:      s.Speed,
				})
			}
		}
	}

	// Fall back to database only when not explicitly targeting a remote host.
	if len(downloads) == 0 && (!strictRemote || baseURL == "") {
		dbDownloads, err := store.ListAllDownloads()
		if err != nil {
			return fmt.Errorf("error listing downloads: %w", err)
		}

		for _, d := range dbDownloads {
			var progress float64
			if d.TotalSize > 0 {
				progress = float64(d.Downloaded) * 100 / float64(d.TotalSize)
			}
			downloads = append(downloads, downloadInfo{
				ID:         d.ID,
				Filename:   d.Filename,
				Status:     d.Status,
				Progress:   progress,
				TotalSize:  d.TotalSize,
				Downloaded: d.Downloaded,
			})
		}
	}

	if len(downloads) == 0 {
		if !jsonOutput {
			fmt.Println("No downloads found.")
		} else {
			fmt.Println("[]")
		}
		return nil
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(downloads, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tFILENAME\tSTATUS\tPROGRESS\tSPEED\tSIZE")
	_, _ = fmt.Fprintln(w, "--\t--------\t------\t--------\t-----\t----")

	for _, d := range downloads {
		progress := fmt.Sprintf("%.1f%%", d.Progress)
		size := utils.FormatBytes(d.TotalSize)

		// Speed display
		var speed string
		if d.Speed > 0 {
			speed = utils.FormatSpeed(d.Speed)
		} else {
			speed = "-"
		}

		// Truncate ID for display
		id := d.ID
		if len(id) > 8 {
			id = id[:8]
		}

		// Truncate filename
		filename := d.Filename
		if len(filename) > 25 {
			filename = filename[:22] + "..."
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", id, filename, d.Status, progress, speed, size)
	}
	_ = w.Flush()
	return nil
}

func showDownloadDetails(partialID string, jsonOutput bool, baseURL string, token string) error {
	strictRemote := resolveHostTarget() != ""

	// Resolve partial ID
	fullID, err := resolveDownloadID(partialID)
	if err != nil {
		return err
	}

	// Try to get from running server first
	if baseURL != "" {
		path := fmt.Sprintf("/download?id=%s", url.QueryEscape(fullID))
		resp, err := doAPIRequest(http.MethodGet, baseURL, token, path, nil)
		if err != nil {
			if strictRemote {
				return fmt.Errorf("error fetching remote download details: %w", err)
			}
		} else {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					utils.Debug("Error closing response body: %v", err)
				}
			}()
			if resp.StatusCode == http.StatusOK {
				var status types.DownloadStatus
				if err := json.NewDecoder(resp.Body).Decode(&status); err == nil {
					printDownloadDetail(status, jsonOutput)
					return nil
				} else if strictRemote {
					return fmt.Errorf("error decoding remote download details: %w", err)
				}
			} else if strictRemote {
				if resp.StatusCode == http.StatusNotFound {
					return fmt.Errorf("remote download not found: %s", partialID)
				}
				return fmt.Errorf("remote server returned %s", resp.Status)
			}
		}
	}

	// Fall back to database - search through all downloads
	downloads, err := store.ListAllDownloads()
	if err != nil {
		return fmt.Errorf("error listing downloads: %w", err)
	}

	var found *types.DownloadRecord
	for _, d := range downloads {
		if d.ID == fullID {
			found = &d
			break
		}
	}

	if found == nil {
		return fmt.Errorf("download not found: %s", partialID)
	}

	var progress float64
	if found.TotalSize > 0 {
		progress = float64(found.Downloaded) * 100 / float64(found.TotalSize)
	}

	status := types.DownloadStatus{
		ID:         found.ID,
		URL:        found.URL,
		Filename:   found.Filename,
		Status:     found.Status,
		Error:      found.Error,
		TotalSize:  found.TotalSize,
		Downloaded: found.Downloaded,
		Progress:   progress,
	}
	printDownloadDetail(status, jsonOutput)
	return nil
}

func printDownloadDetail(d types.DownloadStatus, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.MarshalIndent(d, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("ID:         %s\n", d.ID)
	fmt.Printf("URL:        %s\n", d.URL)
	fmt.Printf("Filename:   %s\n", d.Filename)
	fmt.Printf("Status:     %s\n", d.Status)
	fmt.Printf("Progress:   %.1f%%\n", d.Progress)
	fmt.Printf("Downloaded: %s / %s\n", utils.FormatBytes(d.Downloaded), utils.FormatBytes(d.TotalSize))
	if d.Speed > 0 {
		fmt.Printf("Speed:      %s\n", utils.FormatSpeed(d.Speed))
	}
	if d.Error != "" {
		fmt.Printf("Error:      %s\n", d.Error)
	}
}

func init() {
	rootCmd.AddCommand(lsCmd)
	lsCmd.Flags().Bool("json", false, "Output in JSON format")
	lsCmd.Flags().Bool("watch", false, "Watch mode: refresh every second")
}
