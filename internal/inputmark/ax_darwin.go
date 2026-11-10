//go:build darwin

package inputmark

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

// Accessibility constants.
const (
	axErrorSuccess = 0
	axValueCGPoint = 1
	axValueCGSize  = 2

	// axTimeout bounds each Accessibility request, in seconds, so an app
	// that does not answer delays its mark by at most this much per call
	// instead of the system default of several seconds.
	axTimeout = 0.5
)

type cgSize struct{ W, H float64 }

var (
	axUIElementCreateApplication   func(pid int32) uintptr
	axUIElementSetMessagingTimeout func(el uintptr, timeout float32) int32
	axUIElementCopyAttributeValue  func(el, attr uintptr, value *uintptr) int32
	axValueGetValue                func(value uintptr, valueType uint32, out unsafe.Pointer) bool

	// Attribute names, created once: in the headers they are CFSTR
	// macros, not exported symbols.
	attrFocusedWindow, attrPosition, attrSize uintptr

	axOnce sync.Once
)

// loadAX registers the Accessibility functions once.
func loadAX() {
	load()
	axOnce.Do(func() {
		as := mustDlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
		purego.RegisterLibFunc(&axUIElementCreateApplication, as, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axUIElementSetMessagingTimeout, as, "AXUIElementSetMessagingTimeout")
		purego.RegisterLibFunc(&axUIElementCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axValueGetValue, as, "AXValueGetValue")
		attrFocusedWindow = cfStringCreate("AXFocusedWindow")
		attrPosition = cfStringCreate("AXPosition")
		attrSize = cfStringCreate("AXSize")
	})
}

// FocusedWindowCenter returns the center of the focused window of the
// app of pid, in screen coordinates; ok is false when the app has none,
// Accessibility is not granted or the app does not answer in time. It
// may wait on the app, up to axTimeout per request, so it must not run
// on the main thread.
func FocusedWindowCenter(pid int32) (center mygo.Point, ok bool) {
	defer recoverCallback("focused window read")
	if pid <= 0 {
		return mygo.Point{}, false
	}
	loadAX()
	app := axUIElementCreateApplication(pid)
	if app == 0 {
		return mygo.Point{}, false
	}
	defer cfRelease(app)
	axUIElementSetMessagingTimeout(app, axTimeout)
	var win uintptr
	if code := axUIElementCopyAttributeValue(app, attrFocusedWindow, &win); code != axErrorSuccess || win == 0 {
		return mygo.Point{}, false
	}
	defer cfRelease(win)
	axUIElementSetMessagingTimeout(win, axTimeout)
	// Size is not asked for once position fails: each request may wait
	// up to axTimeout.
	var pos Point
	if !axValueAttr(win, attrPosition, axValueCGPoint, unsafe.Pointer(&pos)) {
		return mygo.Point{}, false
	}
	var size cgSize
	if !axValueAttr(win, attrSize, axValueCGSize, unsafe.Pointer(&size)) {
		return mygo.Point{}, false
	}
	return screenPoint(windowCenter(pos, size.W, size.H)), true
}

// axValueAttr reads an AXValue attribute of valueType into out.
func axValueAttr(el, attr uintptr, valueType uint32, out unsafe.Pointer) bool {
	var v uintptr
	if code := axUIElementCopyAttributeValue(el, attr, &v); code != axErrorSuccess || v == 0 {
		return false
	}
	defer cfRelease(v)
	return axValueGetValue(v, valueType, out)
}
