//go:build darwin

// Package eventtap runs an active CGEventTap on a dedicated CFRunLoop
// thread. Being active,
// the tap can consume events so they never reach the focused app.
package eventtap

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	EventLeftMouseDown = 1
	EventLeftMouseUp   = 2
	EventKeyDown       = 10
	EventKeyUp         = 11
	EventFlagsChanged  = 12

	eventTapDisabledByTimeout   = 0xFFFFFFFE
	eventTapDisabledByUserInput = 0xFFFFFFFF

	stopRetryInterval = 50 * time.Millisecond
	stopTimeout       = 2 * time.Second

	sessionEventTap      = 1
	headInsertEventTap   = 0
	tapOptionDefault     = 0
	keyboardEventKeycode = 9
	eventSourceUserData  = 42
	keyboardEventRepeat  = 8
	mouseEventButton     = 3
	unicodeBufSize       = 16
)

// MaskBit is CGEventMaskBit.
func MaskBit(eventType uint32) uint64 { return 1 << eventType }

// Handler receives every tapped event on the tap's run loop thread and
// returns true to consume it, so it is not delivered any further. The tap
// is active, so input waits on the handler: it must return quickly, or
// macOS disables the tap by timeout (the tap re-enables itself).
type Handler func(eventType uint32, event uintptr) bool

// Tap is an active CGEventTap. The zero value is ready to Start.
type Tap struct {
	mu      sync.Mutex
	handler Handler
	handle  uintptr
	port    uintptr
	source  uintptr
	loop    uintptr
	done    chan struct{}
}

var (
	cgEventTapCreate                func(tap, place, options uint32, mask uint64, callback, userInfo uintptr) uintptr
	cgEventTapEnable                func(port uintptr, enable bool)
	cgEventGetIntegerValueField     func(event uintptr, field uint32) int64
	cgEventGetFlags                 func(event uintptr) uint64
	cgEventGetLocation              func(event uintptr) cgPoint
	cgEventKeyboardGetUnicodeString func(event uintptr, maxLen uintptr, actualLen *uintptr, buf *uint16)
	cgEventCreateCopy               func(event uintptr) uintptr
	cgEventSetIntegerValueField     func(event uintptr, field uint32, value int64)
	cgEventPost                     func(tap uint32, event uintptr)
	cfMachPortCreateRunLoopSource   func(alloc, port uintptr, order int) uintptr
	cfMachPortInvalidate            func(port uintptr)
	cfRunLoopGetCurrent             func() uintptr
	cfRunLoopAddSource              func(loop, source, mode uintptr)
	cfRunLoopRun                    func()
	cfRunLoopStop                   func(loop uintptr)
	cfRelease                       func(obj uintptr)
	cfDictionaryCreate              func(alloc uintptr, keys, values *uintptr, n int, keyCallBacks, valueCallBacks uintptr) uintptr
	axIsProcessTrustedWithOptions   func(options uintptr) bool
	kCFRunLoopDefaultMode           uintptr
	kCFBooleanTrue                  uintptr
	kCFBooleanFalse                 uintptr
	kCFTypeDictionaryKeyCallBacks   uintptr
	kCFTypeDictionaryValueCallBacks uintptr
	kAXTrustedCheckOptionPrompt     uintptr
	loadOnce                        sync.Once
	callbackOnce                    sync.Once
	callbackPtr                     uintptr
	tapsMu                          sync.Mutex
	taps                            = map[uintptr]*Tap{}
	nextHandle                      uintptr
)

func mustDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Sprintf("eventtap: load %s: %v", path, err))
	}
	return h
}

func symbol(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(fmt.Sprintf("eventtap: %v", err))
	}
	return p
}

// globalValue reads a framework global such as a CFStringRef constant.
func globalValue(lib uintptr, name string) uintptr {
	p := symbol(lib, name)
	return **(**uintptr)(unsafe.Pointer(&p))
}

func load() {
	loadOnce.Do(func() {
		cf := mustDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		cg := mustDlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		as := mustDlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
		purego.RegisterLibFunc(&cgEventTapCreate, cg, "CGEventTapCreate")
		purego.RegisterLibFunc(&cgEventTapEnable, cg, "CGEventTapEnable")
		purego.RegisterLibFunc(&cgEventGetIntegerValueField, cg, "CGEventGetIntegerValueField")
		purego.RegisterLibFunc(&cgEventGetFlags, cg, "CGEventGetFlags")
		purego.RegisterLibFunc(&cgEventGetLocation, cg, "CGEventGetLocation")
		purego.RegisterLibFunc(&cgEventKeyboardGetUnicodeString, cg, "CGEventKeyboardGetUnicodeString")
		purego.RegisterLibFunc(&cgEventCreateCopy, cg, "CGEventCreateCopy")
		purego.RegisterLibFunc(&cgEventSetIntegerValueField, cg, "CGEventSetIntegerValueField")
		purego.RegisterLibFunc(&cgEventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&cfMachPortCreateRunLoopSource, cf, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&cfMachPortInvalidate, cf, "CFMachPortInvalidate")
		purego.RegisterLibFunc(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
		purego.RegisterLibFunc(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfRunLoopRun, cf, "CFRunLoopRun")
		purego.RegisterLibFunc(&cfRunLoopStop, cf, "CFRunLoopStop")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfDictionaryCreate, cf, "CFDictionaryCreate")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
		kCFRunLoopDefaultMode = globalValue(cf, "kCFRunLoopDefaultMode")
		kCFBooleanTrue = globalValue(cf, "kCFBooleanTrue")
		kCFBooleanFalse = globalValue(cf, "kCFBooleanFalse")
		kCFTypeDictionaryKeyCallBacks = symbol(cf, "kCFTypeDictionaryKeyCallBacks")
		kCFTypeDictionaryValueCallBacks = symbol(cf, "kCFTypeDictionaryValueCallBacks")
		kAXTrustedCheckOptionPrompt = globalValue(as, "kAXTrustedCheckOptionPrompt")
	})
}

// sharedCallback is the only purego callback for all taps: purego callbacks
// are never freed, so taps are looked up by the userInfo handle instead.
func sharedCallback() uintptr {
	callbackOnce.Do(func() {
		callbackPtr = purego.NewCallback(dispatch)
	})
	return callbackPtr
}

func dispatch(proxy, eventType, event, userInfo uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("eventtap: callback panic: %v", r)
			// Active tap: returning NULL drops the key event. Only return 0
			// when the handler explicitly returns true; on panic pass the
			// original event through.
			ret = event
		}
	}()
	tapsMu.Lock()
	t := taps[userInfo]
	tapsMu.Unlock()
	if t == nil {
		return event
	}
	if t.handleEvent(uint32(eventType), event) {
		// Returning NULL from an active tap drops the event.
		return 0
	}
	return event
}

// handleEvent reports whether the event is consumed. Tap-disabled
// notifications are never consumed.
func (t *Tap) handleEvent(eventType uint32, event uintptr) bool {
	disabled := eventType == eventTapDisabledByTimeout || eventType == eventTapDisabledByUserInput
	if disabled {
		log.Printf("eventtap: tap disabled (type %#x), re-enabling", eventType)
		cgEventTapEnable(t.port, true)
		return false
	}
	return t.handler(eventType, event)
}

// Start creates the tap for the events in mask and runs it on a locked OS
// thread. It returns an error when the tap cannot be created, which usually
// means the Accessibility permission is missing.
func (t *Tap) Start(mask uint64, handler Handler) error {
	load()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done != nil {
		return errors.New("eventtap: already started")
	}

	tapsMu.Lock()
	nextHandle++
	t.handle = nextHandle
	t.handler = handler
	taps[t.handle] = t
	tapsMu.Unlock()

	ready := make(chan error, 1)
	done := make(chan struct{})
	go t.run(mask, ready, done)
	if err := <-ready; err != nil {
		t.unregister()
		return err
	}
	t.done = done
	return nil
}

func (t *Tap) run(mask uint64, ready chan<- error, done chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(done)

	port := cgEventTapCreate(sessionEventTap, headInsertEventTap, tapOptionDefault, mask, sharedCallback(), t.handle)
	if port == 0 {
		ready <- errors.New("eventtap: CGEventTapCreate failed; grant Accessibility and Input Monitoring in System Settings > Privacy & Security, then relaunch")
		return
	}
	t.port = port
	t.source = cfMachPortCreateRunLoopSource(0, port, 0)
	t.loop = cfRunLoopGetCurrent()
	cfRunLoopAddSource(t.loop, t.source, kCFRunLoopDefaultMode)
	cgEventTapEnable(port, true)
	ready <- nil
	cfRunLoopRun()
}

// Stop disables the tap, stops its run loop and releases it. It reports
// whether the tap is stopped: false when its run loop did not stop in
// time, and the tap may still run. A stopped tap may be started again.
func (t *Tap) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done == nil {
		return true
	}
	cgEventTapEnable(t.port, false)
	cfMachPortInvalidate(t.port)
	if !t.waitStopped() {
		log.Printf("eventtap: run loop did not stop within %v, giving up", stopTimeout)
		return false
	}
	t.done = nil
	t.unregister()
	cfRelease(t.source)
	cfRelease(t.port)
	t.port, t.source, t.loop = 0, 0, 0
	return true
}

// waitStopped stops the tap's run loop, retrying until the run loop thread
// exits or stopTimeout passes.
func (t *Tap) waitStopped() bool {
	deadline := time.After(stopTimeout)
	for {
		cfRunLoopStop(t.loop)
		select {
		case <-t.done:
			return true
		case <-deadline:
			return false
		case <-time.After(stopRetryInterval):
		}
	}
}

func (t *Tap) unregister() {
	tapsMu.Lock()
	delete(taps, t.handle)
	tapsMu.Unlock()
}

// Keycode returns the virtual keycode of a keyboard event.
func Keycode(event uintptr) int64 {
	return cgEventGetIntegerValueField(event, keyboardEventKeycode)
}

// Autorepeat reports whether a keyDown is the auto-repeat of a held key.
func Autorepeat(event uintptr) bool {
	return cgEventGetIntegerValueField(event, keyboardEventRepeat) != 0
}

// UserData returns the kCGEventSourceUserData of an event, which a
// poster may set to recognize its own events.
func UserData(event uintptr) int64 {
	return cgEventGetIntegerValueField(event, eventSourceUserData)
}

// Flags returns the modifier flags of an event.
func Flags(event uintptr) uint64 {
	return cgEventGetFlags(event)
}

// UnicodeString returns the text a keyboard event types.
func UnicodeString(event uintptr) string {
	var buf [unicodeBufSize]uint16
	var n uintptr
	cgEventKeyboardGetUnicodeString(event, unicodeBufSize, &n, &buf[0])
	n = min(n, unicodeBufSize)
	return string(utf16.Decode(buf[:n]))
}

// MouseButton returns the button number of a mouse event: 0 left, 1
// right, 2 middle, 3 and on the side buttons.
func MouseButton(event uintptr) int64 {
	return cgEventGetIntegerValueField(event, mouseEventButton)
}

// cgPoint is CGPoint, which purego returns by value on darwin arm64.
type cgPoint struct{ X, Y float64 }

// Location returns where the pointer was at a mouse event, in global
// display coordinates: the origin is the top left of the main display and
// y grows downwards.
func Location(event uintptr) (x, y float64) {
	p := cgEventGetLocation(event)
	return p.X, p.Y
}

// CopyEvent returns a copy of event that outlives the tap callback, or 0.
// The caller releases it with ReleaseEvent, or hands it to PostMarked.
func CopyEvent(event uintptr) uintptr {
	load()
	return cgEventCreateCopy(event)
}

// PostMarked posts events in order with their kCGEventSourceUserData set
// to mark, and releases them. They are posted at the session tap, so the
// taps of this process see them again and must pass events with mark on.
// Zero events are skipped.
func PostMarked(mark int64, events ...uintptr) {
	load()
	for _, ev := range events {
		if ev == 0 {
			continue
		}
		cgEventSetIntegerValueField(ev, eventSourceUserData, mark)
		cgEventPost(sessionEventTap, ev)
		cfRelease(ev)
	}
}

// ReleaseEvent releases an event from CopyEvent. Zero is ignored.
func ReleaseEvent(event uintptr) {
	if event == 0 {
		return
	}
	cfRelease(event)
}

// AccessibilityTrusted reports whether the process may tap keyboard events.
// With prompt, macOS shows its dialog pointing to System Settings when it
// may not.
func AccessibilityTrusted(prompt bool) bool {
	load()
	value := kCFBooleanFalse
	if prompt {
		value = kCFBooleanTrue
	}
	key := kAXTrustedCheckOptionPrompt
	opts := cfDictionaryCreate(0, &key, &value, 1, kCFTypeDictionaryKeyCallBacks, kCFTypeDictionaryValueCallBacks)
	defer cfRelease(opts)
	return axIsProcessTrustedWithOptions(opts)
}
