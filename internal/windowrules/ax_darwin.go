//go:build darwin

package windowrules

import (
	"errors"
	"fmt"
	"slices"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Accessibility constants.
const (
	axErrorSuccess = 0
	axValueCGPoint = 1
	axValueCGSize  = 2

	// axTimeout bounds every Accessibility request, in seconds, so an app
	// that does not answer stalls a round by at most this much per call
	// instead of the system default of several seconds.
	axTimeout = 0.5
)

type cgPoint struct{ X, Y float64 }
type cgSize struct{ W, H float64 }

var (
	axUIElementCreateApplication   func(pid int32) uintptr
	axUIElementSetMessagingTimeout func(el uintptr, timeout float32) int32
	axUIElementCopyAttributeValue  func(el, attr uintptr, value *uintptr) int32
	axUIElementSetAttributeValue   func(el, attr, value uintptr) int32
	axUIElementPerformAction       func(el, action uintptr) int32
	axValueCreate                  func(valueType uint32, value unsafe.Pointer) uintptr
	axValueGetValue                func(value uintptr, valueType uint32, out unsafe.Pointer) bool
	// axUIElementGetWindow is the private _AXUIElementGetWindow, which
	// hs.window:id() relies on. It is nil when the symbol is gone, and
	// windows are then matched by bounds.
	axUIElementGetWindow func(el uintptr, id *uint32) int32

	// Attribute names, created once: in the headers they are CFSTR
	// macros, not exported symbols.
	attrWindows, attrSubrole, attrMinimized, attrFullScreen uintptr
	attrFullScreenButton, attrTitle, attrPosition, attrSize uintptr
	attrCloseButton, attrMain, actionPress, actionRaise     uintptr
)

// loadAX registers the Accessibility functions from ApplicationServices.
func loadAX(as uintptr) {
	purego.RegisterLibFunc(&axUIElementCreateApplication, as, "AXUIElementCreateApplication")
	purego.RegisterLibFunc(&axUIElementSetMessagingTimeout, as, "AXUIElementSetMessagingTimeout")
	purego.RegisterLibFunc(&axUIElementCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
	purego.RegisterLibFunc(&axUIElementSetAttributeValue, as, "AXUIElementSetAttributeValue")
	purego.RegisterLibFunc(&axUIElementPerformAction, as, "AXUIElementPerformAction")
	purego.RegisterLibFunc(&axValueCreate, as, "AXValueCreate")
	purego.RegisterLibFunc(&axValueGetValue, as, "AXValueGetValue")
	if p, err := purego.Dlsym(as, "_AXUIElementGetWindow"); err == nil {
		purego.RegisterFunc(&axUIElementGetWindow, p)
	}
	attrWindows = cfStringCreate("AXWindows")
	attrSubrole = cfStringCreate("AXSubrole")
	attrMinimized = cfStringCreate("AXMinimized")
	attrFullScreen = cfStringCreate("AXFullScreen")
	attrFullScreenButton = cfStringCreate("AXFullScreenButton")
	attrTitle = cfStringCreate("AXTitle")
	attrPosition = cfStringCreate("AXPosition")
	attrSize = cfStringCreate("AXSize")
	attrCloseButton = cfStringCreate("AXCloseButton")
	attrMain = cfStringCreate("AXMain")
	actionPress = cfStringCreate("AXPress")
	actionRaise = cfStringCreate("AXRaise")
}

// axSession asks Accessibility about windows during one round. It looks
// up each app's window list once and releases everything on close, so
// it must not outlive the round.
type axSession struct {
	apps map[int32]*axApp
}

// axApp is one app's element and its AXWindows array, which keeps the
// window elements alive.
type axApp struct {
	app     uintptr
	windows uintptr
	err     error
}

func newAXSession() *axSession {
	load()
	return &axSession{apps: map[int32]*axApp{}}
}

// close releases the elements the session holds.
func (s *axSession) close() {
	for _, a := range s.apps {
		if a.windows != 0 {
			cfRelease(a.windows)
		}
		if a.app != 0 {
			cfRelease(a.app)
		}
	}
	s.apps = nil
}

// app returns the Accessibility state of pid, fetching it on first use.
func (s *axSession) app(pid int32) *axApp {
	if a, ok := s.apps[pid]; ok {
		return a
	}
	a := &axApp{app: axUIElementCreateApplication(pid)}
	s.apps[pid] = a
	if a.app == 0 {
		a.err = fmt.Errorf("pid %d: no Accessibility element", pid)
		return a
	}
	axUIElementSetMessagingTimeout(a.app, axTimeout)
	var windows uintptr
	if code := axUIElementCopyAttributeValue(a.app, attrWindows, &windows); code != axErrorSuccess {
		a.err = fmt.Errorf("pid %d: AXWindows: AXError %d", pid, code)
		return a
	}
	a.windows = windows
	return a
}

// element finds the Accessibility element of w: by window number through
// _AXUIElementGetWindow, or else the first element whose frame is w's
// bounds within Tolerance. The element is owned by the session.
func (s *axSession) element(w CGWindow) (uintptr, error) {
	a := s.app(w.PID)
	if a.err != nil {
		return 0, a.err
	}
	n := cfArrayGetCount(a.windows)
	for i := range n {
		el := a.window(i)
		if matchesWindow(el, w) {
			return el, nil
		}
	}
	return 0, fmt.Errorf("%s window %d: not among its %d AXWindows", w.Owner, w.ID, n)
}

// matchesWindow reports whether el is the element of w.
func matchesWindow(el uintptr, w CGWindow) bool {
	if axUIElementGetWindow != nil {
		var id uint32
		return axUIElementGetWindow(el, &id) == axErrorSuccess && id == w.ID
	}
	frame, ok := elementFrame(el)
	return ok && SameRect(frame, w.Bounds)
}

// Inspect implements Inspector. Only a window that cannot be found is an
// error; an attribute the window lacks reads as its zero value, which for
// AXSubrole makes the window not Normal.
func (s *axSession) Inspect(w CGWindow) (Info, error) {
	el, err := s.element(w)
	if err != nil {
		return Info{}, err
	}
	return elementInfo(el), nil
}

// AppWindows implements Inspector. It reads the app's standard windows
// from AXWindows, which, unlike the CoreGraphics list of windows on
// screen, holds minimized windows too. A window without a CGWindowID
// cannot be compared by age and is left out; without
// _AXUIElementGetWindow no window has one, which is an error.
func (s *axSession) AppWindows(pid int32) ([]AppWindow, error) {
	if axUIElementGetWindow == nil {
		return nil, errors.New("window IDs are unavailable: no _AXUIElementGetWindow")
	}
	elements, err := s.standardWindows(pid)
	if err != nil {
		return nil, err
	}
	windows := make([]AppWindow, 0, len(elements))
	for _, el := range elements {
		id, ok := windowID(el)
		if !ok {
			continue
		}
		windows = append(windows, AppWindow{ID: id, Info: elementInfo(el)})
	}
	return windows, nil
}

// windowID returns the CGWindowID of the window element el, false when
// it has none. The caller checks that _AXUIElementGetWindow is available.
func windowID(el uintptr) (uint32, bool) {
	var id uint32
	ok := axUIElementGetWindow(el, &id) == axErrorSuccess && id != 0
	return id, ok
}

// CloseStandardWindows closes every standard window of the app of pid,
// minimized ones included, by pressing its close button, as a click on
// the red button does; floating windows, panels and dialogs are left
// alone. It returns how many windows it pressed the button of. A window
// that fails stops nothing: the others are still closed, and the first
// failure is returned. It runs on the calling goroutine, like the rest
// of the package's Accessibility calls.
func CloseStandardWindows(pid int32) (closed int, err error) {
	session := newAXSession()
	defer session.close()
	windows, err := session.standardWindows(pid)
	if err != nil {
		return 0, err
	}
	for i, el := range windows {
		if pressErr := pressClose(el); pressErr != nil {
			if err == nil {
				err = fmt.Errorf("pid %d window %d of %d: %w", pid, i+1, len(windows), pressErr)
			}
			continue
		}
		closed++
	}
	return closed, err
}

// CountStandardWindows returns how many standard windows the app of pid
// has, minimized ones included, as AXWindows lists those too. It runs on
// the calling goroutine, like the rest of the package's Accessibility
// calls.
func CountStandardWindows(pid int32) (int, error) {
	session := newAXSession()
	defer session.close()
	windows, err := session.standardWindows(pid)
	if err != nil {
		return 0, err
	}
	return len(windows), nil
}

// RaiseNextWindow raises one of the app's standard windows, given
// whether the app is frontmost. The standard windows are taken in the
// order of their CGWindowIDs, the lowest being the app's primary window
// and the others its child windows. A frontmost app moves to the window
// after the main one, wrapping around, or the first when none is main.
// An app in the background keeps a child window that is main, leaving it
// as it is for the activation to bring forward, and with the primary
// window main, or none, moves to the first child. The window chosen is
// un-minimized, raised and made main. The order is the IDs' rather than
// AXWindows', which is front to back: there the raised window would
// become the first and the old main the second, so clicks would only
// swap the two frontmost windows. A window without a CGWindowID cannot
// be placed in that order and is left out. With fewer than two windows
// to cycle there is nothing to raise, and it does nothing. It runs on
// the calling goroutine, like the rest of the package's Accessibility
// calls.
func RaiseNextWindow(pid int32, appFrontmost bool) error {
	if axUIElementGetWindow == nil {
		return errors.New("window IDs are unavailable: no _AXUIElementGetWindow")
	}
	session := newAXSession()
	defer session.close()
	windows, err := session.standardWindows(pid)
	if err != nil {
		return err
	}
	byID := make(map[uint32]uintptr, len(windows))
	ids := make([]uint32, 0, len(windows))
	var mainID uint32
	for _, el := range windows {
		id, ok := windowID(el)
		if !ok {
			continue
		}
		byID[id] = el
		ids = append(ids, id)
		if boolAttr(el, attrMain) {
			mainID = id
		}
	}
	slices.Sort(ids)
	next := windowToRaise(mainID, ids, appFrontmost)
	if next == 0 {
		return nil
	}
	el := byID[next]
	// AXRaise leaves a minimized window in the Dock, so it is restored
	// first.
	if boolAttr(el, attrMinimized) {
		if code := axUIElementSetAttributeValue(el, attrMinimized, kCFBooleanFalse); code != axErrorSuccess {
			return fmt.Errorf("pid %d window %d: unset AXMinimized: AXError %d", pid, next, code)
		}
	}
	if code := axUIElementPerformAction(el, actionRaise); code != axErrorSuccess {
		return fmt.Errorf("pid %d window %d: AXRaise: AXError %d", pid, next, code)
	}
	if code := axUIElementSetAttributeValue(el, attrMain, kCFBooleanTrue); code != axErrorSuccess {
		return fmt.Errorf("pid %d window %d: set AXMain: AXError %d", pid, next, code)
	}
	return nil
}

// standardWindows returns the elements of the app's standard windows, in
// the order of its AXWindows; the session owns them.
func (s *axSession) standardWindows(pid int32) ([]uintptr, error) {
	a := s.app(pid)
	if a.err != nil {
		return nil, a.err
	}
	n := cfArrayGetCount(a.windows)
	var windows []uintptr
	for i := range n {
		el := a.window(i)
		if stringAttr(el, attrSubrole) == standardWindow {
			windows = append(windows, el)
		}
	}
	return windows, nil
}

// pressClose presses the close button of a window element.
func pressClose(el uintptr) error {
	var button uintptr
	if code := axUIElementCopyAttributeValue(el, attrCloseButton, &button); code != axErrorSuccess {
		return fmt.Errorf("AXCloseButton: AXError %d", code)
	}
	defer cfRelease(button)
	axUIElementSetMessagingTimeout(button, axTimeout)
	if code := axUIElementPerformAction(button, actionPress); code != axErrorSuccess {
		return fmt.Errorf("AXPress: AXError %d", code)
	}
	return nil
}

// window returns the i-th element of the app's AXWindows, with the
// messaging timeout set so a hung app cannot stall the round.
func (a *axApp) window(i int) uintptr {
	el := cfArrayGetValueAtIndex(a.windows, i)
	axUIElementSetMessagingTimeout(el, axTimeout)
	return el
}

// elementInfo reads the Info of a window element.
func elementInfo(el uintptr) Info {
	return Info{
		Subrole:          stringAttr(el, attrSubrole),
		Minimized:        boolAttr(el, attrMinimized),
		FullScreen:       boolAttr(el, attrFullScreen),
		FullScreenButton: hasAttr(el, attrFullScreenButton),
		Title:            stringAttr(el, attrTitle),
	}
}

// setFrame moves and resizes w, or only moves it when positionOnly, so a
// window keeps whatever size it has. Position goes first, then size, then
// the position is read back and set again if the app moved the window
// while resizing it, as some do to keep it on screen.
func (s *axSession) setFrame(w CGWindow, to Rect, positionOnly bool) error {
	el, err := s.element(w)
	if err != nil {
		return err
	}
	pos := cgPoint{X: to.X, Y: to.Y}
	if err := setValue(el, attrPosition, axValueCGPoint, unsafe.Pointer(&pos)); err != nil {
		return err
	}
	if !positionOnly {
		size := cgSize{W: to.W, H: to.H}
		if err := setValue(el, attrSize, axValueCGSize, unsafe.Pointer(&size)); err != nil {
			return err
		}
	}
	got, ok := elementFrame(el)
	positionKept := ok && near(got.X, to.X) && near(got.Y, to.Y)
	if positionKept {
		return nil
	}
	return setValue(el, attrPosition, axValueCGPoint, unsafe.Pointer(&pos))
}

// copyAttr returns an attribute value the caller must release, or 0.
func copyAttr(el, attr uintptr) uintptr {
	var v uintptr
	if axUIElementCopyAttributeValue(el, attr, &v) != axErrorSuccess {
		return 0
	}
	return v
}

func stringAttr(el, attr uintptr) string {
	v := copyAttr(el, attr)
	if v == 0 {
		return ""
	}
	defer cfRelease(v)
	return cfStringValue(v)
}

func boolAttr(el, attr uintptr) bool {
	v := copyAttr(el, attr)
	if v == 0 {
		return false
	}
	defer cfRelease(v)
	return v == kCFBooleanTrue
}

// hasAttr reports whether el has a value for attr, as a window with a
// full screen button has one for AXFullScreenButton.
func hasAttr(el, attr uintptr) bool {
	v := copyAttr(el, attr)
	if v == 0 {
		return false
	}
	cfRelease(v)
	return true
}

// elementFrame reads AXPosition and AXSize.
func elementFrame(el uintptr) (Rect, bool) {
	var pos cgPoint
	var size cgSize
	hasPos := axValueAttr(el, attrPosition, axValueCGPoint, unsafe.Pointer(&pos))
	hasSize := axValueAttr(el, attrSize, axValueCGSize, unsafe.Pointer(&size))
	if !hasPos || !hasSize {
		return Rect{}, false
	}
	return Rect{X: pos.X, Y: pos.Y, W: size.W, H: size.H}, true
}

// axValueAttr reads an AXValue attribute of valueType into out.
func axValueAttr(el, attr uintptr, valueType uint32, out unsafe.Pointer) bool {
	v := copyAttr(el, attr)
	if v == 0 {
		return false
	}
	defer cfRelease(v)
	return axValueGetValue(v, valueType, out)
}

// setValue sets an AXValue attribute of valueType from value.
func setValue(el, attr uintptr, valueType uint32, value unsafe.Pointer) error {
	v := axValueCreate(valueType, value)
	if v == 0 {
		return errors.New("AXValueCreate failed")
	}
	defer cfRelease(v)
	if code := axUIElementSetAttributeValue(el, attr, v); code != axErrorSuccess {
		return fmt.Errorf("set %s: AXError %d", cfStringValue(attr), code)
	}
	return nil
}
