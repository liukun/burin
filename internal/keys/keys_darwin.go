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

	annotatedSessionEventTap = 2
	eventFlagMaskCommand     = 1 << 20
)

var (
	cgEventCreateKeyboardEvent func(source uintptr, keycode uint16, keyDown bool) uintptr
	cgEventSetFlags            func(event uintptr, flags uint64)
	cgEventPost                func(tap uint32, event uintptr)
	cfRelease                  func(obj uintptr)
	loadOnce                   sync.Once
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
		purego.RegisterLibFunc(&cgEventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	})
}

func postKey(keycode uint16, keyDown bool, flags uint64) {
	ev := cgEventCreateKeyboardEvent(0, keycode, keyDown)
	if ev == 0 {
		return
	}
	if flags != 0 {
		cgEventSetFlags(ev, flags)
	}
	cgEventPost(annotatedSessionEventTap, ev)
	cfRelease(ev)
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
