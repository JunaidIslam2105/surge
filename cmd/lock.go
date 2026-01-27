package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/surge-downloader/surge/internal/config"
)

type InstanceLock struct {
	flock *flock.Flock
	path  string
}

var instanceLock *InstanceLock

func AcquireLock() (bool, error) {

	if err := config.EnsureDirs(); err != nil {
		return false, fmt.Errorf("failed to ensure config dirs: %w", err)
	}

	lockPath := filepath.Join(config.GetSurgeDir(), "surge.lock")
	filelock := flock.New(lockPath)

	locked, err := filelock.TryLock()
	if err != nil {
		return false, fmt.Errorf("failed to try lock: %w", err)
	}

	if locked {

		// We are master

		instanceLock = &InstanceLock{
			flock: filelock,
			path:  lockPath,
		}

		return true, nil
	}

	return false, nil
}

func ReleaseLock() error {
	if instanceLock != nil && instanceLock.flock != nil {
		return instanceLock.flock.Unlock()
	}
	return nil
}
