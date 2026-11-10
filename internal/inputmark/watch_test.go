package inputmark

import (
	"slices"
	"sync"
	"testing"
	"time"
)

func TestClickEnded(t *testing.T) {
	tests := []struct {
		name     string
		down, up Point
		want     bool
	}{
		{"same place", Point{10, 10}, Point{10, 10}, true},
		{"at the slop", Point{10, 10}, Point{13, 14}, true},
		{"just past the slop", Point{10, 10}, Point{15, 10.1}, false},
		{"drag", Point{10, 10}, Point{100, 10}, false},
	}
	for _, tt := range tests {
		if got := clickEnded(tt.down, tt.up); got != tt.want {
			t.Errorf("%s: clickEnded(%v, %v) = %v, want %v", tt.name, tt.down, tt.up, got, tt.want)
		}
	}
}

func TestClicks(t *testing.T) {
	var c Clicks
	if c.Release(Point{0, 0}) {
		t.Fatal("release without a press is a click")
	}
	c.Press(Point{5, 5})
	if !c.Release(Point{6, 6}) {
		t.Fatal("click not seen")
	}
	if c.Release(Point{6, 6}) {
		t.Fatal("second release counted again")
	}
	c.Press(Point{5, 5})
	if c.Release(Point{50, 5}) {
		t.Fatal("drag counted as a click")
	}
}

func TestStateObserve(t *testing.T) {
	var s state
	if s.observe(1, "a") {
		t.Fatal("baseline fired")
	}
	if s.observe(1, "a") {
		t.Fatal("no change fired")
	}
	if !s.observe(2, "a") {
		t.Fatal("app change did not fire")
	}
	if !s.observe(2, "b") {
		t.Fatal("input source change did not fire")
	}
	if s.observe(2, "b") {
		t.Fatal("same state fired again")
	}
}

// fakeSystem stands in for the notifications, the main thread, the
// frontmost app and the input source. Posted functions and callbacks run
// only when the test calls run, sourceChanged or activate, as the main
// thread would.
type fakeSystem struct {
	mu       sync.Mutex
	pid      int32
	src      Source
	ok       bool
	posted   []func()
	onSource func()
	onApp    func(int32)
	// subscribed counts the subscriptions made, and stops the stops.
	subscribed, stops int
}

func (f *fakeSystem) subscribe(onSource func(), onApp func(int32)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed++
	f.onSource, f.onApp = onSource, onApp
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.stops++
		f.onSource, f.onApp = nil, nil
	}
}

func (f *fakeSystem) post(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, fn)
}

func (f *fakeSystem) frontPID() (int32, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pid, f.ok
}

func (f *fakeSystem) source() (Source, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.src, f.ok
}

func (f *fakeSystem) set(pid int32, id string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pid, f.src, f.ok = pid, Source{ID: id}, ok
}

// take removes the posted functions and returns them.
func (f *fakeSystem) take() []func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	posted := f.posted
	f.posted = nil
	return posted
}

// run runs the posted functions, as the main thread would.
func (f *fakeSystem) run() {
	for _, fn := range f.take() {
		fn()
	}
}

// sourceChanged sets the input source to id and notifies of it, if
// subscribed.
func (f *fakeSystem) sourceChanged(id string) {
	f.mu.Lock()
	f.src = Source{ID: id}
	on := f.onSource
	f.mu.Unlock()
	if on != nil {
		on()
	}
}

// activate brings app pid to the front and notifies of it, if
// subscribed.
func (f *fakeSystem) activate(pid int32) {
	f.mu.Lock()
	f.pid = pid
	on := f.onApp
	f.mu.Unlock()
	if on != nil {
		on(pid)
	}
}

// report is one call of onChange.
type report struct {
	src Source
	t   Trigger
}

// newTestWatcher is a watcher of sys that sends what it shows on the
// returned channel.
func newTestWatcher(sys *fakeSystem) (*Watcher, chan report) {
	shown := make(chan report, 16)
	w := NewWatcher(func(src Source, t Trigger) { shown <- report{src, t} })
	w.sys = sys
	return w, shown
}

// fakeClock gives w a clock that stands still until the test moves the
// returned time.
func fakeClock(w *Watcher) *time.Time {
	clock := time.Unix(1000, 0)
	w.now = func() time.Time { return clock }
	return &clock
}

func shownNow(shown chan report) []report {
	var got []report
	for {
		select {
		case r := <-shown:
			got = append(got, r)
		default:
			return got
		}
	}
}

// wantShown fails the test unless what shown holds now are the input
// sources of the given IDs, in order.
func wantShown(t *testing.T, shown chan report, want ...string) {
	t.Helper()
	var got []string
	for _, r := range shownNow(shown) {
		got = append(got, r.src.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("shown = %v, want %v", got, want)
	}
}

func TestFirstReadingIsBaseline(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	wantShown(t, shown)
	sys.sourceChanged("b")
	wantShown(t, shown, "b")
}

func TestEventBeforeBaselineIsBaseline(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	// A notification may land before the posted baseline read.
	sys.sourceChanged("b")
	sys.run()
	wantShown(t, shown)
}

func TestSourceChangeShowsOnlyOnRealChange(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	// A notification whose input source reads as before shows nothing.
	sys.sourceChanged("a")
	wantShown(t, shown)
	sys.sourceChanged("b")
	sys.sourceChanged("b")
	wantShown(t, shown, "b")
}

func TestAppActivationShowsOnlyOnNewApp(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	sys.activate(1)
	wantShown(t, shown)
	sys.activate(2)
	wantShown(t, shown, "a")
}

func TestFailedReadShowsNothing(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	sys.set(1, "a", false)
	sys.activate(2)
	sys.sourceChanged("b")
	w.Click(Point{1, 2})
	sys.run()
	wantShown(t, shown)
}

func TestClickShows(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	w.Click(Point{1, 2})
	wantShown(t, shown)
	sys.run()
	wantShown(t, shown, "a")
}

func TestClickBeforeBaselineShowsNothing(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	// The click's read lands before the baseline's, and is the baseline.
	baseline := sys.take()
	w.Click(Point{1, 2})
	sys.run()
	for _, fn := range baseline {
		fn()
	}
	wantShown(t, shown)
}

func TestClickWhileDisabledDoesNothing(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.Click(Point{1, 2})
	sys.run()
	wantShown(t, shown)
}

func TestStaleReadingsDropped(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	onSource, onApp := sys.onSource, sys.onApp
	w.Click(Point{1, 2})
	w.Stop()
	// The click's read lands after the disable, and so do callbacks
	// already on their way.
	sys.run()
	sys.set(1, "b", true)
	onSource()
	onApp(2)
	wantShown(t, shown)
	// Nor do they count for a new subscription, whose baseline is new.
	w.SetEnabled(true)
	sys.set(1, "c", true)
	onSource()
	sys.run()
	wantShown(t, shown)
	sys.sourceChanged("d")
	wantShown(t, shown, "d")
	w.Stop()
}

func TestSetEnabledIsIdempotent(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, _ := newTestWatcher(sys)
	w.SetEnabled(true)
	w.SetEnabled(true)
	if sys.subscribed != 1 {
		t.Errorf("subscribed %d times, want 1", sys.subscribed)
	}
	w.Stop()
	w.Stop()
	w.SetEnabled(false)
	if sys.stops != 1 {
		t.Errorf("stopped %d times, want 1", sys.stops)
	}
}

func TestSetEnabledAndClickDoNotWaitOnMain(t *testing.T) {
	// fakeSystem runs nothing posted until run, as a main thread
	// waiting on the caller.
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w := NewWatcher(func(Source, Trigger) {})
	w.sys = sys
	returned := make(chan struct{})
	go func() {
		w.SetEnabled(true)
		w.Click(Point{1, 2})
		w.Stop()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("SetEnabled, Click or Stop waited on the main thread")
	}
}

func TestCallbackMayCallWatcher(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	var w *Watcher
	w = NewWatcher(func(Source, Trigger) { w.SetEnabled(false) })
	w.sys = sys
	w.SetEnabled(true)
	sys.run()
	done := make(chan struct{})
	go func() {
		sys.sourceChanged("b")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onChange calling back into the watcher deadlocked")
	}
}

// wantTriggers fails the test unless the triggers shown holds now are
// want, in order.
func wantTriggers(t *testing.T, shown chan report, want ...Trigger) {
	t.Helper()
	var got []Trigger
	for _, r := range shownNow(shown) {
		got = append(got, r.t)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("triggers = %+v, want %+v", got, want)
	}
}

func TestTriggers(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	sys.activate(2)
	wantTriggers(t, shown, Trigger{Kind: Activate, PID: 2})
	sys.sourceChanged("b")
	wantTriggers(t, shown, Trigger{Kind: SourceChange, PID: 2})
	w.Click(Point{30, 40})
	sys.run()
	wantTriggers(t, shown, Trigger{Kind: Click, Click: Point{30, 40}, PID: 2})
}

func TestClickSeeingNewAppShowsOnlyClick(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	// The click's read lands before the activation's notification.
	w.Click(Point{5, 6})
	sys.set(2, "a", true)
	sys.run()
	wantTriggers(t, shown, Trigger{Kind: Click, Click: Point{5, 6}, PID: 2})
	// The notification, after, sees no change.
	sys.activate(2)
	wantShown(t, shown)
}

func TestClickSeeingNewSourceShowsBoth(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	w.SetEnabled(true)
	sys.run()
	w.Click(Point{5, 6})
	sys.set(1, "b", true)
	sys.run()
	wantTriggers(t, shown,
		Trigger{Kind: SourceChange, PID: 1},
		Trigger{Kind: Click, Click: Point{5, 6}, PID: 1})
}

func TestActivationWithinWindowAfterClickIsIgnored(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	clock := fakeClock(w)
	w.SetEnabled(true)
	sys.run()
	// The activation's notification lands before the click's read.
	w.Click(Point{5, 6})
	*clock = clock.Add(clickActivateWindow - time.Millisecond)
	sys.activate(2)
	wantShown(t, shown)
	sys.run()
	wantTriggers(t, shown, Trigger{Kind: Click, Click: Point{5, 6}, PID: 2})
}

func TestActivationOnPressBeforeReleaseIsIgnored(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	clock := fakeClock(w)
	w.SetEnabled(true)
	sys.run()
	// macOS activates the app on the press, before the release.
	w.Press()
	*clock = clock.Add(100 * time.Millisecond)
	sys.activate(2)
	wantShown(t, shown)
	*clock = clock.Add(100 * time.Millisecond)
	w.Click(Point{5, 6})
	sys.run()
	wantTriggers(t, shown, Trigger{Kind: Click, Click: Point{5, 6}, PID: 2})
}

func TestActivationAfterWindowShows(t *testing.T) {
	sys := &fakeSystem{pid: 1, src: Source{ID: "a"}, ok: true}
	w, shown := newTestWatcher(sys)
	clock := fakeClock(w)
	w.SetEnabled(true)
	sys.run()
	w.Click(Point{5, 6})
	sys.run()
	wantTriggers(t, shown, Trigger{Kind: Click, Click: Point{5, 6}, PID: 1})
	*clock = clock.Add(clickActivateWindow)
	sys.activate(2)
	wantTriggers(t, shown, Trigger{Kind: Activate, PID: 2})
}

func TestReportedFirst(t *testing.T) {
	var r Reported
	if !r.First("a") || r.First("a") || !r.First("b") {
		t.Fatal("First did not report each ID once")
	}
}
