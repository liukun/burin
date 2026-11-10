package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"burin/internal/eventtap"
	"burin/internal/gesture"
	"burin/internal/inputmark"
)

// Left button events of the tests.
const (
	leftDown uintptr = iota + 100
	leftUp
)

// withInputClicks gives b a stand-in for the watcher's Click and returns
// the clicks it was told of.
func withInputClicks(b *burin) *[]inputmark.Point {
	var clicks []inputmark.Point
	b.inputPress = func() {}
	b.inputClick = func(p inputmark.Point) { clicks = append(clicks, p) }
	return &clicks
}

func TestKeyMaskHasLeftButton(t *testing.T) {
	left := uint64(1<<eventtap.EventLeftMouseDown | 1<<eventtap.EventLeftMouseUp)
	if keyMask&left != left {
		t.Fatalf("keyMask %#x lacks the left button's press and release", keyMask)
	}
}

func TestHandleInputClick(t *testing.T) {
	tests := []struct {
		name   string
		up     [2]float64
		clicks int
	}{
		{"in place", [2]float64{100, 100}, 1},
		{"within the slop", [2]float64{103, 104}, 1},
		{"a drag", [2]float64{106, 100}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeIO(t)
			b := &burin{io: f.eventIO()}
			clicks := withInputClicks(b)
			presses := 0
			b.inputPress = func() { presses++ }
			f.loc[leftDown] = [2]float64{100, 100}
			f.loc[leftUp] = tt.up
			if b.handleInputClick(eventtap.EventLeftMouseDown, leftDown) {
				t.Error("press consumed")
			}
			if b.handleInputClick(eventtap.EventLeftMouseUp, leftUp) {
				t.Error("release consumed")
			}
			if presses != 1 {
				t.Errorf("watcher told of %d presses, want 1", presses)
			}
			if len(*clicks) != tt.clicks {
				t.Fatalf("clicks = %v, want %d", *clicks, tt.clicks)
			}
			// The click is where the button was released.
			if want := (inputmark.Point{X: tt.up[0], Y: tt.up[1]}); tt.clicks == 1 && (*clicks)[0] != want {
				t.Errorf("click at %v, want %v", (*clicks)[0], want)
			}
		})
	}
}

func TestHandleInputClickIgnoresOtherEvents(t *testing.T) {
	f := newFakeIO(t)
	b := &burin{io: f.eventIO()}
	clicks := withInputClicks(b)
	for _, et := range []uint32{eventtap.EventKeyDown, gesture.EventRightMouseDown, gesture.EventRightMouseUp} {
		if b.handleInputClick(et, rightDown) {
			t.Errorf("event %d consumed", et)
		}
	}
	if len(*clicks) != 0 {
		t.Fatalf("clicks = %v, want none", *clicks)
	}
}

// TestLeftClickPassesGestures sends a left click through the tap's chain
// with right and middle gestures configured: none of it is consumed, the
// gesture recognizer is left alone and the click reaches the mark. The
// leader, which reads the event's fields from CoreGraphics, is left out;
// leader's own tests cover its ignoring the left button.
func TestLeftClickPassesGestures(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	clicks := withInputClicks(b)
	f.loc[leftDown] = [2]float64{10, 10}
	f.loc[leftUp] = [2]float64{10, 10}
	before := b.recognizer
	tap := chain(b.handleGesture, b.handleExpander, b.handleInputClick)
	if tap(eventtap.EventLeftMouseDown, leftDown) || tap(eventtap.EventLeftMouseUp, leftUp) {
		t.Fatal("left click consumed")
	}
	if b.recognizer != before {
		t.Errorf("recognizer = %+v, want %+v", b.recognizer, before)
	}
	if len(*clicks) != 1 {
		t.Errorf("clicks = %v, want 1", *clicks)
	}
	if len(*ran) != 0 {
		t.Errorf("ran %v", *ran)
	}
	f.checkCalls()
	f.checkHUD()
	f.checkSettled()
}

// captureLog sends the log to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})
	return &buf
}

// shownMark is one mark shown: small at at, or big at center, found or
// not.
type shownMark struct {
	style  inputmark.Style
	small  bool
	at     inputmark.Point
	center mygo.Point
	found  bool
}

// newMarkBurin returns a burin with settings parsed from data, or none
// when data is nil, and the marks it shows. Its focused windows are the
// centers of pids in windows, and it runs what it spawns at once.
func newMarkBurin(data *string) (*burin, *[]shownMark) {
	var shown []shownMark
	b := &burin{spawn: func(fn func()) { fn() }}
	b.showSmallMark = func(s inputmark.Style, at inputmark.Point) {
		shown = append(shown, shownMark{style: s, small: true, at: at})
	}
	b.showBigMark = func(s inputmark.Style, center mygo.Point, found bool, _ uint64) {
		shown = append(shown, shownMark{style: s, center: center, found: found})
	}
	b.focusedCenter = func(pid int32) (mygo.Point, bool) {
		c, ok := windows[pid]
		return c, ok
	}
	if data != nil {
		s := inputmark.Parse(*data)
		b.inputSources.Store(&s)
	}
	return b, &shown
}

// windows are the centers of the focused windows of the test apps.
var windows = map[int32]mygo.Point{7: {X: 2000, Y: 300}}

// activated is the trigger of the test app of a focused window activated.
var activated = inputmark.Trigger{Kind: inputmark.Activate, PID: 7}

func TestShowInputMarkBeforeSettings(t *testing.T) {
	b, shown := newMarkBurin(nil)
	b.showInputMark(inputmark.Source{ID: "x", Name: "X"}, activated)
	if len(*shown) != 0 {
		t.Fatalf("shown %v before the settings loaded", *shown)
	}
}

func TestShowInputMarkHidden(t *testing.T) {
	data := "## hidden\n:show: no\n"
	b, shown := newMarkBurin(&data)
	src := inputmark.Source{ID: "hidden", Name: "H"}
	b.showInputMark(src, activated)
	b.showInputMark(src, inputmark.Trigger{Kind: inputmark.Click, Click: inputmark.Point{X: 1, Y: 2}})
	if len(*shown) != 0 {
		t.Fatalf("shown %v", *shown)
	}
}

func TestShowInputMarkStyled(t *testing.T) {
	data := "## pinyin\n:text: 拼\n:color: #ff0000\n"
	b, shown := newMarkBurin(&data)
	buf := captureLog(t)
	b.showInputMark(inputmark.Source{ID: "pinyin", Name: "Pinyin"}, activated)
	want := inputmark.Style{Text: "拼", Color: inputmark.Color{R: 255, A: 255}, Show: true}
	if len(*shown) != 1 || (*shown)[0].style != want {
		t.Fatalf("shown %v, want %v", *shown, want)
	}
	if buf.Len() != 0 {
		t.Errorf("logged %q for a configured source", buf.String())
	}
}

func TestShowInputMarkPlaces(t *testing.T) {
	data := ""
	b, shown := newMarkBurin(&data)
	src := inputmark.Source{ID: "x", Name: "X"}
	style := inputmark.Style{Text: "X", Color: inputmark.Color{R: 255, G: 255, B: 255, A: 255}, Show: true}
	tests := []struct {
		name string
		t    inputmark.Trigger
		want shownMark
	}{
		{
			"click beside it",
			inputmark.Trigger{Kind: inputmark.Click, Click: inputmark.Point{X: 10, Y: 20}, PID: 7},
			shownMark{style: style, small: true, at: inputmark.Point{X: 10, Y: 20}},
		},
		{"activation on its window's display", activated, shownMark{style: style, center: windows[7], found: true}},
		{
			"source change on the front app's window's display",
			inputmark.Trigger{Kind: inputmark.SourceChange, PID: 7},
			shownMark{style: style, center: windows[7], found: true},
		},
		{
			"app without a window under the pointer",
			inputmark.Trigger{Kind: inputmark.Activate, PID: 8},
			shownMark{style: style},
		},
	}
	for _, tt := range tests {
		*shown = nil
		b.showInputMark(src, tt.t)
		if len(*shown) != 1 || (*shown)[0] != tt.want {
			t.Errorf("%s: shown %+v, want %+v", tt.name, *shown, tt.want)
		}
	}
}

// deferSpawn makes b hold what it spawns, rather than run it, in the
// returned list.
func deferSpawn(b *burin) *[]func() {
	var pending []func()
	b.spawn = func(fn func()) { pending = append(pending, fn) }
	return &pending
}

func TestShowInputMarkReadsWindowOffMain(t *testing.T) {
	data := ""
	b, shown := newMarkBurin(&data)
	pending := deferSpawn(b)
	read := false
	b.focusedCenter = func(int32) (mygo.Point, bool) { read = true; return mygo.Point{}, false }
	b.showInputMark(inputmark.Source{ID: "x", Name: "X"}, activated)
	if read || len(*shown) != 0 {
		t.Fatalf("read the window or showed %v in the caller", *shown)
	}
	for _, fn := range *pending {
		fn()
	}
	if !read || len(*shown) != 1 {
		t.Fatalf("spawned read %v, shown %v", read, *shown)
	}
}

func TestShowInputMarkDropsStaleBigMarks(t *testing.T) {
	data := "## old\n:text: O\n## new\n:text: N\n"
	b, shown := newMarkBurin(&data)
	pending := deferSpawn(b)
	// An app activated with the input source it left, then the input
	// source macOS switched to for it.
	b.showInputMark(inputmark.Source{ID: "old", Name: "Old"}, activated)
	b.showInputMark(inputmark.Source{ID: "new", Name: "New"}, inputmark.Trigger{Kind: inputmark.SourceChange, PID: 7})
	// The first window read finishes last.
	for i := len(*pending) - 1; i >= 0; i-- {
		(*pending)[i]()
	}
	if len(*shown) != 1 || (*shown)[0].style.Text != "N" {
		t.Fatalf("shown %+v, want only N", *shown)
	}
}

func TestShowInputMarkClickLeavesBigMarkCurrent(t *testing.T) {
	data := ""
	b, shown := newMarkBurin(&data)
	pending := deferSpawn(b)
	src := inputmark.Source{ID: "x", Name: "X"}
	b.showInputMark(src, activated)
	b.showInputMark(src, inputmark.Trigger{Kind: inputmark.Click, Click: inputmark.Point{X: 1, Y: 2}})
	for _, fn := range *pending {
		fn()
	}
	if len(*shown) != 2 || !(*shown)[0].small || (*shown)[1].small {
		t.Fatalf("shown %+v, want the small mark then the big one", *shown)
	}
}

func TestShowInputMarkLogsUnconfiguredOnce(t *testing.T) {
	data := ""
	b, shown := newMarkBurin(&data)
	buf := captureLog(t)
	src := inputmark.Source{ID: "com.example.dvorak", Name: "Dvorak"}
	b.showInputMark(src, activated)
	b.showInputMark(src, activated)
	if got := strings.Count(buf.String(), "com.example.dvorak"); got != 1 {
		t.Errorf("logged %q, want the ID once", buf.String())
	}
	if !strings.Contains(buf.String(), "Dvorak") {
		t.Errorf("logged %q, want the name", buf.String())
	}
	if len(*shown) != 2 || (*shown)[0].style.Text != "D" {
		t.Fatalf("shown %v, want D twice", *shown)
	}
}
