//go:build darwin

// Package pasteboard reads and writes plain text on the general
// NSPasteboard.
package pasteboard

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	typeString         = "public.utf8-plain-text"
	typeConcealed      = "org.nspasteboard.ConcealedType"
	typeTransient      = "com.nspasteboard.TransientType"
	nsUTF8StringEncode = 4
)

var (
	poolPush func() uintptr
	poolPop  func(pool uintptr)
	loadOnce sync.Once
)

func load() {
	loadOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			panic(fmt.Sprintf("pasteboard: %v", err))
		}
		libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			panic(fmt.Sprintf("pasteboard: %v", err))
		}
		purego.RegisterLibFunc(&poolPush, libobjc, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, libobjc, "objc_autoreleasePoolPop")
	})
}

// withPool runs fn inside an autorelease pool on one OS thread, as calls
// from background goroutines have no pool of their own.
func withPool(fn func()) {
	load()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := poolPush()
	defer poolPop(pool)
	fn()
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func general() objc.ID {
	return objc.ID(objc.GetClass("NSPasteboard")).Send(sel("generalPasteboard"))
}

// nsString returns an autoreleased NSString copy of s.
func nsString(s string) objc.ID {
	str := objc.ID(objc.GetClass("NSString")).Send(sel("alloc"))
	str = str.Send(sel("initWithBytes:length:encoding:"), unsafe.StringData(s), len(s), nsUTF8StringEncode)
	runtime.KeepAlive(s)
	return str.Send(sel("autorelease"))
}

func goString(str objc.ID) string {
	if str == 0 {
		return ""
	}
	p := objc.Send[*byte](str, sel("UTF8String"))
	if p == nil {
		return ""
	}
	n := objc.Send[int](str, sel("lengthOfBytesUsingEncoding:"), nsUTF8StringEncode)
	return string(unsafe.Slice(p, n))
}

// WriteConcealed puts text on the pasteboard marked concealed and transient,
// so clipboard history managers skip it. It returns the change count the
// write made, the one clearContents gave it.
func WriteConcealed(text string) (count int, ok bool) { return write(text, true) }

// WriteText puts text on the pasteboard as an ordinary copy, which
// clipboard history managers record.
func WriteText(text string) bool {
	_, ok := write(text, false)
	return ok
}

// write puts text on the pasteboard and returns the change count that
// clearContents returned: the generation this write owns, unlike a
// changeCount read afterwards, which another app's write could have raised.
func write(text string, concealed bool) (count int, ok bool) {
	withPool(func() {
		pb := general()
		count = objc.Send[int](pb, sel("clearContents"))
		if !objc.Send[bool](pb, sel("setString:forType:"), nsString(text), nsString(typeString)) {
			return
		}
		ok = true
		if concealed {
			pb.Send(sel("setString:forType:"), nsString(""), nsString(typeConcealed))
			pb.Send(sel("setString:forType:"), nsString(""), nsString(typeTransient))
		}
	})
	return count, ok
}

// ReadText returns the pasteboard's plain text, empty when it has none.
func ReadText() string {
	var text string
	withPool(func() {
		text = goString(general().Send(sel("stringForType:"), nsString(typeString)))
	})
	return text
}

// ChangeCount returns the general pasteboard's change count, which goes up
// each time its contents change: a copy in any app, or a write here.
func ChangeCount() int {
	var count int
	withPool(func() {
		count = objc.Send[int](general(), sel("changeCount"))
	})
	return count
}
