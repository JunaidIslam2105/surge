package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureAbsPath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "relative dot",
			in:   ".",
			want: wd,
		},
		{
			name: "relative path",
			in:   "foo",
			want: filepath.Join(wd, "foo"),
		},
		{
			name: "empty string",
			in:   "",
			want: wd,
		},
		{
			name: "already absolute",
			in:   wd,
			want: wd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnsureAbsPath(tt.in)
			if got != tt.want {
				t.Errorf("EnsureAbsPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
