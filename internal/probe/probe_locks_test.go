package probe

import "testing"

func TestLockProbeHostReleasesUnusedLock(t *testing.T) {
	release := lockProbeHost("https://example.com/file")
	release()

	probeHostLocks.Lock()
	_, retained := probeHostLocks.hosts["example.com"]
	probeHostLocks.Unlock()
	if retained {
		t.Fatal("released host lock was retained")
	}
}
