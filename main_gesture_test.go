package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"burin/internal/expander"
	"burin/internal/gesture"
	"burin/internal/hud"
	"burin/internal/keys"
	"burin/internal/workspace"
)

// fakeIO stands in for the event tap. Events are small integers with the
// properties set in its maps; copies are handles from 1000 on, which must
// each be posted or released exactly once. It also stands in for the HUD
// and records what it was asked to show.
type fakeIO struct {
	t        *testing.T
	button   map[uintptr]int64
	user     map[uintptr]int64
	key      map[uintptr]expander.Key
	loc      map[uintptr][2]float64
	failCopy map[uintptr]bool
	front    workspace.App
	hud      []string

	next  uintptr
	live  map[uintptr]uintptr // copy -> event it copies
	calls []string
}

func newFakeIO(t *testing.T) *fakeIO {
	return &fakeIO{
		t:        t,
		button:   map[uintptr]int64{},
		user:     map[uintptr]int64{},
		key:      map[uintptr]expander.Key{},
		loc:      map[uintptr][2]float64{},
		failCopy: map[uintptr]bool{},
		next:     1000,
		live:     map[uintptr]uintptr{},
	}
}

func (f *fakeIO) eventIO() eventIO {
	return eventIO{
		userData:    func(ev uintptr) int64 { return f.user[ev] },
		key:         func(ev uintptr) expander.Key { return f.key[ev] },
		mouseButton: func(ev uintptr) int64 { return f.button[ev] },
		location: func(ev uintptr) (float64, float64) {
			l := f.loc[ev]
			return l[0], l[1]
		},
		copyEvent: func(ev uintptr) uintptr {
			if f.failCopy[ev] {
				return 0
			}
			f.next++
			f.live[f.next] = ev
			return f.next
		},
		postMarked: func(mark int64, events ...uintptr) {
			if mark != keys.SyntheticMark {
				f.t.Errorf("posted with mark %#x", mark)
			}
			for _, h := range events {
				if h == 0 {
					continue
				}
				f.settle("post", h)
			}
		},
		releaseEvent: func(h uintptr) {
			if h == 0 {
				return
			}
			f.settle("release", h)
		},
		frontApp: func() workspace.App { return f.front },
	}
}

// hudFuncs sets b's gesture compass to stand-ins: a show is recorded as
// its directions and the press, "over" the app if named, a hide as "hide".
func (f *fakeIO) hudFuncs(b *burin) {
	b.showGestureHUD = func(d hud.Directions, app string, x, y float64) {
		s := fmt.Sprintf("%+v@%v,%v", d, x, y)
		if app != "" {
			s += " over " + app
		}
		f.hud = append(f.hud, s)
	}
	b.hideGestureHUD = func() { f.hud = append(f.hud, "hide") }
}

func (f *fakeIO) checkHUD(want ...string) {
	f.t.Helper()
	if !reflect.DeepEqual(f.hud, want) {
		f.t.Errorf("hud = %q, want %q", f.hud, want)
	}
}

// settle records that copy h was posted or released, which may happen
// only once; the call names the event it copies.
func (f *fakeIO) settle(what string, h uintptr) {
	ev, ok := f.live[h]
	if !ok {
		f.t.Errorf("%s of %d, which is not a live copy", what, h)
		return
	}
	delete(f.live, h)
	f.calls = append(f.calls, what+" "+eventName(ev))
}

func eventName(ev uintptr) string {
	return map[uintptr]string{rightDown: "rightDown", rightUp: "rightUp", middleDown: "middleDown", middleUp: "middleUp"}[ev]
}

// checkSettled fails when a copy was neither posted nor released.
func (f *fakeIO) checkSettled() {
	f.t.Helper()
	if len(f.live) != 0 {
		f.t.Errorf("copies left: %v", f.live)
	}
}

func (f *fakeIO) checkCalls(want ...string) {
	f.t.Helper()
	if len(want) == 0 {
		want = nil
	}
	if !reflect.DeepEqual(f.calls, want) {
		f.t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

// Events of the tests.
const (
	rightDown uintptr = iota + 1
	rightDrag
	rightUp
	middleDown
	middleUp
	marked
)

// newGestureBurin returns a burin with right-button gestures: left runs
// "back" in every app, up runs only in Safari. The right press is at
// 500, 500. The buttons have no hold delay, so a press shows the compass
// at once; withHoldDelay gives the right button one.
func newGestureBurin(t *testing.T) (*burin, *fakeIO, *[]string) {
	f := newFakeIO(t)
	for _, ev := range []uintptr{rightDown, rightDrag, rightUp, marked} {
		f.button[ev] = int64(gesture.ButtonRight)
	}
	f.button[middleDown] = int64(gesture.ButtonMiddle)
	f.button[middleUp] = int64(gesture.ButtonMiddle)
	f.user[marked] = keys.SyntheticMark
	f.loc[rightDown] = [2]float64{500, 500}
	f.loc[rightUp] = f.loc[rightDown]
	var ran []string
	b := &burin{
		io:    f.eventIO(),
		spawn: func(fn func()) { fn() },
		runGesture: func(name, content string) error {
			ran = append(ran, content)
			return nil
		},
	}
	set := gesture.Set{Configs: []gesture.Config{
		{Button: gesture.ButtonRight, Rules: []gesture.Rule{
			{Direction: gesture.Left, Title: "Back", Content: "back"},
			{Direction: gesture.Up, Apps: []string{"Safari"}, Content: "safari up"},
		}},
		{Button: gesture.ButtonMiddle},
	}}
	b.gestures.Store(&set)
	f.hudFuncs(b)
	b.after = func(d time.Duration, fn func()) *time.Timer {
		t.Errorf("show scheduled after %v with no hold delay", d)
		return nil
	}
	return b, f, &ran
}

// fakeAfter stands in for time.AfterFunc: it records the delays and the
// callbacks, which a test fires, and returns real timers that would only
// fire, doing nothing, after an hour, so a test can tell whether they
// were stopped.
type fakeAfter struct {
	t      *testing.T
	delays []time.Duration
	fns    []func()
	timers []*time.Timer
}

func (a *fakeAfter) after(d time.Duration, fn func()) *time.Timer {
	timer := time.AfterFunc(time.Hour, func() {})
	a.t.Cleanup(func() { timer.Stop() })
	a.delays = append(a.delays, d)
	a.fns = append(a.fns, fn)
	a.timers = append(a.timers, timer)
	return timer
}

// stopped reports whether timer i was stopped, and stops it.
func (a *fakeAfter) stopped(i int) bool {
	return !a.timers[i].Stop()
}

// withHoldDelay gives b's right button a hold delay of d and schedules
// its shows on a fakeAfter.
func withHoldDelay(t *testing.T, b *burin, d time.Duration) *fakeAfter {
	set := *b.gestures.Load()
	set.Configs = append([]gesture.Config(nil), set.Configs...)
	for i := range set.Configs {
		if set.Configs[i].Button == gesture.ButtonRight {
			set.Configs[i].HoldDelay = d
		}
	}
	b.gestures.Store(&set)
	a := &fakeAfter{t: t}
	b.after = a.after
	return a
}

// drag sends a right drag far from the press, which must not count: the
// release's location alone tells the gesture.
func drag(b *burin, f *fakeIO) bool {
	f.loc[rightDrag] = [2]float64{900, 100}
	return b.handleGesture(gesture.EventRightMouseDragged, rightDrag)
}

// releaseBy places the right release dx, dy from the press.
func releaseBy(f *fakeIO, dx, dy float64) {
	down := f.loc[rightDown]
	f.loc[rightUp] = [2]float64{down[0] + dx, down[1] + dy}
}

func TestGesturePassesMarkedEvents(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	for _, et := range []uint32{gesture.EventRightMouseDown, gesture.EventRightMouseUp} {
		if b.handleGesture(et, marked) {
			t.Errorf("marked event %d consumed", et)
		}
	}
	// Key events are not the gestures' business.
	if b.handleGesture(10, rightDown) {
		t.Error("keyDown consumed")
	}
	f.checkCalls()
	f.checkSettled()
}

func TestGesturePassesUnconfiguredButton(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	f.button[rightDown] = 3
	if b.handleGesture(gesture.EventOtherMouseDown, rightDown) {
		t.Fatal("press of button 3 consumed")
	}
	if b.downCopy != 0 {
		t.Fatal("press of button 3 copied")
	}
	f.checkSettled()
}

func TestGesturePressCopyFails(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	f.failCopy[rightDown] = true
	if b.handleGesture(gesture.EventRightMouseDown, rightDown) {
		t.Fatal("press that cannot be copied consumed")
	}
	releaseBy(f, -50, 0)
	if drag(b, f) {
		t.Fatal("drag of an untracked press consumed")
	}
	if b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release of an untracked press consumed")
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
	f.checkCalls()
	f.checkSettled()
}

func TestGestureLastPressWins(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	if !b.handleGesture(gesture.EventOtherMouseDown, middleDown) {
		t.Fatal("middle press passed")
	}
	f.checkCalls("release rightDown")
	if b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release of the replaced press consumed")
	}
	if !b.handleGesture(gesture.EventOtherMouseUp, middleUp) {
		t.Fatal("middle release passed")
	}
	f.checkCalls("release rightDown", "post middleDown", "post middleUp")
	f.checkSettled()
}

func TestGestureClickReplays(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	if !b.handleGesture(gesture.EventRightMouseDown, rightDown) {
		t.Fatal("press passed")
	}
	if !drag(b, f) {
		t.Fatal("drag passed")
	}
	releaseBy(f, 5, 2)
	if !b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release passed")
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
	f.checkCalls("post rightDown", "post rightUp")
	f.checkSettled()
}

func TestGestureRunsRule(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, -40, 1)
	if !b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release passed")
	}
	if !reflect.DeepEqual(*ran, []string{"back"}) {
		t.Fatalf("ran %q", *ran)
	}
	f.checkCalls("release rightDown", "release rightUp")
	f.checkSettled()
}

func TestGestureEmptyRuleSwallowsClick(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	set := gesture.Set{Configs: []gesture.Config{
		{Button: gesture.ButtonRight, Rules: []gesture.Rule{
			{Direction: gesture.DownRight, Title: "Cancel", Content: ""},
		}},
	}}
	b.gestures.Store(&set)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, 50, 50)
	if !b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release passed")
	}
	// The rule runs its empty content, which does nothing, and the click
	// is not replayed.
	if !reflect.DeepEqual(*ran, []string{""}) {
		t.Fatalf("ran %q", *ran)
	}
	f.checkCalls("release rightDown", "release rightUp")
	f.checkSettled()
}

func TestGestureWithoutRuleReplays(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	f.front = workspace.App{Name: "Notes"}
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, 0, -50)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
	f.checkCalls("post rightDown", "post rightUp")
	f.checkSettled()

	// The same gesture over Safari has a rule.
	b, f, ran = newGestureBurin(t)
	f.front = workspace.App{Name: "Safari"}
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, 0, -50)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	if !reflect.DeepEqual(*ran, []string{"safari up"}) {
		t.Fatalf("ran %q", *ran)
	}
	f.checkSettled()
}

func TestGestureReleaseCopyFails(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	f.failCopy[rightUp] = true
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, -50, 0)
	if b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release that cannot be copied consumed")
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
	f.checkCalls("release rightDown")
	f.checkSettled()
	if b.downCopy != 0 {
		t.Fatal("downCopy kept")
	}
}

func TestGestureHUD(t *testing.T) {
	// A click: the rules for every app show around the press, then hide.
	b, f, _ := newGestureBurin(t)
	f.front = workspace.App{Name: "Notes"}
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500 over Notes")
	drag(b, f)
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500 over Notes")
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500 over Notes", "hide")

	// A gesture over Safari also shows its rule, untitled.
	b, f, _ = newGestureBurin(t)
	f.front = workspace.App{Name: "Safari"}
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, -50, 0)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	f.checkHUD("{Up:up Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500 over Safari", "hide")

	// A button without rules shows nothing: its press and release hide.
	b, f, _ = newGestureBurin(t)
	b.handleGesture(gesture.EventOtherMouseDown, middleDown)
	b.handleGesture(gesture.EventOtherMouseUp, middleUp)
	f.checkHUD("hide", "hide")

	// A release that cannot be copied hides too.
	b, f, _ = newGestureBurin(t)
	f.failCopy[rightUp] = true
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500", "hide")
}

func TestGestureHUDStaleShow(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	var pending []func()
	b.spawn = func(fn func()) { pending = append(pending, fn) }
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	// The show runs only after the release: it is dropped.
	for _, fn := range pending {
		fn()
	}
	f.checkHUD("hide")
	f.checkCalls("post rightDown", "post rightUp")
	f.checkSettled()
}

func TestGestureHUDStaleHide(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	var pending []func()
	b.spawn = func(fn func()) { pending = append(pending, fn) }
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	// The show of the second press runs before the hide of the first
	// release: the hide is dropped and the HUD stays up.
	last := len(pending) - 1
	pending[last]()
	for _, fn := range pending[:last] {
		fn()
	}
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500")
}

func TestGestureHUDNoHoldDelay(t *testing.T) {
	// newGestureBurin's after fails the test if a show is scheduled.
	b, f, _ := newGestureBurin(t)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500")
	if b.hudTimer != nil {
		t.Fatal("timer kept with no hold delay")
	}
}

func TestGestureHUDHoldDelay(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	a := withHoldDelay(t, b, 150*time.Millisecond)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	// Nothing shows until the button has been held for the delay.
	f.checkHUD()
	if !reflect.DeepEqual(a.delays, []time.Duration{150 * time.Millisecond}) {
		t.Fatalf("delays = %v", a.delays)
	}
	a.fns[0]()
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500")
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@500,500", "hide")
}

func TestGestureHUDReleaseBeforeHoldDelay(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	a := withHoldDelay(t, b, 150*time.Millisecond)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	releaseBy(f, -50, 0)
	b.handleGesture(gesture.EventRightMouseUp, rightUp)
	if !a.stopped(0) {
		t.Error("release left the timer running")
	}
	if b.hudTimer != nil {
		t.Error("timer kept after the release")
	}
	// A timer that fired all the same is dropped: nothing stays up.
	a.fns[0]()
	f.checkHUD("hide")
	// The gesture does not wait for the compass.
	if !reflect.DeepEqual(*ran, []string{"back"}) {
		t.Fatalf("ran %q", *ran)
	}
	f.checkSettled()
}

func TestGestureHUDPressBeforeHoldDelay(t *testing.T) {
	b, f, _ := newGestureBurin(t)
	a := withHoldDelay(t, b, 150*time.Millisecond)
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	f.loc[rightDown] = [2]float64{600, 600}
	b.handleGesture(gesture.EventRightMouseDown, rightDown)
	if !a.stopped(0) {
		t.Error("second press left the first timer running")
	}
	// The first press's show is stale; the second's shows at its press.
	a.fns[0]()
	f.checkHUD()
	a.fns[1]()
	f.checkHUD("{Up: Down: Left:Back Right: UpLeft: UpRight: DownLeft: DownRight:}@600,600")
}

// fakeTap is a running tap whose Stop succeeds or not.
type fakeTap struct {
	mask    uint64
	stopOK  bool
	stopped int
}

func (t *fakeTap) Stop() bool {
	t.stopped++
	return t.stopOK
}

// tapStarter starts fakeTaps, failing for the masks in fail.
type tapStarter struct {
	fail    map[uint64]bool
	started []uint64
}

func (s *tapStarter) start(mask uint64) (stopper, error) {
	s.started = append(s.started, mask)
	if s.fail[mask] {
		return nil, errors.New("tap failed")
	}
	return &fakeTap{mask: mask, stopOK: true}, nil
}

func newTapBurin(t *testing.T, oldMask uint64, stopOK bool, fail map[uint64]bool) (*burin, *fakeTap, *tapStarter, *fakeIO) {
	f := newFakeIO(t)
	s := &tapStarter{fail: fail}
	old := &fakeTap{mask: oldMask, stopOK: stopOK}
	b := &burin{io: f.eventIO(), spawn: func(fn func()) { fn() }, startTap: s.start, tap: old, mouseMask: oldMask}
	f.hudFuncs(b)
	return b, old, s, f
}

func TestSetMouseMaskUnchanged(t *testing.T) {
	b, old, s, _ := newTapBurin(t, 0x18, true, nil)
	b.setMouseMask(0x18)
	if old.stopped != 0 || len(s.started) != 0 || b.tap != old {
		t.Fatalf("rebuilt: stopped %d, started %v", old.stopped, s.started)
	}
}

func TestSetMouseMaskBeforeListen(t *testing.T) {
	b, _, s, _ := newTapBurin(t, 0, true, nil)
	b.tap = nil
	b.setMouseMask(0x18)
	if b.mouseMask != 0x18 || len(s.started) != 0 || b.tap != nil {
		t.Fatalf("mask %#x, started %v", b.mouseMask, s.started)
	}
}

func TestSetMouseMaskRebuilds(t *testing.T) {
	b, old, s, f := newTapBurin(t, 0, true, nil)
	// A press tracked by the old tap.
	f.button[rightDown] = int64(gesture.ButtonRight)
	set := gesture.Set{Configs: []gesture.Config{{Button: gesture.ButtonRight}}}
	b.gestures.Store(&set)
	b.spawn = func(fn func()) { fn() }
	b.handleGesture(gesture.EventRightMouseDown, rightDown)

	b.setMouseMask(0x58)
	if old.stopped != 1 || !reflect.DeepEqual(s.started, []uint64{0x58}) {
		t.Fatalf("stopped %d, started %v", old.stopped, s.started)
	}
	if tap, _ := b.tap.(*fakeTap); tap == nil || tap.mask != 0x58 || b.mouseMask != 0x58 {
		t.Fatalf("tap %+v, mask %#x", b.tap, b.mouseMask)
	}
	f.checkCalls("release rightDown")
	f.checkSettled()
	if b.downCopy != 0 {
		t.Fatal("downCopy kept")
	}
	// The press showed no gestures, as the button has no rules, and hid
	// any; the rebuild hides the compass all the same.
	f.checkHUD("hide", "hide")
	// The recognizer forgot the press: its release passes.
	if b.handleGesture(gesture.EventRightMouseUp, rightUp) {
		t.Fatal("release after the rebuild consumed")
	}
}

func TestSetMouseMaskStopTimesOut(t *testing.T) {
	b, old, s, _ := newTapBurin(t, 0x18, false, nil)
	b.setMouseMask(0x58)
	if b.mouseMask != 0x18 || b.tap != old || len(s.started) != 0 {
		t.Fatalf("mask %#x, started %v", b.mouseMask, s.started)
	}
}

func TestSetMouseMaskStartFails(t *testing.T) {
	b, _, s, _ := newTapBurin(t, 0x18, true, map[uint64]bool{0x58: true})
	b.setMouseMask(0x58)
	if !reflect.DeepEqual(s.started, []uint64{0x58, 0x18}) {
		t.Fatalf("started %v", s.started)
	}
	if tap, _ := b.tap.(*fakeTap); tap == nil || tap.mask != 0x18 || b.mouseMask != 0x18 {
		t.Fatalf("tap %+v, mask %#x", b.tap, b.mouseMask)
	}

	// Both fail: no tap.
	b, _, s, _ = newTapBurin(t, 0x18, true, map[uint64]bool{0x58: true, 0x18: true})
	b.setMouseMask(0x58)
	if b.tap != nil || len(s.started) != 2 {
		t.Fatalf("tap %+v, started %v", b.tap, s.started)
	}
}

func TestGestureDirections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []gesture.Rule
		want  hud.Directions
	}{
		{"none", nil, hud.Directions{}},
		{"untitled", []gesture.Rule{{Direction: gesture.Down}}, hud.Directions{Down: "down"}},
		{"first wins", []gesture.Rule{
			{Direction: gesture.Left, Title: "Back", Apps: []string{"Safari"}},
			{Direction: gesture.Left, Title: "Previous"},
			{Direction: gesture.Up, Title: "Mission Control"},
			{Direction: gesture.Right},
		}, hud.Directions{Up: "Mission Control", Left: "Back", Right: "right"}},
		{"diagonals", []gesture.Rule{
			{Direction: gesture.UpLeft, Title: "Close"},
			{Direction: gesture.DownRight},
		}, hud.Directions{UpLeft: "Close", DownRight: "down-right"}},
	} {
		if got := gestureDirections(tc.rules); got != tc.want {
			t.Errorf("%s: gestureDirections = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
