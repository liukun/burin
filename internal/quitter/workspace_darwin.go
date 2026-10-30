//go:build darwin

package quitter

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const nsUTF8StringEncoding = 4

var (
	poolPush func() uintptr
	poolPop  func(pool uintptr)
	loadOnce sync.Once
)

func load() {
	loadOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			panic(fmt.Sprintf("quitter: %v", err))
		}
		libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			panic(fmt.Sprintf("quitter: %v", err))
		}
		purego.RegisterLibFunc(&poolPush, libobjc, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, libobjc, "objc_autoreleasePoolPop")
	})
}

// onMain runs fn on the main thread inside an autorelease pool and
// reports whether it ran: NSWorkspace updates its list of running apps
// and their state on the main run loop, so it is asked there, the way
// windowrules asks mygo for the displays. After the app has quit fn is
// dropped.
func onMain(fn func()) bool {
	load()
	ran := false
	mygo.RunOnMain(func() {
		pool := poolPush()
		defer poolPop(pool)
		fn()
		ran = true
	})
	return ran
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func goString(str objc.ID) string {
	if str == 0 {
		return ""
	}
	p := objc.Send[*byte](str, sel("UTF8String"))
	if p == nil {
		return ""
	}
	n := objc.Send[int](str, sel("lengthOfBytesUsingEncoding:"), nsUTF8StringEncoding)
	return string(unsafe.Slice(p, n))
}

func pidOf(app objc.ID) int32 { return objc.Send[int32](app, sel("processIdentifier")) }

func sharedWorkspace() objc.ID {
	return objc.ID(objc.GetClass("NSWorkspace")).Send(sel("sharedWorkspace"))
}

// frontPID returns the pid of the frontmost app of ws, or -1 when there
// is none.
func frontPID(ws objc.ID) int32 {
	front := ws.Send(sel("frontmostApplication"))
	if front == 0 {
		return -1
	}
	return pidOf(front)
}

// listApps returns the running apps; ok is false when the main thread no
// longer runs them.
func listApps() (apps []App, ok bool) {
	ok = onMain(func() {
		ws := sharedWorkspace()
		front := frontPID(ws)
		list := ws.Send(sel("runningApplications"))
		n := objc.Send[uint](list, sel("count"))
		apps = make([]App, 0, n)
		for i := range n {
			app := list.Send(sel("objectAtIndex:"), i)
			pid := pidOf(app)
			apps = append(apps, App{
				PID:      pid,
				Name:     goString(app.Send(sel("localizedName"))),
				BundleID: goString(app.Send(sel("bundleIdentifier"))),
				Path:     goString(app.Send(sel("bundleURL")).Send(sel("path"))),
				Active:   pid == front,
				Hidden:   objc.Send[bool](app, sel("isHidden")),
			})
		}
	})
	return apps, ok
}

// frontmostPID returns the pid of the frontmost app, or -1 when there is
// none; ok is false when the main thread no longer runs it.
func frontmostPID() (pid int32, ok bool) {
	pid = -1
	ok = onMain(func() { pid = frontPID(sharedWorkspace()) })
	return pid, ok
}

// apply hides or quits the app of step. It reports false when the app is
// gone or refused.
func apply(step Step) bool {
	method := "hide"
	if step.Rule.Action == Quit {
		method = "terminate"
	}
	done := false
	onMain(func() {
		cls := objc.ID(objc.GetClass("NSRunningApplication"))
		app := cls.Send(sel("runningApplicationWithProcessIdentifier:"), step.App.PID)
		if app == 0 {
			return
		}
		done = objc.Send[bool](app, sel(method))
	})
	return done
}
