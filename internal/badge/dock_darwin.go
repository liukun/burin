//go:build darwin

package badge

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"

	"burin/internal/workspace"
)

// dockBundleID is the Dock's, whose Accessibility tree holds the badges.
const dockBundleID = "com.apple.dock"

const (
	axErrorSuccess        = 0
	cfStringEncodingUTF8  = 0x08000100
	cfStringInitialBuffer = 256
	// axTimeout bounds every Accessibility request, in seconds, so a
	// Dock that does not answer stalls a poll by at most this much per
	// call instead of the system default of several seconds.
	axTimeout = 0.5
)

var (
	loadOnce sync.Once

	axUIElementCreateApplication   func(pid int32) uintptr
	axUIElementSetMessagingTimeout func(el uintptr, timeout float32) int32
	axUIElementCopyAttributeValue  func(el, attr uintptr, value *uintptr) int32

	cfArrayGetCount                   func(arr uintptr) int
	cfArrayGetValueAtIndex            func(arr uintptr, i int) uintptr
	cfGetTypeID                       func(obj uintptr) uint
	cfStringGetTypeID                 func() uint
	cfArrayGetTypeID                  func() uint
	cfURLGetTypeID                    func() uint
	cfURLCopyFileSystemPath           func(url uintptr, style int) uintptr
	cfStringGetLength                 func(s uintptr) int
	cfStringGetMaximumSizeForEncoding func(length int, encoding uint32) int
	cfStringGetCString                func(s uintptr, buf *byte, size int, encoding uint32) bool
	cfStringCreateWithCString         func(alloc uintptr, cstr *byte, encoding uint32) uintptr
	cfRelease                         func(obj uintptr)

	// Attribute names, created once: in the headers they are CFSTR
	// macros, not exported symbols.
	attrChildren, attrRole, attrTitle, attrStatusLabel, attrURL uintptr
)

// cfURLPOSIXPathStyle is kCFURLPOSIXPathStyle.
const cfURLPOSIXPathStyle = 0

func mustDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Sprintf("badge: %v", err))
	}
	return h
}

// load registers the Accessibility and CoreFoundation functions this
// package needs, apart from windowrules' own.
func load() {
	loadOnce.Do(func() {
		cf := mustDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		as := mustDlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
		purego.RegisterLibFunc(&axUIElementCreateApplication, as, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axUIElementSetMessagingTimeout, as, "AXUIElementSetMessagingTimeout")
		purego.RegisterLibFunc(&axUIElementCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&cfArrayGetCount, cf, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfArrayGetTypeID, cf, "CFArrayGetTypeID")
		purego.RegisterLibFunc(&cfURLGetTypeID, cf, "CFURLGetTypeID")
		purego.RegisterLibFunc(&cfURLCopyFileSystemPath, cf, "CFURLCopyFileSystemPath")
		purego.RegisterLibFunc(&cfStringGetLength, cf, "CFStringGetLength")
		purego.RegisterLibFunc(&cfStringGetMaximumSizeForEncoding, cf, "CFStringGetMaximumSizeForEncoding")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		attrChildren = cfStringCreate("AXChildren")
		attrRole = cfStringCreate("AXRole")
		attrTitle = cfStringCreate("AXTitle")
		attrStatusLabel = cfStringCreate("AXStatusLabel")
		attrURL = cfStringCreate("AXURL")
	})
}

// DockIcon is what Read tells of an icon in the Dock.
type DockIcon struct {
	// Label is the badge's text, empty without a badge.
	Label string
	// Path is the app's bundle, from its AXURL, empty when unknown or
	// without a badge. The
	// Dock titles apps in the user's language, which the installed apps
	// are not listed by, so the icon is drawn from here.
	Path string
}

// Read returns every Dock icon by its title: the AXStatusLabel and AXURL
// of each element of the Dock's first AXList. It needs Accessibility
// permission, and runs on the calling goroutine.
func Read() (map[string]DockIcon, error) {
	load()
	pid, err := dockPID()
	if err != nil {
		return nil, err
	}
	app := axUIElementCreateApplication(pid)
	if app == 0 {
		return nil, fmt.Errorf("Dock pid %d: no Accessibility element", pid)
	}
	defer cfRelease(app)
	axUIElementSetMessagingTimeout(app, axTimeout)
	children, err := copyArray(app, attrChildren)
	if err != nil {
		return nil, fmt.Errorf("Dock: %w", err)
	}
	defer cfRelease(children)
	list := firstList(children)
	if list == 0 {
		return nil, errors.New("Dock: no AXList among its children")
	}
	icons, err := copyArray(list, attrChildren)
	if err != nil {
		return nil, fmt.Errorf("Dock list: %w", err)
	}
	defer cfRelease(icons)
	n := cfArrayGetCount(icons)
	labels := make(map[string]DockIcon, n)
	for i := range n {
		icon := cfArrayGetValueAtIndex(icons, i)
		axUIElementSetMessagingTimeout(icon, axTimeout)
		title := stringAttr(icon, attrTitle)
		if title == "" {
			continue
		}
		label := stringAttr(icon, attrStatusLabel)
		if label == "" {
			// Only an icon with a badge shows, so only its path is asked.
			labels[title] = DockIcon{}
			continue
		}
		labels[title] = DockIcon{Label: label, Path: pathAttr(icon, attrURL)}
	}
	return labels, nil
}

// dockPID returns the pid of the running Dock.
func dockPID() (int32, error) {
	apps, ok := workspace.RunningApps()
	if !ok {
		return 0, errors.New("the main thread no longer runs")
	}
	for _, a := range apps {
		if a.BundleID == dockBundleID {
			return a.PID, nil
		}
	}
	return 0, errors.New("the Dock is not running")
}

// firstList returns the first element of children whose role is AXList,
// owned by children, or 0.
func firstList(children uintptr) uintptr {
	for i := range cfArrayGetCount(children) {
		el := cfArrayGetValueAtIndex(children, i)
		axUIElementSetMessagingTimeout(el, axTimeout)
		if stringAttr(el, attrRole) == "AXList" {
			return el
		}
	}
	return 0
}

// copyArray returns the array value of attr, which the caller must
// release.
func copyArray(el, attr uintptr) (uintptr, error) {
	var v uintptr
	if code := axUIElementCopyAttributeValue(el, attr, &v); code != axErrorSuccess {
		return 0, fmt.Errorf("%s: AXError %d", cfStringValue(attr), code)
	}
	if v == 0 {
		return 0, fmt.Errorf("%s: no value", cfStringValue(attr))
	}
	if cfGetTypeID(v) != cfArrayGetTypeID() {
		cfRelease(v)
		return 0, fmt.Errorf("%s: not an array", cfStringValue(attr))
	}
	return v, nil
}

// stringAttr returns the string value of attr, or "" when el has none or
// it is not a string.
func stringAttr(el, attr uintptr) string {
	var v uintptr
	if axUIElementCopyAttributeValue(el, attr, &v) != axErrorSuccess || v == 0 {
		return ""
	}
	defer cfRelease(v)
	if cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	return cfStringValue(v)
}

// pathAttr returns the file path of the URL value of attr, or "".
func pathAttr(el, attr uintptr) string {
	var v uintptr
	if axUIElementCopyAttributeValue(el, attr, &v) != axErrorSuccess || v == 0 {
		return ""
	}
	defer cfRelease(v)
	if cfGetTypeID(v) != cfURLGetTypeID() {
		return ""
	}
	path := cfURLCopyFileSystemPath(v, cfURLPOSIXPathStyle)
	if path == 0 {
		return ""
	}
	defer cfRelease(path)
	return cfStringValue(path)
}

// cfStringValue converts a CFString to Go. A string too long for the
// first buffer is read again into one of its maximum size.
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
