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

	sessionEventTap      = 1
	eventFlagMaskCommand = 1 << 20
	eventSourceUserData  = 42
)

// SyntheticMark is the kCGEventSourceUserData of the keys Stroke posts,
// so that burin's own event tap can tell them from typed ones.
const SyntheticMark = 0x4255524E // "BURN"

// ExpansionMark is the kCGEventSourceUserData of the keys Backspaces and
// CmdV post for a snippet expansion. Burin's own event tap skips them
// all: the expander must not take them as typed.
const ExpansionMark = 0x42555258 // "BURX"

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

// keyPost is one key event to post.
type keyPost struct {
	keycode uint16
	keyDown bool
	flags   uint64
	mark    int64
}

// pressPosts is the keyDown and the keyUp of a press of keycode with
// exactly flags, marked with mark.
func pressPosts(keycode uint16, flags uint64, mark int64) [2]keyPost {
	return [2]keyPost{
		{keycode, true, flags, mark},
		{keycode, false, flags, mark},
	}
}

// post posts p at the session event tap, so that burin's own event tap
// sees it and skips or feeds it by its mark.
func post(p keyPost) {
	ev := cgEventCreateKeyboardEvent(0, p.keycode, p.keyDown)
	if ev == 0 {
		return
	}
	// The event carries exactly flags, even none.
	cgEventSetFlags(ev, p.flags)
	cgEventSetIntegerValueField(ev, eventSourceUserData, p.mark)
	cgEventPost(sessionEventTap, ev)
	cfRelease(ev)
}

func postAll(posts [2]keyPost) {
	for _, p := range posts {
		post(p)
	}
}

// Stroke posts a press of keycode with flags, marked with SyntheticMark.
func Stroke(keycode int64, flags uint64) {
	load()
	postAll(strokePosts(keycode, flags))
}

// Backspaces posts n presses of Delete, marked with ExpansionMark. They go
// to the session event tap because the system save panel loses keys posted
// at the annotated session event tap.
func Backspaces(n int) {
	load()
	for range n {
		postAll(backspacePosts())
		time.Sleep(2 * time.Millisecond)
	}
}

// CmdV posts Cmd+V, marked with ExpansionMark, at the session event tap
// for the same reason as Backspaces.
func CmdV() {
	load()
	postAll(cmdVPosts())
}

func strokePosts(keycode int64, flags uint64) [2]keyPost {
	return pressPosts(uint16(keycode), flags, SyntheticMark)
}

func backspacePosts() [2]keyPost {
	return pressPosts(keycodeDelete, 0, ExpansionMark)
}

func cmdVPosts() [2]keyPost {
	return pressPosts(keycodeV, eventFlagMaskCommand, ExpansionMark)
}
