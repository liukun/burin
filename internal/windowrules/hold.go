package windowrules

import (
	"sync"
	"time"
)

// SettleDelay is how long rounds hold after the displays change. macOS
// takes a few seconds to reconnect a display and lay the windows out
// again; a round in between would see the windows on the wrong display
// and clamp them to its size, and the clamp is never undone. A flaky
// cable fires the change several times in a row, and each one extends
// the hold.
const SettleDelay = 10 * time.Second

// hold is a deadline before which rounds do nothing. The zero value
// holds nothing.
type hold struct {
	mu    sync.Mutex
	until time.Time
}

// extend holds rounds until at least d from now; a later deadline is
// kept.
func (h *hold) extend(now time.Time, d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	until := now.Add(d)
	if until.After(h.until) {
		h.until = until
	}
}

// holding reports whether rounds still hold at now.
func (h *hold) holding(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return now.Before(h.until)
}
