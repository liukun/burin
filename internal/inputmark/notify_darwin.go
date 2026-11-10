//go:build darwin

package inputmark

import (
	"log"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"burin/internal/workspace"
)

// cfNotificationSuspensionBehaviorDeliverImmediately delivers the input
// source change even while burin, an accessory app, is in the background.
const cfNotificationSuspensionBehaviorDeliverImmediately = 4

// fallbackSourceChanged is the name of the distributed notification that
// kTISNotifySelectedKeyboardInputSourceChanged stands for, used should
// HIToolbox not export the constant.
const fallbackSourceChanged = "AppleSelectedInputSourcesChangedNotification"

var (
	cfNotificationCenterGetDistributedCenter func() uintptr
	cfNotificationCenterAddObserver          func(center, observer, callback, name, object uintptr, behavior int)
	cfNotificationCenterRemoveObserver       func(center, observer, name, object uintptr)

	// sourceChangedName is the name of the input source change
	// notification, and appActivatedName and applicationKey the
	// NSWorkspace notification of an app activated and the key of its
	// app in userInfo. They live as long as the process.
	sourceChangedName uintptr
	appActivatedName  objc.ID
	applicationKey    objc.ID

	// sourceCallback is the only purego callback for the input source
	// change, and appObserver the only instance of the observer class of
	// app activations: purego callbacks and classes are never freed, so
	// both are made once and stay, and subscribe only swaps what they
	// call.
	sourceCallback uintptr
	appObserver    objc.ID

	notifyOnce sync.Once
	// notifyLoaded is set once loadNotify has made all of the above; it
	// stays false should the load have panicked, as notifyOnce then
	// counts as done all the same.
	notifyLoaded bool
)

// observerTag is the observer the input source callback is registered
// under: CFNotificationCenter only compares its address, which a package
// variable keeps fixed.
var observerTag byte

// loadNotify registers the notification functions and makes the callback
// and observer once, and reports whether they are there.
func loadNotify() bool {
	load()
	notifyOnce.Do(func() {
		cf := mustDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		hi := mustDlopen("/System/Library/Frameworks/Carbon.framework/Frameworks/HIToolbox.framework/HIToolbox")
		appKit := mustDlopen("/System/Library/Frameworks/AppKit.framework/AppKit")
		purego.RegisterLibFunc(&cfNotificationCenterGetDistributedCenter, cf, "CFNotificationCenterGetDistributedCenter")
		purego.RegisterLibFunc(&cfNotificationCenterAddObserver, cf, "CFNotificationCenterAddObserver")
		purego.RegisterLibFunc(&cfNotificationCenterRemoveObserver, cf, "CFNotificationCenterRemoveObserver")

		sourceChangedName = optionalGlobalValue(hi, "kTISNotifySelectedKeyboardInputSourceChanged")
		if sourceChangedName == 0 {
			sourceChangedName = cfStringCreate(fallbackSourceChanged)
		}
		appActivatedName = objc.ID(globalValue(appKit, "NSWorkspaceDidActivateApplicationNotification"))
		applicationKey = objc.ID(globalValue(appKit, "NSWorkspaceApplicationKey"))

		sourceCallback = purego.NewCallback(sourceChangedCallback)
		cls, err := objc.RegisterClass("BurinInputMarkObserver", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{
			Cmd: objc.RegisterName("appActivated:"),
			Fn:  appActivatedMethod,
		}})
		if err != nil {
			panic("inputmark: " + err.Error())
		}
		appObserver = objc.ID(cls).Send(objc.RegisterName("new"))
		notifyLoaded = true
	})
	return notifyLoaded
}

// optionalGlobalValue is globalValue for a constant that may be missing:
// it returns 0 then.
func optionalGlobalValue(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil || p == 0 {
		return 0
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

// subscription is what the callbacks call.
type subscription struct {
	onSource func()
	onApp    func(pid int32)
}

// notify is the state of the observers: cur is the subscription the
// callbacks call, nil when none, and registered whether the observers
// are on the notification centers. Only the main thread registers, and
// it does so to match cur, whatever order the requests arrive in.
var notify struct {
	mu  sync.Mutex
	cur *subscription
	// registered is main thread only.
	registered bool
}

// current returns the subscription the callbacks call, nil when none.
func current() *subscription {
	notify.mu.Lock()
	defer notify.mu.Unlock()
	return notify.cur
}

// syncObservers adds the observers while there is a subscription and
// removes them while there is none. Main thread only. A failed load is
// logged, and nothing is added.
func syncObservers() {
	defer recoverCallback("observer sync")
	if !loadNotify() {
		return
	}
	want := current() != nil
	if want == notify.registered {
		return
	}
	center := cfNotificationCenterGetDistributedCenter()
	appCenter := objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace")).
		Send(objc.RegisterName("notificationCenter"))
	observer := uintptr(unsafe.Pointer(&observerTag))
	if want {
		cfNotificationCenterAddObserver(center, observer, sourceCallback, sourceChangedName, 0,
			cfNotificationSuspensionBehaviorDeliverImmediately)
		appCenter.Send(objc.RegisterName("addObserver:selector:name:object:"),
			appObserver, objc.RegisterName("appActivated:"), appActivatedName, objc.ID(0))
	} else {
		cfNotificationCenterRemoveObserver(center, observer, sourceChangedName, 0)
		appCenter.Send(objc.RegisterName("removeObserver:name:object:"),
			appObserver, appActivatedName, objc.ID(0))
	}
	notify.registered = want
}

// sourceChangedCallback is the CFNotificationCallback of the input source
// change, on the main thread. The notification carries no input source;
// the subscriber reads it.
func sourceChangedCallback(center, observer, name, object, userInfo uintptr) {
	defer recoverCallback("input source change callback")
	sub := current()
	if sub == nil {
		return
	}
	sub.onSource()
}

// appActivatedMethod is -[BurinInputMarkObserver appActivated:], on the
// main thread. The app is the one in the notification, not the
// frontmost, which may already be another.
func appActivatedMethod(self objc.ID, cmd objc.SEL, note objc.ID) {
	defer recoverCallback("app activation callback")
	sub := current()
	if sub == nil {
		return
	}
	pid, ok := activatedPID(note)
	if !ok {
		return
	}
	sub.onApp(pid)
}

// activatedPID returns the pid of the app of an activation notification,
// or of the frontmost app should it carry none.
func activatedPID(note objc.ID) (int32, bool) {
	info := note.Send(objc.RegisterName("userInfo"))
	if info != 0 {
		app := info.Send(objc.RegisterName("objectForKey:"), applicationKey)
		if app != 0 {
			return objc.Send[int32](app, objc.RegisterName("processIdentifier")), true
		}
	}
	return workspace.FrontmostPID()
}

// recoverCallback logs a panic of code run from the main loop, a
// notification callback or the observer sync, rather than let it unwind
// through C frames and take the app down.
func recoverCallback(what string) {
	if r := recover(); r != nil {
		log.Printf("inputmark: %s panic: %v", what, r)
	}
}

// nativeSystem is the system of the running app.
type nativeSystem struct{}

// subscribe calls onSource each time the input source changes, as when
// switched by hand or by the system for the app activated, and onApp
// with the pid of each app activated, until stop is called. Both are
// called on the main thread and must not block it. A new subscribe
// replaces the last.
//
// Neither subscribe nor stop waits: the observers are added and removed
// on the main thread later, as their notifications are delivered on the
// run loop they were added on, and the caller may hold a lock the main
// thread waits for. stop may be called more than once.
func (nativeSystem) subscribe(onSource func(), onApp func(pid int32)) (stop func()) {
	sub := &subscription{onSource: onSource, onApp: onApp}
	notify.mu.Lock()
	notify.cur = sub
	notify.mu.Unlock()
	go mygo.RunOnMain(syncObservers)
	return func() {
		notify.mu.Lock()
		if notify.cur == sub {
			notify.cur = nil
		}
		notify.mu.Unlock()
		go mygo.RunOnMain(syncObservers)
	}
}

// post runs fn on the main thread from a goroutine of its own, so the
// caller never waits for it.
func (nativeSystem) post(fn func()) { go mygo.RunOnMain(fn) }

// frontPID and source run on the main thread, where mygo.RunOnMain, under
// both, calls at once.
func (nativeSystem) frontPID() (int32, bool) { return workspace.FrontmostPID() }
func (nativeSystem) source() (Source, bool)  { return CurrentSource() }
