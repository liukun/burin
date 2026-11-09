// Package copyflash flashes an emoji in the middle of the display under
// the pointer each time the pasteboard changes, so that a copy shows it
// landed: the emoji grows as it fades out.
package copyflash

import (
	"sync"
	"time"

	"burin/internal/pasteboard"
)

// PollInterval is how often the pasteboard's change count is read: the
// pasteboard tells no one of its changes.
const PollInterval = 200 * time.Millisecond

// counter decides which change counts are a change to announce.
type counter struct {
	// last is the change count last observed, the baseline at first.
	last int
	// ignored holds the change counts of burin's own writes, not to be
	// announced; several may be pending at once.
	ignored map[int]struct{}
}

// observe takes the change count read now and reports whether it is a
// change to announce.
func (c *counter) observe(count int) bool {
	if count == c.last {
		return false
	}
	c.last = count
	_, own := c.ignored[count]
	// Change counts only grow, so those up to count are stale.
	for n := range c.ignored {
		if n <= count {
			delete(c.ignored, n)
		}
	}
	return !own
}

// ignore records count as a change of burin's own.
func (c *counter) ignore(count int) {
	if c.ignored == nil {
		c.ignored = map[int]struct{}{}
	}
	c.ignored[count] = struct{}{}
}

// Watcher polls the pasteboard's change count while enabled and calls
// onChange, on its goroutine, for each change. Its methods are safe from
// any goroutine.
type Watcher struct {
	onChange func()
	// read returns the pasteboard's change count; tests stand in for it.
	read func() int

	// mu guards the counter and the polling.
	mu      sync.Mutex
	counter counter
	// stop stops the poller, nil while disabled; a poller whose stop is
	// no longer it is stale and stops.
	stop chan struct{}
}

// NewWatcher returns a disabled watcher that calls onChange for each
// change of the pasteboard once enabled.
func NewWatcher(onChange func()) *Watcher {
	return &Watcher{onChange: onChange, read: pasteboard.ChangeCount}
}

// SetEnabled starts or stops polling. Starting takes the change count
// now as the baseline, so it does not announce the copy before it.
func (w *Watcher) SetEnabled(enabled bool) {
	if !enabled {
		w.mu.Lock()
		w.stopPolling()
		w.mu.Unlock()
		return
	}
	// Read under the lock, which Own holds through its write, so the
	// baseline includes any write in progress and does not announce it.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stop != nil {
		return
	}
	w.counter = counter{last: w.read()}
	stop := make(chan struct{})
	w.stop = stop
	go w.poll(stop)
}

// Stop stops polling.
func (w *Watcher) Stop() { w.SetEnabled(false) }

// Own runs write, a pasteboard write of burin's own, and leaves the
// change it made unannounced. The write runs under the watcher's lock,
// so the poller cannot announce the change before it is recorded.
// While disabled it only runs write, as enabling takes a new baseline.
func (w *Watcher) Own(write func() (count int, ok bool)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	count, ok := write()
	if !ok || w.stop == nil {
		return
	}
	w.counter.ignore(count)
}

// stopPolling stops the poller, if any. w.mu is held.
func (w *Watcher) stopPolling() {
	if w.stop == nil {
		return
	}
	close(w.stop)
	w.stop = nil
}

// poll reads the change count every PollInterval until stop closes.
func (w *Watcher) poll(stop chan struct{}) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		count := w.read()
		w.mu.Lock()
		stale := w.stop != stop
		changed := !stale && w.counter.observe(count)
		w.mu.Unlock()
		if stale {
			return
		}
		if changed {
			w.onChange()
		}
	}
}
