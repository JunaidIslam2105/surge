package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/service"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
	"github.com/google/uuid"
)

// DownloadRequest represents a download request from the browser extension
type DownloadRequest struct {
	URL                  string            `json:"url"`
	Filename             string            `json:"filename,omitempty"`
	Path                 string            `json:"path,omitempty"`
	RelativeToDefaultDir bool              `json:"relative_to_default_dir,omitempty"`
	Mirrors              []string          `json:"mirrors,omitempty"`
	SkipApproval         bool              `json:"skip_approval,omitempty"` // Extension validated request, skip TUI prompt
	Headers              map[string]string `json:"headers,omitempty"`       // Custom HTTP headers from browser (cookies, auth, etc.)
	IsExplicitCategory   bool              `json:"is_explicit_category,omitempty"`
	Workers              int               `json:"workers,omitempty"`        // Per-task worker count override (bypasses √size heuristic when >0)
	MinChunkSize         int64             `json:"min_chunk_size,omitempty"` // Per-task minimum chunk size override
}

type BatchDownloadRequest struct {
	Downloads    []DownloadRequest `json:"downloads"`
	Path         string            `json:"path,omitempty"`
	SkipApproval bool              `json:"skip_approval,omitempty"`
}

type resolvedDownloadRequest struct {
	request       DownloadRequest
	settings      *config.Settings
	outPath       string
	urlForAdd     string
	mirrorsForAdd []string
	isDuplicate   bool
	isActive      bool
}

func handleDownload(w http.ResponseWriter, r *http.Request, defaultOutputDir string, service service.DownloadService) {
	if handleDownloadStatusRequest(w, r, service) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if service == nil {
		http.Error(w, "Service unavailable", http.StatusInternalServerError)
		return
	}

	resolved, err := resolveDownloadRequest(r, defaultOutputDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if maybeRequireDownloadApproval(w, service, resolved) {
		return
	}

	newID, filename, err := enqueueDownloadRequest(r, service, resolved)
	if err != nil {
		recordPreflightDownloadError(resolved.urlForAdd, resolved.outPath, err)
		publishSystemLog(fmt.Sprintf("Error adding %s: %v", resolved.urlForAdd, err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	atomic.AddInt32(&activeDownloads, 1)
	writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"status":   "queued",
		"message":  "Download queued successfully",
		"id":       newID,
		"filename": filename,
	})
}

func handleDownloadStatusRequest(w http.ResponseWriter, r *http.Request, service service.DownloadService) bool {
	if r.Method != http.MethodGet {
		return false
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return true
	}

	if service == nil {
		http.Error(w, "Service unavailable", http.StatusInternalServerError)
		return true
	}

	status, err := service.GetStatus(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return true
	}

	writeJSONResponse(w, http.StatusOK, status)
	return true
}

func decodeAndValidateDownloadRequest(r *http.Request) (DownloadRequest, error) {
	var req DownloadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		return req, fmt.Errorf("invalid json: %w", err)
	}
	return validateDownloadRequest(req)
}

func validateDownloadRequest(req DownloadRequest) (DownloadRequest, error) {
	if req.URL == "" {
		return req, fmt.Errorf("url is required")
	}
	if strings.Contains(req.Filename, "..") {
		return req, fmt.Errorf("invalid filename")
	}
	if strings.Contains(req.Filename, "/") || strings.Contains(req.Filename, "\\") {
		return req, fmt.Errorf("invalid filename")
	}
	if strings.Contains(req.Path, "..") {
		return req, fmt.Errorf("invalid path")
	}
	if req.RelativeToDefaultDir && req.Path != "" {
		// Linux filepath.IsAbs does not recognize Windows drive paths, so those
		// are normalized later against the daemon's default download directory.
		if filepath.IsAbs(req.Path) && !utils.IsWindowsAbsPath(req.Path) {
			return req, fmt.Errorf("invalid path")
		}
		cleanPath := filepath.Clean(req.Path)
		if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
			return req, fmt.Errorf("invalid path")
		}
		req.Path = cleanPath
	}
	return req, nil
}

func handleBatchDownload(w http.ResponseWriter, r *http.Request, defaultOutputDir string, service service.DownloadService) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if service == nil {
		http.Error(w, "Service unavailable", http.StatusInternalServerError)
		return
	}

	var req BatchDownloadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Downloads) == 0 {
		http.Error(w, "downloads are required", http.StatusBadRequest)
		return
	}

	settings := getSettings()
	sharedPath := utils.EnsureAbsPath(resolveOutputDir(req.Path, false, defaultOutputDir, settings))
	requests := make([]types.DownloadEvent, 0, len(req.Downloads))

	for _, item := range req.Downloads {
		if item.Path == "" {
			item.Path = sharedPath
		}
		item.SkipApproval = req.SkipApproval
		validated, err := validateDownloadRequest(item)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		urlForAdd, mirrorsForAdd := normalizeDownloadTargets(validated.URL, validated.Mirrors)
		itemPath := utils.EnsureAbsPath(resolveOutputDir(validated.Path, validated.RelativeToDefaultDir, defaultOutputDir, settings))
		requests = append(requests, types.DownloadEvent{
			Type:         types.EventRequest,
			DownloadID:   uuid.New().String(),
			URL:          urlForAdd,
			Filename:     validated.Filename,
			Path:         itemPath,
			Mirrors:      mirrorsForAdd,
			Headers:      validated.Headers,
			Workers:      validated.Workers,
			MinChunkSize: validated.MinChunkSize,
		})
	}

	if !req.SkipApproval {
		if serverProgram == nil {
			writeJSONResponse(w, http.StatusConflict, map[string]string{
				"status":  "error",
				"message": "Batch confirmation requires the TUI",
			})
			return
		}
		batchID := uuid.New().String()
		if err := service.Publish(types.DownloadEvent{
			Type:        types.EventBatchRequest,
			DownloadID:  batchID,
			Path:        sharedPath,
			BatchEvents: requests,
		}); err != nil {
			http.Error(w, "Failed to notify TUI: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, http.StatusAccepted, map[string]string{
			"status":  "pending_approval",
			"message": "Batch download request sent to TUI for confirmation",
			"id":      batchID,
		})
		return
	}

	queued := 0
	var failures []map[string]string
	for _, item := range requests {
		resolved := &resolvedDownloadRequest{
			request: DownloadRequest{
				URL:          item.URL,
				Filename:     item.Filename,
				Path:         item.Path,
				Mirrors:      item.Mirrors,
				SkipApproval: true,
				Headers:      item.Headers,
				Workers:      item.Workers,
				MinChunkSize: item.MinChunkSize,
			},
			settings:      settings,
			outPath:       item.Path,
			urlForAdd:     item.URL,
			mirrorsForAdd: item.Mirrors,
		}
		if _, _, err := enqueueDownloadRequest(r, service, resolved); err != nil {
			recordPreflightDownloadError(item.URL, item.Path, err)
			publishSystemLog(fmt.Sprintf("Error adding %s: %v", item.URL, err))
			failures = append(failures, map[string]string{
				"url":   item.URL,
				"error": err.Error(),
			})
			continue
		}
		atomic.AddInt32(&activeDownloads, 1)
		queued++
	}

	if len(failures) > 0 {
		statusCode := http.StatusMultiStatus
		status := "partial"
		message := "Batch downloads partially queued"
		if queued == 0 {
			statusCode = http.StatusInternalServerError
			status = "error"
			message = "Batch downloads failed"
		}
		writeJSONResponse(w, statusCode, map[string]interface{}{
			"status":   status,
			"message":  message,
			"count":    queued,
			"failures": failures,
		})
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "queued",
		"message": "Batch downloads queued successfully",
		"count":   queued,
	})
}

func resolveDownloadRequest(r *http.Request, defaultOutputDir string) (*resolvedDownloadRequest, error) {
	settings := getSettings()
	req, err := decodeAndValidateDownloadRequest(r)
	if err != nil {
		return nil, err
	}

	utils.Debug("Received download request: URL=%s, Filename=%s, Path=%s, Headers=%v", req.URL, req.Filename, req.Path, req.Headers)

	outPath := utils.EnsureAbsPath(resolveOutputDir(req.Path, req.RelativeToDefaultDir, defaultOutputDir, settings))
	urlForAdd, mirrorsForAdd := normalizeDownloadTargets(req.URL, req.Mirrors)
	isDuplicate, isActive := resolveDuplicateState(urlForAdd, settings)

	utils.Debug("Download request: URL=%s, Filename=%s, SkipApproval=%v, isDuplicate=%v, isActive=%v", urlForAdd, req.Filename, req.SkipApproval, isDuplicate, isActive)

	return &resolvedDownloadRequest{
		request:       req,
		settings:      settings,
		outPath:       outPath,
		urlForAdd:     urlForAdd,
		mirrorsForAdd: mirrorsForAdd,
		isDuplicate:   isDuplicate,
		isActive:      isActive,
	}, nil
}

func normalizeDownloadTargets(url string, mirrors []string) (string, []string) {
	if len(mirrors) == 0 && strings.Contains(url, ",") {
		return ParseURLArg(url)
	}
	return url, mirrors
}

func resolveDuplicateState(urlForAdd string, settings *config.Settings) (bool, bool) {
	activeDownloadsFunc := func() map[string]*types.DownloadRecord {
		active := make(map[string]*types.DownloadRecord)
		if GlobalPool != nil {
			for _, cfg := range GlobalPool.GetAll() {
				c := cfg
				active[c.ID] = &c
			}
		}
		return active
	}

	dupResult := orchestrator.CheckForDuplicate(urlForAdd, activeDownloadsFunc)
	if dupResult == nil {
		return false, false
	}
	return dupResult.Exists, dupResult.IsActive
}

func maybeRequireDownloadApproval(w http.ResponseWriter, service service.DownloadService, resolved *resolvedDownloadRequest) bool {
	req := resolved.request

	// EXTENSION VETTING SHORTCUT:
	// If SkipApproval is true, we trust the extension completely.
	// The backend will auto-rename duplicate files, so no need to reject.
	if req.SkipApproval {
		utils.Debug("Extension request: skipping all prompts, proceeding with download")
		return false
	}

	shouldPrompt := config.Resolve[bool](resolved.settings.Extension.ExtensionPrompt) || (config.Resolve[bool](resolved.settings.General.WarnOnDuplicate) && resolved.isDuplicate)
	if !shouldPrompt {
		return false
	}

	if serverProgram != nil {
		utils.Debug("Requesting TUI confirmation for: %s (Duplicate: %v)", req.URL, resolved.isDuplicate)

		downloadID := uuid.New().String()
		if err := service.Publish(types.DownloadEvent{
			Type:         types.EventRequest,
			DownloadID:   downloadID,
			URL:          resolved.urlForAdd,
			Filename:     req.Filename,
			Path:         resolved.outPath,
			Mirrors:      resolved.mirrorsForAdd,
			Headers:      req.Headers,
			Workers:      req.Workers,
			MinChunkSize: req.MinChunkSize,
		}); err != nil {
			recordPreflightDownloadError(resolved.urlForAdd, resolved.outPath, err)
			publishSystemLog(fmt.Sprintf("Error adding %s: %v", resolved.urlForAdd, err))
			http.Error(w, "Failed to notify TUI: "+err.Error(), http.StatusInternalServerError)
			return true
		}

		writeJSONResponse(w, http.StatusAccepted, map[string]string{
			"status":  "pending_approval",
			"message": "Download request sent to TUI for confirmation",
			"id":      downloadID,
		})
		return true
	}

	// HEADLESS/SERVER MODE:
	// If we're here, shouldPrompt must be true but we have no TUI.
	// We auto-approve extension requests that are NOT duplicates, as there is
	// no way to display a confirmation prompt in headless mode.
	if !resolved.isDuplicate {
		utils.Debug("Headless mode: auto-approving extension request (bypass ExtensionPrompt)")
		return false
	}

	writeJSONResponse(w, http.StatusConflict, map[string]string{
		"status":  "error",
		"message": "Download rejected: Duplicate download detected (Headless mode)",
	})
	return true
}

func enqueueDownloadRequest(r *http.Request, service service.DownloadService, resolved *resolvedDownloadRequest) (string, string, error) {
	lifecycle, err := lifecycleForLocalService(service)
	if err != nil {
		return "", "", fmt.Errorf("failed to initialize lifecycle manager: %w", err)
	}

	req := resolved.request
	reqID := r.URL.Query().Get("id")

	if lifecycle != nil {
		dlReq := &orchestrator.DownloadRequest{
			URL:                resolved.urlForAdd,
			Filename:           req.Filename,
			Path:               resolved.outPath,
			Mirrors:            resolved.mirrorsForAdd,
			Headers:            req.Headers,
			IsExplicitCategory: req.IsExplicitCategory,
			SkipApproval:       req.SkipApproval,
			Workers:            req.Workers,
			MinChunkSize:       req.MinChunkSize,
		}
		// Use the application-level enqueue context instead of r.Context().
		// The HTTP request context is cancelled when the handler returns or
		// the client disconnects, which races against the server probe that
		// can take several seconds — causing sporadic "context cancelled"
		// failures on otherwise valid downloads.
		ctx := currentEnqueueContext()
		if reqID != "" {
			return lifecycle.EnqueueWithID(ctx, dlReq, reqID)
		}
		return lifecycle.Enqueue(ctx, dlReq)
	}

	if reqID != "" {
		id, err := service.AddWithID(resolved.urlForAdd, resolved.outPath, req.Filename, resolved.mirrorsForAdd, req.Headers, reqID, req.IsExplicitCategory, req.Workers, req.MinChunkSize)
		return id, req.Filename, err
	}
	id, err := service.Add(resolved.urlForAdd, resolved.outPath, req.Filename, resolved.mirrorsForAdd, req.Headers, req.IsExplicitCategory, req.Workers, req.MinChunkSize)
	return id, req.Filename, err
}

// processDownloads handles the logic of adding downloads either to local pool or remote server
// Returns the number of successfully added downloads
func processDownloads(urls []string, outputDir string, port int) int {
	successCount := 0

	// If port > 0, we are sending to a remote server
	if port > 0 {
		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
		token := resolveLocalToken()
		for _, arg := range urls {
			url, mirrors := ParseURLArg(arg)
			if url == "" {
				continue
			}
			err := sendToServer(url, mirrors, outputDir, baseURL, token)
			if err != nil {
				fmt.Printf("Error adding %s: %v\n", url, err)
			} else {
				successCount++
			}
		}
		return successCount
	}

	// Internal add (TUI or Headless mode)
	if GlobalService == nil {
		fmt.Fprintln(os.Stderr, "Error: GlobalService not initialized")
		return 0
	}

	settings := getSettings()

	lifecycle, err := lifecycleForLocalService(GlobalService)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: unable to initialize lifecycle manager:", err)
		return 0
	}

	for _, arg := range urls {
		// Validation
		if arg == "" {
			continue
		}

		urlArg, mirrors := ParseURLArg(arg)
		if urlArg == "" {
			continue
		}

		// Ensure the URL is valid and normalized
		url, err := ValidateAndNormalizeURL(urlArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding %s: %v\n", urlArg, err)
			continue
		}

		// Prepare output path
		outPath := resolveOutputDir(outputDir, false, "", settings)
		outPath = utils.EnsureAbsPath(outPath)

		// CLI explicit arg means we do not auto-route when user provided an explicit output path.
		isExplicit := isExplicitOutputPath(outPath, config.Resolve[string](settings.General.DefaultDownloadDir))
		if lifecycle == nil {
			err = fmt.Errorf("lifecycle manager unavailable")
			recordPreflightDownloadError(url, outPath, err)
			publishSystemLog(fmt.Sprintf("Error adding %s: %v", url, err))
			continue
		}

		_, _, err = lifecycle.Enqueue(currentEnqueueContext(), &orchestrator.DownloadRequest{
			URL:                url,
			Path:               outPath,
			Mirrors:            mirrors,
			IsExplicitCategory: isExplicit,
		})
		if err != nil {
			recordPreflightDownloadError(url, outPath, err)
			publishSystemLog(fmt.Sprintf("Error adding %s: %v", url, err))
			continue
		}
		atomic.AddInt32(&activeDownloads, 1)
		successCount++
	}
	return successCount
}

func resolveOutputDir(reqPath string, relativeToDefaultDir bool, defaultOutputDir string, settings *config.Settings) string {
	outPath := reqPath

	if mapped := mapClientWindowsPath(reqPath, relativeToDefaultDir, defaultOutputDir, settings); mapped != "" {
		return mapped
	}

	if relativeToDefaultDir && reqPath != "" {
		baseDir := config.Resolve[string](settings.General.DefaultDownloadDir)
		if baseDir == "" {
			baseDir = defaultOutputDir
		}
		if baseDir == "" {
			baseDir = "."
		}
		outPath = filepath.Join(baseDir, reqPath)
	} else if outPath == "" {
		if defaultOutputDir != "" {
			outPath = defaultOutputDir
		} else if config.Resolve[string](settings.General.DefaultDownloadDir) != "" {
			outPath = config.Resolve[string](settings.General.DefaultDownloadDir)
		} else {
			outPath = "."
		}
	}

	return outPath
}

func mapClientWindowsPath(reqPath string, relativeToDefaultDir bool, defaultOutputDir string, settings *config.Settings) string {
	reqPath = strings.TrimSpace(reqPath)
	if reqPath == "" || !utils.IsWindowsAbsPath(reqPath) {
		return ""
	}

	// On a Windows host, a Windows-absolute path is a real local path.
	// filepath.IsAbs returns true for it, so the normal code path handles it
	// correctly. Only remap when the daemon is running on a non-Windows OS
	// (i.e. the path is from a Windows browser extension talking to a Linux/macOS daemon).
	if runtime.GOOS == "windows" && filepath.IsAbs(reqPath) {
		return ""
	}

	baseDir := "."
	if relativeToDefaultDir {
		if settings != nil && strings.TrimSpace(config.Resolve[string](settings.General.DefaultDownloadDir)) != "" {
			baseDir = config.Resolve[string](settings.General.DefaultDownloadDir)
		} else if strings.TrimSpace(defaultOutputDir) != "" {
			baseDir = defaultOutputDir
		}
	} else {
		if strings.TrimSpace(defaultOutputDir) != "" {
			baseDir = defaultOutputDir
		} else if settings != nil && strings.TrimSpace(config.Resolve[string](settings.General.DefaultDownloadDir)) != "" {
			baseDir = config.Resolve[string](settings.General.DefaultDownloadDir)
		}
	}

	if mapped, ok := utils.MapWindowsPathToDefaultDir(reqPath, baseDir); ok {
		return mapped
	}

	if !shouldFallbackUnmappedWindowsPath(relativeToDefaultDir, runtime.GOOS) {
		return ""
	}

	// If we positively identified a Windows absolute path but could not
	// project it onto the server-side default directory, keep the download
	// rooted at baseDir instead of letting a bogus "E:/..." path turn into
	// a Linux-relative path via EnsureAbsPath.
	return filepath.Clean(baseDir)
}

func shouldFallbackUnmappedWindowsPath(relativeToDefaultDir bool, hostOS string) bool {
	return relativeToDefaultDir || hostOS != "windows"
}
