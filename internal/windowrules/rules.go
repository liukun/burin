// Package windowrules keeps chosen app windows pinned to a corner of a
// display and keeps other windows clear of a left margin, ported from the
// Hammerspoon window_rules module. Windows are listed through CoreGraphics,
// which costs no Accessibility IPC, so a scan is cheap enough to run every
// few seconds; Accessibility is only asked about apps whose windows
// changed, and only to move windows.
//
// rules.go, plan.go, load.go and missioncontrol.go are plain Go so their
// tests run on any platform; the CoreGraphics and Accessibility calls live
// in the darwin files.
package windowrules

import (
	"cmp"
	"math"
	"regexp"
	"slices"
)

// Tolerance is how far, in points, two coordinates or sizes may differ and
// still count as equal, so a window a few points off its spot is left
// alone instead of being nudged every round.
const Tolerance = 5

// Rect is a frame in global screen coordinates, origin at the top left of
// the primary display, as CoreGraphics window bounds and Accessibility
// positions use.
type Rect struct{ X, Y, W, H float64 }

// Size is the size of a display's full frame, which identifies a display
// across reconnects better than its ID.
type Size struct{ W, H float64 }

// Range is a fraction range of the usable area, 0 to 1.
type Range struct{ Min, Max float64 }

// Anchor is the corner of the usable area a pinned window sticks to.
type Anchor int

// Anchors.
const (
	// TopLeft puts the window's top left corner at the usable area's.
	TopLeft Anchor = iota
	// BottomLeft puts the window's bottom left corner at the usable
	// area's, above the Dock.
	BottomLeft
)

// Display is one connected display.
type Display struct {
	// Frame is the whole display.
	Frame Rect
	// Visible is the usable area: Frame without the menu bar and Dock.
	Visible Rect
}

// Rule pins the windows of one app to a corner of a display.
type Rule struct {
	// App is the exact process name, as kCGWindowOwnerName reports it.
	// Helpers such as "自动填充 (Trello)" must not match "Trello".
	App string
	// Screen picks the target display by full frame size. When set and no
	// such display is connected, the rule is skipped; windows on other
	// displays move to it. When nil, a window stays on its own display.
	Screen *Size
	Anchor Anchor
	// Width and Height bound the window size as fractions of the usable
	// area. A size inside the range is kept, one outside is clamped.
	Width, Height Range
	// Title, when set, keeps only windows whose title matches.
	Title *regexp.Regexp
	// Oldest keeps only the app's oldest window, the one with the lowest
	// CGWindowID. Slack adds "[Main]" to its main window's title only
	// during a huddle, and a DM pop-out has the same title format as the
	// main window, so no title tells them apart; but CGWindowIDs grow
	// monotonically, and the main window, created at launch, has the
	// lowest one.
	Oldest bool
	// FullScreenable keeps only windows with a full screen button, which
	// tells a main window from settings windows of the same subrole.
	FullScreenable bool
}

// MarginRule keeps the windows on one display at least Margin points from
// the left edge of its usable area.
type MarginRule struct {
	// Screen picks the display by full frame size; without it the rule is
	// skipped.
	Screen Size
	Margin float64
	// Except names apps whose windows the rule leaves alone. Windows
	// pinned by a Rule are left alone without being listed.
	Except []string
}

// Config is the rules of window_rules.md. Pins run before Margins, so the
// margin rules know which windows are pinned. Of several Pins for one
// app, ActivePins picks the one that runs.
type Config struct {
	Pins    []Rule
	Margins []MarginRule
}

// Len is the number of rules.
func (c Config) Len() int { return len(c.Pins) + len(c.Margins) }

// CGWindow is one on-screen window as CoreGraphics lists it.
type CGWindow struct {
	ID     uint32
	PID    int32
	Owner  string
	Bounds Rect
}

// Info is what Accessibility tells about a window. CoreGraphics cannot
// tell a huddle popup or a settings window from a main window, so rules
// need this before they touch a window.
type Info struct {
	Subrole          string
	Minimized        bool
	FullScreen       bool
	FullScreenButton bool
	Title            string
}

// standardWindow is the subrole of ordinary document and app windows, as
// opposed to dialogs, panels and floating windows.
const standardWindow = "AXStandardWindow"

// Normal reports whether rules may move the window: a standard window
// that is neither minimized nor full screen.
func (i Info) Normal() bool {
	return i.Subrole == standardWindow && !i.Minimized && !i.FullScreen
}

// Window is a listed window together with what Accessibility says about
// it.
type Window struct {
	CG   CGWindow
	Info Info
}

func near(a, b float64) bool { return math.Abs(a-b) <= Tolerance }

// SameRect reports whether a and b are equal within Tolerance.
func SameRect(a, b Rect) bool {
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.W, b.W) && near(a.H, b.H)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// ExpectedFrame is where rule wants a window with frame win, given the
// usable area avail of its target display: the size clamped into the
// rule's ranges, then the frame pinned to the anchor corner.
func ExpectedFrame(rule Rule, avail, win Rect) Rect {
	w := clamp(win.W, avail.W*rule.Width.Min, avail.W*rule.Width.Max)
	h := clamp(win.H, avail.H*rule.Height.Min, avail.H*rule.Height.Max)
	y := avail.Y
	if rule.Anchor == BottomLeft {
		y = avail.Y + avail.H - h
	}
	return Rect{X: avail.X, Y: y, W: w, H: h}
}

// LeftMarginFrame is where rule wants a window with frame win, given the
// usable area avail of the rule's display, and false when the window
// already keeps the margin. A window too far left moves right and, if it
// would then cross the right edge, narrows. Unlike other comparisons
// there is no tolerance: x one point short of the margin still moves.
func LeftMarginFrame(rule MarginRule, avail, win Rect) (Rect, bool) {
	minX := avail.X + rule.Margin
	if win.X >= minX {
		return Rect{}, false
	}
	rightEdge := avail.X + avail.W
	w := math.Min(win.W, rightEdge-minX)
	return Rect{X: minX, Y: win.Y, W: w, H: win.H}, true
}

// SelectWindows returns the windows rule applies to, in order. Windows
// without a full screen button (when FullScreenable) and windows whose
// title does not match Title are dropped first; then, when Oldest is set,
// only the standard window with the lowest CGWindowID is kept.
func SelectWindows(rule Rule, wins []Window) []Window {
	filtered := rule.FullScreenable || rule.Title != nil || rule.Oldest
	if !filtered {
		return wins
	}
	var selected []Window
	for _, w := range wins {
		if !passesFilters(rule, w.Info) {
			continue
		}
		selected = append(selected, w)
	}
	if !rule.Oldest || len(selected) == 0 {
		return selected
	}
	oldest := slices.MinFunc(selected, func(a, b Window) int { return cmp.Compare(a.CG.ID, b.CG.ID) })
	return []Window{oldest}
}

// passesFilters reports whether a window passes rule's FullScreenable and
// Title filters and, when Oldest is set, is a standard window. Only
// standard windows are compared by age, as AppWindows lists no other
// kind, so an older dialog, or a window whose inspection failed, does not
// keep the main window from being pinned.
func passesFilters(rule Rule, info Info) bool {
	passesFullScreen := !rule.FullScreenable || info.FullScreenButton
	passesTitle := rule.Title == nil || rule.Title.MatchString(info.Title)
	passesOldest := !rule.Oldest || info.Subrole == standardWindow
	return passesFullScreen && passesTitle && passesOldest
}

// IsExcepted reports whether a margin rule leaves w alone: it is pinned
// by a Rule this round (in handled), or its app is in the rule's Except.
func IsExcepted(rule MarginRule, w CGWindow, handled map[uint32]bool) bool {
	return handled[w.ID] || slices.Contains(rule.Except, w.Owner)
}

// WindowsOfApp returns the windows owned by the process named exactly
// app, in order.
func WindowsOfApp(app string, wins []CGWindow) []CGWindow {
	var out []CGWindow
	for _, w := range wins {
		if w.Owner == app {
			out = append(out, w)
		}
	}
	return out
}

// ActivePins returns the pin rules that run with the connected displays:
// of several rules for one app, the first whose Screen is nil or
// connected, so a rule without a Screen listed after one with it is the
// fallback when that display is unplugged. The rest keep their order.
func ActivePins(pins []Rule, displays []Display) []Rule {
	var out []Rule
	chosen := map[string]bool{}
	for _, rule := range pins {
		connected := rule.Screen == nil || FindScreenBySize(*rule.Screen, displays) != nil
		if chosen[rule.App] || !connected {
			continue
		}
		chosen[rule.App] = true
		out = append(out, rule)
	}
	return out
}

// FindScreenBySize returns the first display whose full frame is size
// within Tolerance, or nil.
func FindScreenBySize(size Size, displays []Display) *Display {
	for i := range displays {
		f := displays[i].Frame
		if near(f.W, size.W) && near(f.H, size.H) {
			return &displays[i]
		}
	}
	return nil
}

// ScreenOf returns the display that overlaps r the most, as hs.window's
// screen() picks it, or nil when r is on no display.
func ScreenOf(r Rect, displays []Display) *Display {
	var best *Display
	bestArea := 0.0
	for i := range displays {
		if area := overlap(r, displays[i].Frame); area > bestArea {
			best, bestArea = &displays[i], area
		}
	}
	return best
}

// overlap is the area r and s share.
func overlap(r, s Rect) float64 {
	w := math.Min(r.X+r.W, s.X+s.W) - math.Max(r.X, s.X)
	h := math.Min(r.Y+r.H, s.Y+s.H) - math.Max(r.Y, s.Y)
	return math.Max(w, 0) * math.Max(h, 0)
}

// isCGFullScreen reports whether r covers its whole display, as a full
// screen window does, so it can be skipped without asking Accessibility.
func isCGFullScreen(r Rect, displays []Display) bool {
	d := ScreenOf(r, displays)
	return d != nil && SameRect(r, d.Frame)
}
