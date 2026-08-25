package tui

// renderCache stores a rendered component until one of its declared inputs
// changes. Component-specific keys keep invalidation dependencies explicit.
type renderCache[K comparable] struct {
	key    K
	render string
	valid  bool
}

func (c *renderCache[K]) Get(key K) (string, bool) {
	if c == nil || !c.valid || c.key != key {
		return "", false
	}
	return c.render, true
}

func (c *renderCache[K]) Set(key K, render string) string {
	c.key = key
	c.render = render
	c.valid = true
	return render
}

func (c *renderCache[K]) Invalidate() {
	if c == nil {
		return
	}
	c.render = ""
	c.valid = false
}
