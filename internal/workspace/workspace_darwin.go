//go:build darwin

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const (
	nsUTF8StringEncoding = 4

	// nsApplicationActivateIgnoringOtherApps brings the app forward even
	// though burin, an accessory app, is not the active one.
	nsApplicationActivateIgnoringOtherApps = 1 << 1
)

// errNoMainThread is returned when the app has quit and the main thread
// no longer runs requests.
var errNoMainThread = errors.New("the main thread no longer runs")

var (
	poolPush func() uintptr
	poolPop  func(pool uintptr)
	loadOnce sync.Once
)

func load() {
	loadOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			panic(fmt.Sprintf("workspace: %v", err))
		}
		libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			panic(fmt.Sprintf("workspace: %v", err))
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

// RunningApps returns the running apps; ok is false when the main thread
// no longer runs them.
func RunningApps() (apps []App, ok bool) {
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

// FrontmostPID returns the pid of the frontmost app, or -1 when there is
// none; ok is false when the main thread no longer runs it.
func FrontmostPID() (pid int32, ok bool) {
	pid = -1
	ok = onMain(func() { pid = frontPID(sharedWorkspace()) })
	return pid, ok
}

// FrontmostApp returns the name, bundle ID and pid of the frontmost app,
// marked Active, or the zero App when there is none or the main thread
// no longer runs.
func FrontmostApp() App {
	var app App
	onMain(func() {
		front := sharedWorkspace().Send(sel("frontmostApplication"))
		if front == 0 {
			return
		}
		app = App{
			PID:      pidOf(front),
			Name:     goString(front.Send(sel("localizedName"))),
			BundleID: goString(front.Send(sel("bundleIdentifier"))),
			Active:   true,
		}
	})
	return app
}

// send sends the request of fn to the running app pid on the main thread.
// It reports false when the app is gone or did not take the request at
// once.
func send(pid int32, fn func(app objc.ID) bool) bool {
	done := false
	onMain(func() {
		cls := objc.ID(objc.GetClass("NSRunningApplication"))
		app := cls.Send(sel("runningApplicationWithProcessIdentifier:"), pid)
		if app == 0 {
			return
		}
		done = fn(app)
	})
	return done
}

// sendBool sends the argumentless method to the running app pid and
// returns its BOOL; false also when the app is gone.
func sendBool(pid int32, method string) bool {
	return send(pid, func(app objc.ID) bool { return objc.Send[bool](app, sel(method)) })
}

// Hide hides the app pid. It reports false when the app is gone or did
// not take the request at once; the request may still land, as hide is
// asynchronous.
func Hide(pid int32) bool {
	return sendBool(pid, "hide")
}

// Terminate quits the app pid. It reports false when the app is gone or
// did not take the request at once; the request may still land, as
// terminate is asynchronous.
func Terminate(pid int32) bool {
	return sendBool(pid, "terminate")
}

// Activate brings the app pid to the front, unhiding it. It reports false
// when the app is gone or did not take the request at once; like hide,
// activation is asynchronous.
func Activate(pid int32) bool {
	return send(pid, func(app objc.ID) bool {
		// On some macOS versions activating a hidden app brings it to
		// the front without unhiding it, so it is unhidden first. Like
		// hide, unhide is asynchronous, and its NO is ignored.
		objc.Send[bool](app, sel("unhide"))
		return objc.Send[bool](app, sel("activateWithOptions:"), uint(nsApplicationActivateIgnoringOtherApps))
	})
}

// installed caches the installed apps: InstalledApps reads the disk, and
// the leader keys that launch apps should not wait on it every time. An
// app installed after the first launch is not seen until burin restarts.
var installed struct {
	mu   sync.Mutex
	apps []mygo.InstalledApp
}

// installedApps returns the installed apps, reading them on the first
// call. A failed read is not cached, so the next call tries again.
func installedApps() ([]mygo.InstalledApp, error) {
	installed.mu.Lock()
	defer installed.mu.Unlock()
	if installed.apps != nil {
		return installed.apps, nil
	}
	apps, err := mygo.Shell.InstalledApps()
	if err != nil {
		return nil, err
	}
	installed.apps = apps
	return apps, nil
}

// launchTimeout bounds the open command, which returns once the app has
// launched.
const launchTimeout = 10 * time.Second

// Launch starts the installed app whose name is name, ignoring case.
//
// It runs the open command rather than NSWorkspace: opening a file URL
// from this process makes macOS record the process in the bundle's
// com.apple.macl attribute, a write into /Applications that App
// Management blocks with a "prevented from modifying apps" notification
// on every first launch. open is Apple's and exempt.
func Launch(name string) error {
	path, err := installedPath(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), launchTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/open", "-a", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// installedPath returns the bundle path of the installed app whose name
// is name, ignoring case.
func installedPath(name string) (string, error) {
	apps, err := installedApps()
	if err != nil {
		return "", err
	}
	for _, app := range apps {
		if strings.EqualFold(app.Name, name) {
			return app.Path, nil
		}
	}
	return "", fmt.Errorf("no app named %q", name)
}

// Toggle hides app name when it is frontmost, activates it when it runs
// in the background or hidden, and launches it when it does not run.
func Toggle(name string) error {
	apps, ok := RunningApps()
	if !ok {
		return errNoMainThread
	}
	action, app := Decide(apps, name)
	// Hide and activate are asynchronous: NO means the app did not take
	// the request at once, not that it refused, so it is no error.
	switch action {
	case HideApp:
		Hide(app.PID)
	case ActivateApp:
		Activate(app.PID)
	default:
		return Launch(name)
	}
	return nil
}

// Open activates app name when it runs and launches it when it does not.
func Open(name string) error {
	apps, ok := RunningApps()
	if !ok {
		return errNoMainThread
	}
	app, running := find(apps, name)
	if !running {
		return Launch(name)
	}
	// Asynchronous like Toggle's: NO is no error.
	Activate(app.PID)
	return nil
}

// AppPath returns the bundle path of app name: the running app's, as Open
// finds it, else the installed app's, as Launch finds it.
func AppPath(name string) (string, error) {
	apps, ok := RunningApps()
	if !ok {
		return "", errNoMainThread
	}
	if app, running := find(apps, name); running && app.Path != "" {
		return app.Path, nil
	}
	return installedPath(name)
}
