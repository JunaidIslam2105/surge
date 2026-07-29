package cmd

import (
	"sync"
	"testing"

	"github.com/SurgeDM/Surge/internal/utils"
	"github.com/adrg/xdg"
)

var xdgEnvMu sync.Mutex

func setupXDGEnvIsolation(t *testing.T) string {
	t.Helper()
	xdgEnvMu.Lock()

	tempDir := t.TempDir()

	oldConfigHome := xdg.ConfigHome
	oldDataHome := xdg.DataHome
	oldStateHome := xdg.StateHome
	oldCacheHome := xdg.CacheHome
	oldRuntimeDir := xdg.RuntimeDir

	xdg.ConfigHome = tempDir
	xdg.DataHome = tempDir
	xdg.StateHome = tempDir
	xdg.CacheHome = tempDir
	xdg.RuntimeDir = tempDir

	t.Cleanup(func() {
		utils.CloseDebug()
		xdg.ConfigHome = oldConfigHome
		xdg.DataHome = oldDataHome
		xdg.StateHome = oldStateHome
		xdg.CacheHome = oldCacheHome
		xdg.RuntimeDir = oldRuntimeDir
		if err := resetSharedStateDB(); err != nil {
			t.Errorf("failed to restore shared Surge test directories: %v", err)
		}
		xdgEnvMu.Unlock()
	})

	t.Setenv("APPDATA", tempDir)
	t.Setenv("USERPROFILE", tempDir)
	t.Setenv("SystemRoot", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_DATA_HOME", tempDir)
	t.Setenv("XDG_STATE_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("XDG_RUNTIME_DIR", tempDir)
	t.Setenv("HOME", tempDir)
	t.Setenv("SURGE_SYSTEM_RUNTIME_DIR", tempDir)
	t.Setenv("SURGE_SYSTEM_STATE_DIR", tempDir)

	return tempDir
}
