package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SurgeDM/Surge/internal/config"
	"github.com/SurgeDM/Surge/internal/orchestrator"
	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

func completedSpeedBps(entry types.DownloadRecord) float64 {
	if entry.Status != "completed" {
		return 0
	}
	if entry.AvgSpeed > 0 {
		return entry.AvgSpeed
	}
	if entry.TimeTaken > 0 {
		return float64(entry.TotalSize) * 1000 / float64(entry.TimeTaken)
	}
	return 0
}

type LocalDownloadService struct {
	lifecycle *orchestrator.LifecycleManager
}

func NewLocalDownloadService(lifecycle *orchestrator.LifecycleManager) *LocalDownloadService {
	return &LocalDownloadService{lifecycle: lifecycle}
}

func (s *LocalDownloadService) ReloadSettings(settings *config.Settings) error {
	if s.lifecycle != nil {
		s.lifecycle.ApplySettings(settings)
	}
	return nil
}

func (s *LocalDownloadService) StreamEvents(ctx context.Context) (<-chan types.DownloadEvent, func(), error) {
	if s.lifecycle == nil || s.lifecycle.GetEventBus() == nil {
		return nil, nil, fmt.Errorf("event bus not initialized")
	}
	ch, cleanup := s.lifecycle.GetEventBus().Subscribe()
	return ch, cleanup, nil
}

func (s *LocalDownloadService) Publish(msg types.DownloadEvent) error {
	if s.lifecycle != nil && s.lifecycle.GetEventBus() != nil {
		return s.lifecycle.GetEventBus().Publish(msg)
	}
	return fmt.Errorf("event bus not initialized")
}

func (s *LocalDownloadService) Shutdown() error {
	if s.lifecycle != nil {
		s.lifecycle.Shutdown()
	}
	return nil
}

func (s *LocalDownloadService) Add(url string, path string, filename string, mirrors []string, headers map[string]string, isExplicitCategory bool, workers int, minChunkSize int64) (string, error) {
	if s.lifecycle == nil {
		return "", types.ErrServiceUnavailable
	}
	req := &orchestrator.DownloadRequest{
		URL:                url,
		Path:               path,
		Filename:           filename,
		Mirrors:            mirrors,
		Headers:            headers,
		IsExplicitCategory: isExplicitCategory,
		Workers:            workers,
		MinChunkSize:       minChunkSize,
	}
	id, _, err := s.lifecycle.Enqueue(context.Background(), req)
	return id, err
}

func (s *LocalDownloadService) AddWithID(url string, path string, filename string, mirrors []string, headers map[string]string, id string, isExplicitCategory bool, workers int, minChunkSize int64) (string, error) {
	if s.lifecycle == nil {
		return "", types.ErrServiceUnavailable
	}
	req := &orchestrator.DownloadRequest{
		URL:                url,
		Path:               path,
		Filename:           filename,
		Mirrors:            mirrors,
		Headers:            headers,
		IsExplicitCategory: isExplicitCategory,
		Workers:            workers,
		MinChunkSize:       minChunkSize,
	}
	newID, _, err := s.lifecycle.EnqueueWithID(context.Background(), req, id)
	return newID, err
}

func (s *LocalDownloadService) Pause(id string) error {
	if s.lifecycle == nil {
		return types.ErrServiceUnavailable
	}
	return s.lifecycle.Pause(id)
}

func (s *LocalDownloadService) Resume(id string) error {
	if s.lifecycle == nil {
		return types.ErrServiceUnavailable
	}
	return s.lifecycle.Resume(id)
}

func (s *LocalDownloadService) ResumeBatch(ids []string) []error {
	if s.lifecycle == nil {
		return []error{types.ErrServiceUnavailable}
	}
	return s.lifecycle.ResumeBatch(ids)
}

func (s *LocalDownloadService) UpdateURL(id string, newURL string) error {
	if s.lifecycle == nil {
		return types.ErrServiceUnavailable
	}
	return s.lifecycle.UpdateURL(id, newURL)
}

func (s *LocalDownloadService) Delete(id string) error {
	if s.lifecycle == nil {
		return types.ErrServiceUnavailable
	}
	return s.lifecycle.Cancel(id)
}

func (s *LocalDownloadService) Purge(id string) error {
	destPath := ""
	status, err := s.GetStatus(id)
	if err == nil && status != nil {
		destPath = filepath.Clean(status.DestPath)
	} else {
		history, err := s.History()
		if err == nil {
			for _, entry := range history {
				if entry.ID == id {
					destPath = filepath.Clean(entry.DestPath)
					break
				}
			}
		}
	}
	if err := s.Delete(id); err != nil {
		return err
	}
	if destPath != "" && destPath != "." {
		var errs []string
		if err := utils.RemoveFile(destPath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
		if err := utils.RemoveFile(destPath + types.IncompleteSuffix); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
		if len(errs) > 0 {
			return fmt.Errorf("failed to delete files: %s", strings.Join(errs, ", "))
		}
	}
	return nil
}

func (s *LocalDownloadService) GetStatus(id string) (*types.DownloadStatus, error) {
	if id == "" {
		return nil, fmt.Errorf("missing id")
	}
	if s.lifecycle != nil && s.lifecycle.GetScheduler() != nil {
		if status := s.lifecycle.GetScheduler().GetStatus(id); status != nil {
			return status, nil
		}
	}
	entry, err := store.GetDownload(id)
	if err == nil && entry != nil {
		var progress float64
		if entry.TotalSize > 0 {
			progress = float64(entry.Downloaded) * 100 / float64(entry.TotalSize)
		} else if entry.Status == "completed" {
			progress = 100.0
		}
		status := types.DownloadStatus{
			ID:           entry.ID,
			URL:          entry.URL,
			Filename:     entry.Filename,
			DestPath:     entry.DestPath,
			TotalSize:    entry.TotalSize,
			Downloaded:   entry.Downloaded,
			Progress:     progress,
			Speed:        completedSpeedBps(*entry),
			Status:       entry.Status,
			Error:        entry.Error,
			TimeTaken:    entry.TimeTaken,
			AvgSpeed:     entry.AvgSpeed,
			RateLimit:    entry.RateLimit,
			RateLimitSet: entry.RateLimitSet,
		}
		return &status, nil
	}
	return nil, types.ErrNotFound
}

func (s *LocalDownloadService) History() ([]types.DownloadRecord, error) {
	return store.LoadCompletedDownloads()
}
func (s *LocalDownloadService) ClearCompleted() (int64, error) {
	return store.RemoveCompletedDownloads()
}
func (s *LocalDownloadService) ClearFailed() (int64, error) {
	return store.RemoveFailedDownloads()
}

func (s *LocalDownloadService) SetRateLimit(id string, rate int64) error {
	if rate < 0 {
		return fmt.Errorf("rate limit must be non-negative")
	}
	if s.lifecycle == nil || s.lifecycle.GetScheduler() == nil {
		return types.ErrPoolNotInit
	}

	entry, err := store.GetDownload(id)
	if err != nil && !errors.Is(err, types.ErrNotFound) {
		return err
	}

	poolStatus := s.lifecycle.GetScheduler().GetStatus(id)
	if poolStatus == nil && (entry == nil || entry.Status == "completed") {
		return fmt.Errorf("%w: %s", types.ErrNotFound, id)
	}

	err = store.UpdateRateLimit(id, rate)
	if err != nil && !errors.Is(err, types.ErrNotFound) {
		return err
	}

	foundInPool := s.lifecycle.GetScheduler().SetDownloadRateLimit(id, rate)
	if err != nil && !foundInPool {
		return fmt.Errorf("%w: %s", types.ErrNotFound, id)
	}
	return nil
}

func (s *LocalDownloadService) ClearRateLimit(id string) error {
	if s.lifecycle == nil || s.lifecycle.GetScheduler() == nil {
		return types.ErrPoolNotInit
	}

	entry, err := store.GetDownload(id)
	if err != nil && !errors.Is(err, types.ErrNotFound) {
		return err
	}

	poolStatus := s.lifecycle.GetScheduler().GetStatus(id)
	if poolStatus == nil && (entry == nil || entry.Status == "completed") {
		return fmt.Errorf("%w: %s", types.ErrNotFound, id)
	}

	err = store.ClearRateLimit(id)
	if err != nil && !errors.Is(err, types.ErrNotFound) {
		return err
	}

	foundInPool := s.lifecycle.GetScheduler().ClearDownloadRateLimit(id)
	if err != nil && !foundInPool {
		return fmt.Errorf("%w: %s", types.ErrNotFound, id)
	}
	return nil
}

func (s *LocalDownloadService) SetGlobalRateLimit(rate int64) error {
	if rate < 0 {
		return fmt.Errorf("rate limit must be non-negative")
	}
	if s.lifecycle == nil || s.lifecycle.GetScheduler() == nil {
		return types.ErrPoolNotInit
	}

	settings := s.lifecycle.GetSettings()
	if settings == nil {
		return fmt.Errorf("settings not found")
	}

	if settings.Network.GlobalRateLimit == nil {
		settings.Network.GlobalRateLimit = config.DefaultSettings().Network.GlobalRateLimit
	}
	oldValue := settings.Network.GlobalRateLimit.Value
	settings.Network.GlobalRateLimit.Value = utils.FormatRateLimit(rate)
	if err := config.SaveSettings(settings); err != nil {
		settings.Network.GlobalRateLimit.Value = oldValue
		return err
	}

	s.lifecycle.GetScheduler().SetGlobalRateLimit(rate)
	return nil
}

func (s *LocalDownloadService) SetDefaultRateLimit(rate int64) error {
	if rate < 0 {
		return fmt.Errorf("rate limit must be non-negative")
	}
	if s.lifecycle == nil || s.lifecycle.GetScheduler() == nil {
		return types.ErrPoolNotInit
	}

	settings := s.lifecycle.GetSettings()
	if settings == nil {
		return fmt.Errorf("settings not found")
	}

	if settings.Network.DefaultDownloadRateLimit == nil {
		settings.Network.DefaultDownloadRateLimit = config.DefaultSettings().Network.DefaultDownloadRateLimit
	}
	oldValue := settings.Network.DefaultDownloadRateLimit.Value
	settings.Network.DefaultDownloadRateLimit.Value = utils.FormatRateLimit(rate)
	if err := config.SaveSettings(settings); err != nil {
		settings.Network.DefaultDownloadRateLimit.Value = oldValue
		return err
	}

	s.lifecycle.GetScheduler().SetDefaultDownloadRateLimit(rate)

	if configs := s.lifecycle.GetScheduler().GetAll(); configs != nil {
		var dbErrs []string
		for _, cfg := range configs {
			if !cfg.RateLimitSet {
				if err := store.UpdateDefaultRateLimit(cfg.ID, rate); err != nil {
					dbErrs = append(dbErrs, fmt.Sprintf("%s: %v", cfg.ID, err))
				}
			}
		}
		if len(dbErrs) > 0 {
			return fmt.Errorf("failed to update default rate limit in DB for some downloads: %s", strings.Join(dbErrs, "; "))
		}
	}
	return nil
}

func (s *LocalDownloadService) List() ([]types.DownloadStatus, error) {
	var statuses []types.DownloadStatus
	if s.lifecycle != nil && s.lifecycle.GetScheduler() != nil {
		activeConfigs := s.lifecycle.GetScheduler().GetAll()
		for _, cfg := range activeConfigs {
			statusStr := "downloading"
			var errStr string
			if st := s.lifecycle.GetScheduler().GetStatus(cfg.ID); st != nil {
				statusStr = st.Status
				errStr = st.Error
			}
			status := types.DownloadStatus{
				ID:           cfg.ID,
				URL:          cfg.URL,
				Filename:     cfg.Filename,
				Status:       statusStr,
				Error:        errStr,
				RateLimit:    cfg.RateLimit,
				RateLimitSet: cfg.RateLimitSet,
			}
			if cfg.ProgressState != nil {
				cp := progress.CfgProgress(&cfg)
				if cp != nil {
					downloaded, totalSize, _, sessionElapsed, connections, sessionStart := cp.GetProgress()
					status.TotalSize = totalSize
					status.Downloaded = downloaded
					if dp := cp.GetDestPath(); dp != "" {
						status.DestPath = dp
					}
					if status.TotalSize > 0 {
						status.Progress = float64(status.Downloaded) * 100 / float64(status.TotalSize)
					}
					status.Connections = int(connections)
					if cp.IsPausing() {
						status.Status = "pausing"
					} else if cp.IsPaused() {
						status.Status = "paused"
					} else if cp.Done.Load() {
						status.Status = "completed"
					}
					// GetStatus and metric reads are separate snapshots; recheck errors
					// so a worker failure between those reads is not reported as downloading.
					if err := cp.GetError(); err != nil {
						status.Status = "error"
						status.Error = err.Error()
					}
					if status.Status == "downloading" {
						sessionDownloaded := downloaded - sessionStart
						if sessionElapsed.Seconds() > 0 && sessionDownloaded > 0 {
							status.Speed = float64(sessionDownloaded) / sessionElapsed.Seconds()
							remaining := status.TotalSize - status.Downloaded
							if remaining > 0 && status.Speed > 0 {
								status.ETA = int64(float64(remaining) / status.Speed)
							}
						}
					}
				}
			}
			statuses = append(statuses, status)
		}
	}
	dbDownloads, err := store.ListAllDownloads()
	if err == nil {
		existingIDs := make(map[string]bool)
		for _, s := range statuses {
			existingIDs[s.ID] = true
		}
		for _, d := range dbDownloads {
			if existingIDs[d.ID] {
				continue
			}
			var progress float64
			if d.TotalSize > 0 {
				progress = float64(d.Downloaded) * 100 / float64(d.TotalSize)
			} else if d.Status == "completed" {
				progress = 100.0
			}
			statuses = append(statuses, types.DownloadStatus{
				ID:           d.ID,
				URL:          d.URL,
				Filename:     d.Filename,
				DestPath:     d.DestPath,
				Status:       d.Status,
				Error:        d.Error,
				TotalSize:    d.TotalSize,
				Downloaded:   d.Downloaded,
				Progress:     progress,
				Speed:        completedSpeedBps(d),
				Connections:  0,
				TimeTaken:    d.TimeTaken,
				AvgSpeed:     d.AvgSpeed,
				RateLimit:    d.RateLimit,
				RateLimitSet: d.RateLimitSet,
			})
		}
	}
	return statuses, nil
}
