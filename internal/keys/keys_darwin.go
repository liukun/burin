//go:build darwin

// Package keys posts synthetic keystrokes with CGEventPost.
package keys

import (
	"fmt"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

const (
	keycodeDelete = 51
	keycodeV      = 9

	sessionEventTap          = 1
	annotatedSessionEventTap = 2
	eventFlagMaskCommand     = 1 << 20
	eventSourceUserData      = 42
)

// SyntheticMark is the kCGEventSourceUserData of the keys Stroke posts,
// so that burin's own event tap can tell them from typed ones.
const SyntheticMark = 0x4255524E // "BURN"

var (
	cgEventCreateKeyboardEvent  func(source uintptr, keycode uint16, keyDown bool) uintptr
	cgEventSetFlags             func(event uintptr, flags uint64)
	cgEventSetIntegerValueField func(event uintptr, field uint32, value int64)
	cgEventPost                 func(tap uint32, event uintptr)
	cfRelease                   func(obj uintptr)
	loadOnce                    sync.Once
)

func load() {
	loadOnce.Do(func() {
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			panic(fmt.Sprintf("keys: %v", err))
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			panic(fmt.Sprintf("keys: %v", err))
		}
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventSetFlags, cg, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventSetIntegerValueField, cg, "CGEventSetIntegerValueField")
		purego.RegisterLibFunc(&cgEventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	})
}

func postKey(keycode uint16, keyDown bool, flags uint64) {
	post(keycode, keyDown, flags, false)
}

// postMarked posts a key event with exactly flags, marked with
// SyntheticMark. Unlike postKey, it posts before the session event taps,
// so burin's own tap sees it: the leader skips it by its mark, and the
// expander takes it as typed.
func postMarked(keycode uint16, keyDown bool, flags uint64) {
	post(keycode, keyDown, flags, true)
}

func post(keycode uint16, keyDown bool, flags uint64, marked bool) {
	ev := cgEventCreateKeyboardEvent(0, keycode, keyDown)
	if ev == 0 {
		return
	}
	// A marked event carries exactly flags, even none.
	setFlags := flags != 0 || marked
	if setFlags {
		cgEventSetFlags(ev, flags)
	}
	tap := uint32(annotatedSessionEventTap)
	if marked {
		cgEventSetIntegerValueField(ev, eventSourceUserData, SyntheticMark)
		tap = sessionEventTap
	}
	cgEventPost(tap, ev)
	cfRelease(ev)
}

// Stroke posts a press of keycode with flags, marked with SyntheticMark.
func Stroke(keycode int64, flags uint64) {
	load()
	postMarked(uint16(keycode), true, flags)
	postMarked(uint16(keycode), false, flags)
}

// Backspaces posts n presses of Delete.
func Backspaces(n int) {
	load()
	for range n {
		postKey(keycodeDelete, true, 0)
		postKey(keycodeDelete, false, 0)
		time.Sleep(2 * time.Millisecond)
	}
}

// CmdV posts Cmd+V.
func CmdV() {
	load()
	postKey(keycodeV, true, eventFlagMaskCommand)
	postKey(keycodeV, false, eventFlagMaskCommand)
}
