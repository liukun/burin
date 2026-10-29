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

	kCGWindowOwnerPID  uintptr
	kCGWindowOwnerName uintptr
	kCGWindowBounds    uintptr
	kCGWindowLayer     uintptr
	kCGWindowNumber    uintptr
	kCFBooleanTrue     uintptr

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
		kCGWindowOwnerPID = globalValue(cg, "kCGWindowOwnerPID")
		kCGWindowOwnerName = globalValue(cg, "kCGWindowOwnerName")
		kCGWindowBounds = globalValue(cg, "kCGWindowBounds")
		kCGWindowLayer = globalValue(cg, "kCGWindowLayer")
		kCGWindowNumber = globalValue(cg, "kCGWindowNumber")
		kCFBooleanTrue = globalValue(cf, "kCFBooleanTrue")
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

// listWindows returns the on-screen windows of the normal layer, front to
// back, in the current Space. kCGWindowName is not read: it needs the
// Screen Recording permission, and titles come from Accessibility.
func listWindows() ([]CGWindow, error) {
	load()
	arr := cgWindowListCopyWindowInfo(cgWindowListOptionOnScreenOnly|cgWindowListExcludeDesktopElements, cgNullWindowID)
	if arr == 0 {
		return nil, errors.New("CGWindowListCopyWindowInfo returned NULL")
	}
	defer cfRelease(arr)
	n := cfArrayGetCount(arr)
	out := make([]CGWindow, 0, n)
	for i := range n {
		w, ok := windowFromDict(cfArrayGetValueAtIndex(arr, i))
		if ok {
			out = append(out, w)
		}
	}
	return out, nil
}

// windowFromDict reads one window dictionary of the list; ok is false for
// windows outside the normal layer and for incomplete entries.
func windowFromDict(dict uintptr) (CGWindow, bool) {
	layer, hasLayer := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowLayer))
	if !hasLayer || layer != normalWindowLayer {
		return CGWindow{}, false
	}
	id, hasID := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowNumber))
	pid, hasPID := cfNumberInt64(cfDictionaryGetValue(dict, kCGWindowOwnerPID))
	boundsDict := cfDictionaryGetValue(dict, kCGWindowBounds)
	var r Rect
	hasBounds := boundsDict != 0 && cgRectMakeWithDictionaryRepresentation(boundsDict, &r)
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
