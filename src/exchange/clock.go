package exchange

import (
	"sync"
	"time"
)

// Clock provides the time source for market data and paper orders.
// Tests use ManualClock; live adapters may use the wall clock.
type Clock interface {
	Now() time.Time
}

// WallClock uses time.Now.
type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now() }

// ManualClock is a deterministic clock for fake feeds and tests.
type ManualClock struct {
	mu sync.RWMutex
	t  time.Time
}

// NewManualClock starts at t (typically UTC).
func NewManualClock(t time.Time) *ManualClock {
	return &ManualClock{t: t.UTC()}
}

func (c *ManualClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.t
}

// Set jumps the clock to t.
func (c *ManualClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t.UTC()
}

// Advance moves the clock forward by d.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
