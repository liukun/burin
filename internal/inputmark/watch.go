// Package inputmark shows the current keyboard input source, so that the
// language typed in next is never a surprise: as a large, faint character
// in the middle of the display of the focused window when the frontmost
// app or the input source changes, and as a small one that follows the
// pointer after a left click. An app switch a click caused shows only the
// small one.
package inputmark

import (
	"math"
	"sync"
	"time"
)

// clickActivateWindow is how long after a left press or a click an app
// activation counts as caused by that click, and so shows no large mark:
// the click's small one is enough. The press counts too, as macOS
// activates the app on the press, often before the release. A press that
// ends as a drag, not a click, thus shows neither mark for the app it
// activated.
const clickActivateWindow = 400 * time.Millisecond

// ClickSlop is how far, in pixels, the pointer may move between a left
// press and its release for the two to be a click rather than a drag.
const ClickSlop = 5

// Point is a pointer location in CoreGraphics coordinates.
type Point struct{ X, Y float64 }

// clickEnded reports whether a left release at up ends a click pressed
// at down: one that moved no more than ClickSlop, as a drag, which
// selects text or moves a window, is no reason to show the input source.
func clickEnded(down, up Point) bool {
	return math.Hypot(up.X-down.X, up.Y-down.Y) <= ClickSlop
}

// Clicks tracks the left button's presses to tell clicks from drags. It
// is safe from any goroutine.
type Clicks struct {
	mu sync.Mutex
	// down is where the last press was, while pressed is set.
	down    Point
	pressed bool
}

// Press records a left press at p.
func (c *Clicks) Press(p Point) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.down, c.pressed = p, true
}

// Release reports whether a left release at p ends a click. A release
// without a press seen, as one whose press came before the tap started,
// is none.
func (c *Clicks) Release(p Point) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pressed {
		return false
	}
	c.pressed = false
	return clickEnded(c.down, p)
}

// Kind is what made the watcher report the input source.
type Kind int

const (
	// Click is a left click.
	Click Kind = iota
	// Activate is an app activated.
	Activate
	// SourceChange is the input source changed.
	SourceChange
)

// Trigger is what made the watcher report the input source, and where
// to show it: the point of a click, or the pid of the app whose focused
// window's display shows the rest.
type Trigger struct {
	Kind Kind
	// Click is where the click was, for Click.
	Click Point
	// PID is the app activated, for Activate, or the frontmost app read
	// with the input source, for SourceChange and Click.
	PID int32
}

// state is what the watcher last read, to tell a change.
type state struct {
	// primed is set once the first reading is the baseline.
	primed bool
	pid    int32
	id     string
}

// appChanged reports whether frontPID is another app than the last
// reading's. The baseline is none.
func (s *state) appChanged(frontPID int32) bool {
	return s.primed && frontPID != s.pid
}

// observe takes the frontmost app's pid and the input source ID read now
// and reports whether either changed. The first reading is the baseline
// and is no change.
func (s *state) observe(frontPID int32, sourceID string) bool {
	changed := s.primed && (frontPID != s.pid || sourceID != s.id)
	s.primed, s.pid, s.id = true, frontPID, sourceID
	return changed
}

// system is where the watcher learns of the frontmost app and the input
// source; tests stand in for it.
type system interface {
	// subscribe calls onSource each time the input source changes and
	// onApp with the pid of each app activated, on the main thread,
	// until stop is called. Neither it nor stop waits for the main
	// thread.
	subscribe(onSource func(), onApp func(pid int32)) (stop func())
	// post runs fn on the main thread later, without waiting for it.
	post(fn func())
	// frontPID and source read the frontmost app and the input source;
	// ok is false when they could not be read. Main thread only.
	frontPID() (pid int32, ok bool)
	source() (src Source, ok bool)
}

// Watcher watches the frontmost app and the input source while enabled
// and calls onChange, on the main thread, with the input source and what
// triggered it each time either changes, or a click is reported. Its
// methods are safe from any goroutine and never wait on the main thread,
// which may be waiting on the caller.
type Watcher struct {
	onChange func(Source, Trigger)
	sys      system

	// mu guards the rest.
	mu sync.Mutex
	// stop ends the subscription, nil while disabled.
	stop func()
	// gen counts the unsubscriptions; a reading made for an earlier
	// subscription is stale and dropped, as one may land on the main
	// thread after a disable or a new enable.
	gen uint64
	// st is what the current subscription last read.
	st state
	// lastPointer is when the last left press or click was reported.
	lastPointer time.Time
	// now is the clock, replaced in tests.
	now func() time.Time
}

// NewWatcher returns a disabled watcher of the system that calls
// onChange with the input source once enabled.
func NewWatcher(onChange func(Source, Trigger)) *Watcher {
	return &Watcher{onChange: onChange, sys: nativeSystem{}, now: time.Now}
}

// SetEnabled subscribes to or unsubscribes from the changes of the
// frontmost app and the input source; a repeated call does nothing. The
// first reading after enabling is the baseline, so it shows nothing for
// the app and input source at hand. It returns at once: the subscription
// and the baseline's reading are made on the main thread later.
func (w *Watcher) SetEnabled(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !enabled {
		w.unsubscribe()
		return
	}
	if w.stop != nil {
		return
	}
	w.st = state{}
	gen := w.gen
	w.stop = w.sys.subscribe(
		func() { w.readAndReport(gen, Trigger{Kind: SourceChange}) },
		func(pid int32) { w.appActivated(gen, pid) },
	)
	w.sys.post(func() { w.readAndReport(gen, Trigger{Kind: SourceChange}) })
}

// Stop unsubscribes, as SetEnabled(false).
func (w *Watcher) Stop() { w.SetEnabled(false) }

// unsubscribe ends the subscription, if any. w.mu is held.
func (w *Watcher) unsubscribe() {
	if w.stop == nil {
		return
	}
	w.stop()
	w.stop = nil
	w.gen++
}

// Press notes a left press, as the app it clicks on may be activated
// before the release. While disabled it does nothing.
func (w *Watcher) Press() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stop == nil {
		return
	}
	w.lastPointer = w.now()
}

// Click shows the input source after a click at p. The event tap reports
// it, and the input source is read on the main thread, so it only posts
// the read and the tap never waits. While disabled it does nothing.
func (w *Watcher) Click(p Point) {
	w.mu.Lock()
	enabled := w.stop != nil
	gen := w.gen
	if enabled {
		w.lastPointer = w.now()
	}
	w.mu.Unlock()
	if !enabled {
		return
	}
	w.sys.post(func() { w.readAndReport(gen, Trigger{Kind: Click, Click: p}) })
}

// appActivated takes the pid of an activated app, with the input source
// read now. Main thread only.
func (w *Watcher) appActivated(gen uint64, pid int32) {
	src, ok := w.sys.source()
	if !ok {
		return
	}
	w.report(gen, src, Trigger{Kind: Activate, PID: pid})
}

// readAndReport reads the frontmost app and the input source for the
// baseline or an input source change, which t is a SourceChange for, or
// for a click. Main thread only.
func (w *Watcher) readAndReport(gen uint64, t Trigger) {
	pid, ok := w.sys.frontPID()
	if !ok {
		return
	}
	src, ok := w.sys.source()
	if !ok {
		return
	}
	t.PID = pid
	w.report(gen, src, t)
}

// report takes a reading of subscription gen, triggered by t with the
// frontmost app t.PID, and calls onChange when the app or input source
// changed, or for a click, unless the reading is the baseline. A stale
// reading is dropped. onChange runs outside w.mu, so it may call back
// into the watcher.
//
// An app switch a click caused shows only the click's small mark: a
// click's reading that sees a new app reports just the click, and an
// activation within clickActivateWindow of a press or click is not
// reported. A click's reading that sees only the input source changed
// reports the change too, ahead of the click.
func (w *Watcher) report(gen uint64, src Source, t Trigger) {
	w.mu.Lock()
	if w.gen != gen {
		w.mu.Unlock()
		return
	}
	// observe reports no change for the baseline; a click must not show
	// it either.
	clicked := t.Kind == Click && w.st.primed
	appChanged := w.st.appChanged(t.PID)
	changed := w.st.observe(t.PID, src.ID)
	causedByClick := t.Kind == Activate && changed &&
		w.now().Sub(w.lastPointer) < clickActivateWindow
	w.mu.Unlock()

	if t.Kind == Click {
		sourceOnlyChanged := changed && !appChanged
		if sourceOnlyChanged {
			w.onChange(src, Trigger{Kind: SourceChange, PID: t.PID})
		}
		if clicked {
			w.onChange(src, t)
		}
		return
	}
	if changed && !causedByClick {
		w.onChange(src, t)
	}
}

// Reported remembers the input source IDs already logged, so each is
// logged once. It is safe from any goroutine.
type Reported struct {
	mu   sync.Mutex
	seen map[string]bool
}

// First reports whether id is new, and records it.
func (r *Reported) First(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[id] {
		return false
	}
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	r.seen[id] = true
	return true
}
