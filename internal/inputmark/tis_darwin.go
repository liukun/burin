//go:build darwin

package inputmark

import (
	"bytes"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

const (
	cfStringEncodingUTF8  = 0x08000100
	cfStringInitialBuffer = 256
)

var (
	tisCopyCurrentKeyboardInputSource func() uintptr
	tisGetInputSourceProperty         func(source, key uintptr) uintptr
	cfStringGetLength                 func(str uintptr) int
	cfStringGetMaximumSizeForEncoding func(length int, encoding uint32) int
	cfStringGetCString                func(str uintptr, buf *byte, size int, encoding uint32) bool
	cfRelease                         func(obj uintptr)
	cfStringCreateWithCString         func(alloc uintptr, cstr *byte, encoding uint32) uintptr

	kTISPropertyInputSourceID uintptr
	kTISPropertyLocalizedName uintptr

	loadOnce sync.Once
)

func mustDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Sprintf("inputmark: load %s: %v", path, err))
	}
	return h
}

// globalValue reads a framework global such as a CFStringRef constant.
func globalValue(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(fmt.Sprintf("inputmark: %v", err))
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

// load opens the frameworks and registers every function of the package
// once. The Text Input Sources API lives in HIToolbox, inside Carbon.
func load() {
	loadOnce.Do(func() {
		cf := mustDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		hi := mustDlopen("/System/Library/Frameworks/Carbon.framework/Frameworks/HIToolbox.framework/HIToolbox")
		purego.RegisterLibFunc(&tisCopyCurrentKeyboardInputSource, hi, "TISCopyCurrentKeyboardInputSource")
		purego.RegisterLibFunc(&tisGetInputSourceProperty, hi, "TISGetInputSourceProperty")
		purego.RegisterLibFunc(&cfStringGetLength, cf, "CFStringGetLength")
		purego.RegisterLibFunc(&cfStringGetMaximumSizeForEncoding, cf, "CFStringGetMaximumSizeForEncoding")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		kTISPropertyInputSourceID = globalValue(hi, "kTISPropertyInputSourceID")
		kTISPropertyLocalizedName = globalValue(hi, "kTISPropertyLocalizedName")
	})
}

// cfStringCreate makes a CFString of s that is never released.
func cfStringCreate(s string) uintptr {
	b := append([]byte(s), 0)
	return cfStringCreateWithCString(0, &b[0], cfStringEncodingUTF8)
}

// CurrentSource returns the current keyboard input source; ok is false
// when there is none or the main thread no longer runs. The Text Input
// Sources API is asked on the main thread, which it expects.
func CurrentSource() (src Source, ok bool) {
	load()
	mygo.RunOnMain(func() {
		ref := tisCopyCurrentKeyboardInputSource()
		if ref == 0 {
			return
		}
		defer cfRelease(ref)
		// The properties follow the Get rule: ref owns them.
		src.ID = cfStringValue(tisGetInputSourceProperty(ref, kTISPropertyInputSourceID))
		src.Name = cfStringValue(tisGetInputSourceProperty(ref, kTISPropertyLocalizedName))
		ok = src.ID != ""
	})
	return src, ok
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
