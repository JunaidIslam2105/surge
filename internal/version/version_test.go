package version

import "testing"

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"with v prefix", "v1.2.3", "1.2.3"},
		{"without prefix", "1.2.3", "1.2.3"},
		{"with whitespace", "  1.2.3  ", "1.2.3"},
		{"v prefix and whitespace", "  v1.2.3 ", "1.2.3"},
		{"suffix kept", "v1.2.3-beta", "1.2.3-beta"},
		{"uppercase V untouched", "V1.2.3", "V1.2.3"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeVersion(tt.in); got != tt.want {
				t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{"newer patch", "1.2.3", "1.2.2", true},
		{"newer minor", "1.3.0", "1.2.9", true},
		{"newer major", "2.0.0", "1.99.99", true},
		{"equal", "1.2.3", "1.2.3", false},
		{"current ahead", "1.2.3", "1.2.4", false},
		{"current ahead major", "1.9.9", "2.0.0", false},
		{"lexicographic trap", "1.10.0", "1.9.9", true},
		{"lexicographic trap reversed", "1.9.9", "1.10.0", false},
		{"major bump wins over minor", "2.0.0-beta", "1.9.9", true},
		{"current behind major", "2.0.0", "1.9.9", true},
		{"zero handling", "0.10.0", "0.9.0", true},
		{"zero current ahead", "0.9.0", "0.10.0", false},
		{"same with v prefix", "v1.2.3", "1.2.3", false},
		{"stable newer than prerelease", "1.2.3", "1.2.3-beta.1", true},
		{"prerelease older than stable", "1.2.3-beta.1", "1.2.3", false},
		{"newer prerelease", "1.2.3-beta.2", "1.2.3-beta.1", true},
		{"build metadata ignored", "1.2.3+build2", "1.2.3+build1", false},
		{"two segments coerced", "1.3", "1.2.9", true},
		{"invalid latest fails closed", "1.2.x", "1.2.0", false},
		{"invalid current fails closed", "1.2.1", "1.2.x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewerVersion(tt.latest, tt.current); got != tt.want {
				t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}
