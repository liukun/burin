//go:build darwin

package windowrules

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreGraphics and CoreFoundation constants.
const (
	cgWindowListOptionOnScreenOnly     = 1 << 0
	cgWindowListExcludeDesktopElements = 1 << 4
	cgNullWindowID                     = 0

	cfNumberSInt64Type    = 4
	cfStringEncodingUTF8  = 0x08000100
	cfStringInitialBuffer = 256

	// procNameBuffer holds a process name from proc_name, which copies at
	// most 2*MAXCOMLEN (32) bytes.
	procNameBuffer = 64

	// normalWindowLayer is the layer of app windows; menus, the Dock and
	// status items sit above it.
	normalWindowLayer = 0
)

var (
	cgWindowListCopyWindowInfo             func(option, relativeToWindow uint32) uintptr
	cgRectMakeWithDictionaryRepresentation func(dict uintptr, rect *Rect) bool
	cfArrayGetCount                        func(arr uintptr) int
	cfArrayGetValueAtIndex                 func(arr uintptr, i int) uintptr
	cfDictionaryGetValue                   func(dict, key uintptr) uintptr
	cfNumberGetValue                       func(num uintptr, numberType int, value unsafe.Pointer) bool
	cfStringGetLength                      func(str uintptr) int
	cfStringGetMaximumSizeForEncoding      func(length int, encoding uint32) int
	cfStringGetCString                     func(str uintptr, buf *byte, size int, encoding uint32) bool
	cfStringCreateWithCString              func(alloc uintptr, cstr *byte, encoding uint32) uintptr
	cfRelease                              func(obj uintptr)
	procName                               func(pid int32, buf *byte, size uint32) int32

	kCGWindowOwnerPID  uintptr
	kCGWindowOwnerName uintptr
	kCGWindowBounds    uintptr
	kCGWindowLayer     uintptr
	kCGWindowNumber    uintptr
	kCFBooleanTrue     uintptr
	kCFBooleanFalse    uintptr

	loadOnce sync.Once
)

func mustDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Sprintf("windowrules: load %s: %v", path, err))
	}
	return h
}

func symbol(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(fmt.Sprintf("windowrules: %v", err))
	}
	return p
}

// globalValue reads a framework global such as a CFStringRef constant.
func globalValue(lib uintptr, name string) uintptr {
	p := symbol(lib, name)
	return **(**uintptr)(unsafe.Pointer(&p))
}

// load opens the frameworks and registers every function of the package
// once.
func load() {
	loadOnce.Do(func() {
		cf := mustDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		cg := mustDlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		as := mustDlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
		libSystem := mustDlopen("/usr/lib/libSystem.B.dylib")
		purego.RegisterLibFunc(&cgWindowListCopyWindowInfo, cg, "CGWindowListCopyWindowInfo")
		purego.RegisterLibFunc(&cgRectMakeWithDictionaryRepresentation, cg, "CGRectMakeWithDictionaryRepresentation")
		purego.RegisterLibFunc(&cfArrayGetCount, cf, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfDictionaryGetValue, cf, "CFDictionaryGetValue")
		purego.RegisterLibFunc(&cfNumberGetValue, cf, "CFNumberGetValue")
		purego.RegisterLibFunc(&cfStringGetLength, cf, "CFStringGetLength")
		purego.RegisterLibFunc(&cfStringGetMaximumSizeForEncoding, cf, "CFStringGetMaximumSizeForEncoding")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&procName, libSystem, "proc_name")
		kCGWindowOwnerPID = globalValue(cg, "kCGWindowOwnerPID")
		kCGWindowOwnerName = globalValue(cg, "kCGWindowOwnerName")
		kCGWindowBounds = globalValue(cg, "kCGWindowBounds")
		kCGWindowLayer = globalValue(cg, "kCGWindowLayer")
		kCGWindowNumber = globalValue(cg, "kCGWindowNumber")
		kCFBooleanTrue = globalValue(cf, "kCFBooleanTrue")
		kCFBooleanFalse = globalValue(cf, "kCFBooleanFalse")
		loadAX(as)
	})
}

// cfStringValue converts a CFString to Go. A string too long for the
// first buffer is read again into one of the maximum UTF-8 size.
func cfStringValue(s uintptr) string {
	if s == 0 {
		return ""
	}
	buf := make([]byte, cfStringInitialBuffer)
	if !cfStringGetCString(s, &buf[0], len(buf), cfStringEncodingUTF8) {
		size := cfStringGetMaximumSizeForEncoding(cfStringGetLength(s), cfStringEncodingUTF8) + 1
		buf = make([]byte, size)
		if !cfStringGetCString(s, &buf[0], len(buf), cfStringEncodingUTF8) {
			return ""
		}
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}

// cfStringCreate makes a CFString the caller owns.
func cfStringCreate(s string) uintptr {
	cstr := append([]byte(s), 0)
	return cfStringCreateWithCString(0, &cstr[0], cfStringEncodingUTF8)
}

// cfNumberInt64 reads a CFNumber; ok is false when it is missing.
func cfNumberInt64(num uintptr) (int64, bool) {
	if num == 0 {
		return 0, false
	}
	var v int64
	ok := cfNumberGetValue(num, cfNumberSInt64Type, unsafe.Pointer(&v))
	return v, ok
}

// processName returns the name of the process pid, not localized, or ""
// when it cannot be read.
func processName(pid int32) string {
	buf := make([]byte, procNameBuffer)
	n := procName(pid, &buf[0], uint32(len(buf)))
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

// listWindows returns the on-screen windows of the normal layer, front to
// back, in the current Space, and whether Mission Control is up, told by
// the Dock's backdrop windows over displays (see isMissionControlWindow).
// kCGWindowName is not read: it needs the Screen Recording permission,
// and titles come from Accessibility.
func listWindows(displays []Display) (windows []CGWindow, missionControl bool, err error) {
	load()
	arr := cgWindowListCopyWindowInfo(cgWindowListOptionOnScreenOnly|cgWindowListExcludeDesktopElements, cgNullWindowID)
	if arr == 0 {
		return nil, false, errors.New("CGWindowListCopyWindowInfo returned NULL")
	}
	defer cfRelease(arr)
	n := cfArrayGetCount(arr)
	windows = make([]CGWindow, 0, n)
	for i := range n {
		dict := cfArrayGetValueAtIndex(arr, i)
		layer, hasLayer := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowLayer))
		if !hasLayer {
			continue
		}
		if layer != normalWindowLayer {
			missionControl = missionControl || isBackdrop(dict, layer, displays)
			continue
		}
		if w, ok := windowFromDict(dict); ok {
			windows = append(windows, w)
		}
	}
	return windows, missionControl, nil
}

// isBackdrop reports whether the window of dict, at layer, is one of the
// Dock's Mission Control backdrops.
func isBackdrop(dict uintptr, layer int64, displays []Display) bool {
	r, hasBounds := boundsOf(dict)
	if !hasBounds {
		return false
	}
	process := func() string {
		pid, hasPID := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowOwnerPID))
		if !hasPID {
			return ""
		}
		return processName(int32(pid))
	}
	return isMissionControlWindow(layer, r, displays, process)
}

// boundsOf reads kCGWindowBounds of a window dictionary.
func boundsOf(dict uintptr) (Rect, bool) {
	var r Rect
	boundsDict := cfDictionaryGetValue(dict, kCGWindowBounds)
	ok := boundsDict != 0 && cgRectMakeWithDictionaryRepresentation(boundsDict, &r)
	return r, ok
}

// windowFromDict reads one window dictionary of the normal layer; ok is
// false for incomplete entries.
func windowFromDict(dict uintptr) (CGWindow, bool) {
	id, hasID := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowNumber))
	pid, hasPID := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowOwnerPID))
	r, hasBounds := boundsOf(dict)
	if !hasID || !hasPID || !hasBounds {
		return CGWindow{}, false
	}
	return CGWindow{
		ID:     uint32(id),
		PID:    int32(pid),
		Owner:  cfStringValue(cfDictionaryGetValue(dict, kCGWindowOwnerName)),
		Bounds: r,
	}, true
}
