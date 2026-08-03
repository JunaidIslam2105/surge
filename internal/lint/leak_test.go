package lint

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigLeakPrevention statically analyzes all test files to ensure they
// properly isolate the configuration environment to prevent mutating the
// developer's actual device configuration.
func TestConfigLeakPrevention(t *testing.T) {
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "node_modules" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		contentStr := string(content)

		// If a test uses a known isolation wrapper, we assume it's fully isolated.
		isIsolatedViaWrapper := strings.Contains(contentStr, "setupIsolatedCmdState") ||
			strings.Contains(contentStr, "setupTestEnv") ||
			strings.Contains(contentStr, "SetupTestEnv") ||
			strings.Contains(contentStr, "setupXDGEnvIsolation")

		if isIsolatedViaWrapper {
			return nil // Fully isolated, skip further manual checks
		}

		// 1. If a test manually mocks XDG_CONFIG_HOME, it MUST also mock APPDATA for Windows
		hasXDG := strings.Contains(contentStr, "\"XDG_CONFIG_HOME\"") || strings.Contains(contentStr, "`XDG_CONFIG_HOME`")
		hasAppData := strings.Contains(strings.ToUpper(contentStr), "\"APPDATA\"") || strings.Contains(strings.ToUpper(contentStr), "`APPDATA`")

		if hasXDG && !hasAppData {
			// Some tests are allowed to just mock XDG if they are very specific (e.g., paths_test.go),
			// but we enforce this globally for safety unless skipped by a comment.
			if !strings.Contains(contentStr, "lint:ignore-leak-check") {
				t.Errorf("%s: Leaky test detected! Mocks XDG_CONFIG_HOME but forgets to mock APPDATA. This will leak on Windows devices.", path)
			}
		}

		if hasAppData && !hasXDG {
			if !strings.Contains(contentStr, "lint:ignore-leak-check") {
				t.Errorf("%s: Leaky test detected! Mocks APPDATA but forgets to mock XDG_CONFIG_HOME. This will leak on Linux/macOS devices.", path)
			}
		}

		// 2. If a test calls config.SaveSettings, it must be properly isolated
		if strings.Contains(contentStr, "config.SaveSettings") || strings.Contains(contentStr, "SaveSettings(") {
			isIsolated := hasXDG || hasAppData ||
				strings.Contains(contentStr, "setupIsolatedCmdState") ||
				strings.Contains(contentStr, "setupTestEnv") ||
				strings.Contains(contentStr, "SetupTestEnv") ||
				strings.Contains(contentStr, "setupXDGEnvIsolation")

			if !isIsolated {
				if !strings.Contains(contentStr, "lint:ignore-leak-check") {
					t.Errorf("%s: Leaky test detected! Calls SaveSettings but does not appear to isolate the environment (e.g. missing XDG_CONFIG_HOME / APPDATA mock).", path)
				}
			}
		}

		// 3. If a test uses os.Setenv or os.Chdir, it must not leak
		if strings.Contains(contentStr, "os.Setenv") || strings.Contains(contentStr, "os.Unsetenv") {
			if !isIsolatedViaWrapper {
				if !strings.Contains(contentStr, "lint:ignore-leak-check") {
					t.Errorf("%s: Leaky test detected! Uses os.Setenv or os.Unsetenv directly without an isolation wrapper. Use t.Setenv() instead, or add 'lint:ignore-leak-check' if you manually handle cleanup.", path)
				}
			}
		}

		if strings.Contains(contentStr, "os.Chdir") {
			if !isIsolatedViaWrapper {
				if !strings.Contains(contentStr, "lint:ignore-leak-check") {
					t.Errorf("%s: Leaky test detected! Uses os.Chdir directly. This changes the working directory for all tests. Add 'lint:ignore-leak-check' if you manually handle cleanup.", path)
				}
			}
		}

		// 4. Check for unclosed temporary directories
		if strings.Contains(contentStr, "os.MkdirTemp") && !strings.Contains(contentStr, "os.RemoveAll") {
			if !strings.Contains(contentStr, "lint:ignore-leak-check") {
				t.Errorf("%s: Leaky test detected! Uses os.MkdirTemp but does not seem to call os.RemoveAll for cleanup.", path)
			}
		}

		// 5. Check for global state contamination with store
		if strings.Contains(contentStr, "store.Configure") && !strings.Contains(contentStr, "store.CloseDB") {
			if !strings.Contains(contentStr, "lint:ignore-leak-check") {
				t.Errorf("%s: Leaky test detected! Uses store.Configure but does not call store.CloseDB.", path)
			}
		}

		// 6. Check for dangling file handles
		if (strings.Contains(contentStr, "os.Create(") || strings.Contains(contentStr, "os.Open(")) && !strings.Contains(contentStr, ".Close()") {
			if !strings.Contains(contentStr, "lint:ignore-leak-check") {
				t.Errorf("%s: Leaky test detected! Uses os.Create or os.Open but does not seem to call .Close().", path)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("Failed to walk directory: %v", err)
	}
}

// TestGlobalGoleakEnforcement verifies that every package containing tests
// also has a TestMain function that uses go.uber.org/goleak.
func TestGlobalGoleakEnforcement(t *testing.T) {
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "node_modules" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}

			// Check files in this directory
			entries, readErr := os.ReadDir(path)
			if readErr != nil {
				return readErr
			}

			hasTest := false
			hasMainTestFile := false
			hasGoleak := false
			hasIgnore := false

			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(entry.Name(), "_test.go") {
					hasTest = true
					if entry.Name() == "main_test.go" {
						hasMainTestFile = true
					}

					content, readErr := os.ReadFile(filepath.Join(path, entry.Name()))
					if readErr == nil {
						contentStr := string(content)
						if strings.Contains(contentStr, "goleak.VerifyTestMain") || strings.Contains(contentStr, "goleak.Find()") || strings.Contains(contentStr, "goleak.VerifyNone") {
							hasGoleak = true
						}
						if strings.Contains(contentStr, "lint:ignore-leak-check") {
							hasIgnore = true
						}
					}
				}
			}

			if hasTest && !hasIgnore {
				if !hasMainTestFile {
					t.Errorf("%s: Package contains test files but is missing a main_test.go file.", path)
				} else if !hasGoleak {
					t.Errorf("%s: Package has a main_test.go but is missing goleak validation.", path)
				}
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("Failed to walk directory: %v", err)
	}
}
