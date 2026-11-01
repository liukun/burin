//go:build darwin

package windowrules

import (
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const (
	// DefaultInterval is how often the engine scans: often enough that a
	// window dragged off its spot returns within seconds, rarely enough to
	// cost nothing on a steady desktop.
	DefaultInterval = 3 * time.Second

	// slowRound is how long a round may take before it is logged even
	// with nothing to do.
	slowRound = 10 * time.Millisecond
	// quietRounds is how many idle rounds go by between two timing lines,
	// so the log shows the steady-state cost without a line every scan.
	quietRounds = 100
	// stopTimeout bounds how long Stop waits for a round to finish: a
	// round asks the main thread for the displays, which may no longer
	// run once the app quits.
	stopTimeout = time.Second
)

// Engine applies a Config on its own goroutine, every interval. All
// CoreGraphics and Accessibility calls happen there, never on the event
// tap thread, and the engine runs no run loop of its own.
type Engine struct {
	mu   sync.Mutex
	cfg  Config
	stop chan struct{}
	done chan struct{}
	hold hold
}

// DisplaysChanged tells the engine the displays changed, so rounds hold
// for SettleDelay while macOS lays the windows out again. It is safe to
// call from any goroutine, before Start and after Stop.
func (e *Engine) DisplaysChanged() {
	e.hold.extend(time.Now(), SettleDelay)
	log.Printf("windowrules: displays changed, holding rounds for %v", SettleDelay)
}

// SetConfig replaces the rules from the next round on. The cache of
// Accessibility answers is kept: it describes windows, not rules.
func (e *Engine) SetConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
}

func (e *Engine) config() Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// Start runs a round now and then every interval until Stop. Starting a
// started engine does nothing.
func (e *Engine) Start(interval time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stop != nil {
		return
	}
	e.stop = make(chan struct{})
	e.done = make(chan struct{})
	go e.run(interval, e.stop, e.done)
}

// Stop ends the rounds, waiting for a running one up to stopTimeout.
func (e *Engine) Stop() {
	e.mu.Lock()
	stop, done := e.stop, e.done
	e.stop, e.done = nil, nil
	e.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(stopTimeout):
		log.Printf("windowrules: round did not finish within %v", stopTimeout)
	}
}

func (e *Engine) run(interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var cache Cache
	idle := 0
	for {
		cache = e.round(cache, &idle)
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// round scans once and moves the windows the rules want elsewhere. It
// returns the cache for the next round; idle counts the rounds since the
// last timing line.
func (e *Engine) round(cache Cache, idle *int) Cache {
	cfg := e.config()
	// Without rules there is nothing to keep, and nothing to scan.
	if cfg.Len() == 0 {
		return nil
	}
	start := time.Now()
	// The windows are in flux after a display change; the cache is kept,
	// bounds that changed drop their entries on the next round anyway.
	if e.hold.holding(start) {
		return cache
	}
	ds := displays()
	windows, missionControl, err := listWindows(ds)
	if err != nil {
		log.Printf("windowrules: %v", err)
		return cache
	}
	// Mission Control reports thumbnails as window bounds: a round now
	// would move and shrink windows that are in place.
	if missionControl {
		log.Printf("windowrules: Mission Control is up, round skipped")
		return cache
	}
	session := newAXSession()
	defer session.close()
	moves, next, stats := Plan(cfg, windows, ds, cache, session)
	for _, m := range moves {
		log.Printf("windowrules: %s", m)
		if err := session.setFrame(m.Window, m.To, m.PositionOnly); err != nil {
			log.Printf("windowrules: move %s window %d: %v", m.Window.Owner, m.Window.ID, err)
		}
	}
	elapsed := time.Since(start)
	*idle++
	busy := len(moves) > 0 || stats.Inspects > 0 || elapsed >= slowRound
	if busy || *idle >= quietRounds {
		log.Printf("windowrules: round %v, %d windows, %d inspected, %d moved",
			elapsed.Round(time.Microsecond), stats.Windows, stats.Inspects, len(moves))
		*idle = 0
	}
	return next
}

// displays returns the connected displays. mygo hops to the main thread
// for them and reports full frames as Bounds and usable areas as
// WorkArea, in the top-left-origin coordinates of CoreGraphics and
// Accessibility.
func displays() []Display {
	ds := mygo.Screen.Displays()
	out := make([]Display, 0, len(ds))
	for _, d := range ds {
		out = append(out, Display{Frame: rectOf(d.Bounds), Visible: rectOf(d.WorkArea)})
	}
	return out
}

func rectOf(r mygo.Rectangle) Rect {
	return Rect{X: float64(r.X), Y: float64(r.Y), W: float64(r.Width), H: float64(r.Height)}
}
