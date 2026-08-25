package tui

import "testing"

func TestRenderCacheKeyAndInvalidation(t *testing.T) {
	type key struct {
		width, height int
		version       uint64
	}

	var cache renderCache[key]
	initial := key{width: 80, height: 24, version: 1}
	if _, ok := cache.Get(initial); ok {
		t.Fatal("empty cache reported a hit")
	}
	if got := cache.Set(initial, "render"); got != "render" {
		t.Fatalf("Set returned %q, want render", got)
	}

	tests := []struct {
		name string
		key  key
		want bool
	}{
		{name: "same inputs", key: initial, want: true},
		{name: "resize", key: key{width: 81, height: 24, version: 1}},
		{name: "state change", key: key{width: 80, height: 24, version: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := cache.Get(tt.key)
			if got != tt.want {
				t.Fatalf("cache hit = %t, want %t", got, tt.want)
			}
		})
	}

	cache.Invalidate()
	if _, ok := cache.Get(initial); ok {
		t.Fatal("invalidated cache reported a hit")
	}

	cache.Set(initial, "")
	if render, ok := cache.Get(initial); !ok || render != "" {
		t.Fatalf("empty render cache = %q, %t; want empty hit", render, ok)
	}
}
