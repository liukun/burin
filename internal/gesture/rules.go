// Package gesture holds mouse gestures: hold a mouse button other than the
// left one, drag in one direction and release, and the rule for that
// button, direction and frontmost app runs. A press that is no gesture,
// or one no rule takes, is replayed as the click it was.
//
// rules.go, load.go and recognizer.go are plain Go so their tests run on
// any platform; the event tap lives in package eventtap.
package gesture

import (
	"fmt"
	"slices"

	"burin/internal/workspace"
)

// Event types, the values of CGEventType.
const (
	EventRightMouseDown    = 3
	EventRightMouseUp      = 4
	EventRightMouseDragged = 6
	EventOtherMouseDown    = 25
	EventOtherMouseUp      = 26
	EventOtherMouseDragged = 27
)

// The CGEventMask bits of the right mouse button's events and of the
// other buttons' events.
const (
	rightMask = 1<<EventRightMouseDown | 1<<EventRightMouseUp | 1<<EventRightMouseDragged
	otherMask = 1<<EventOtherMouseDown | 1<<EventOtherMouseUp | 1<<EventOtherMouseDragged
)

// Direction is the way a gesture drags.
type Direction string

// Directions.
const (
	Left  Direction = "left"
	Right Direction = "right"
	Up    Direction = "up"
	Down  Direction = "down"
)

var directions = map[string]Direction{"left": Left, "right": Right, "up": Up, "down": Down}

// Button is a mouse button by its CoreGraphics number
// (kCGMouseEventButtonNumber): 0 is the left button, 1 the right, 2 the
// middle and 3 on the side buttons.
type Button int

// Buttons with a name of their own.
const (
	ButtonLeft   Button = 0
	ButtonRight  Button = 1
	ButtonMiddle Button = 2
)

func (b Button) String() string {
	switch b {
	case ButtonLeft:
		return "left"
	case ButtonRight:
		return "right"
	case ButtonMiddle:
		return "middle"
	default:
		return fmt.Sprintf("button%d", int(b))
	}
}

// Rule is one gesture of a button and the raw content it runs, before
// placeholder expansion.
type Rule struct {
	Direction Direction
	// Title is the rest of the heading, for notifications.
	Title string
	// Apps lists the names and bundle IDs of the apps the rule is for;
	// empty means every app.
	Apps    []string
	Content string
}

// Name names the rule of button b in notifications: its title, or else
// the button and the direction.
func (r Rule) Name(b Button) string {
	if r.Title != "" {
		return r.Title
	}
	return b.String() + " " + string(r.Direction)
}

// matchesApp reports whether the rule's :app: lines list app, by its name
// or its bundle ID.
func (r Rule) matchesApp(app workspace.App) bool {
	byName := app.Name != "" && slices.Contains(r.Apps, app.Name)
	byBundle := app.BundleID != "" && slices.Contains(r.Apps, app.BundleID)
	return byName || byBundle
}

// Config is one mouse file: the button it is named after and its rules in
// file order. Err is set when the file could not be read or parsed; the
// button then has no rules and is not listened to.
type Config struct {
	Button Button
	File   string
	Rules  []Rule
	Err    error
}

// Set is the result of scanning a mouse directory.
type Set struct {
	Configs []Config
	// Errors lists the files that do not name a supported button, and
	// directory errors. Such files are skipped.
	Errors []error
}

// config returns the loaded config of button b, if it has one that
// parsed.
func (s *Set) config(b Button) (Config, bool) {
	if s == nil {
		return Config{}, false
	}
	for _, c := range s.Configs {
		if c.Button == b && c.Err == nil {
			return c, true
		}
	}
	return Config{}, false
}

// Has reports whether button b has a file that loaded, so its presses
// are gestures to recognize.
func (s *Set) Has(b Button) bool {
	_, ok := s.config(b)
	return ok
}

// Mask is the CGEventMask of the events the buttons of s need: the right
// mouse events for the right button, the other mouse events for the
// middle and side buttons. Without buttons it is zero, and no mouse event
// reaches the tap.
func (s *Set) Mask() uint64 {
	if s == nil {
		return 0
	}
	var mask uint64
	for _, c := range s.Configs {
		switch {
		case c.Err != nil:
		case c.Button == ButtonRight:
			mask |= rightMask
		default:
			mask |= otherMask
		}
	}
	return mask
}

// AllErrors returns the errors of s: those of the files that name no
// supported button, then those of the files that failed to load.
func (s *Set) AllErrors() []error {
	errs := slices.Clone(s.Errors)
	for _, c := range s.Configs {
		if c.Err != nil {
			errs = append(errs, c.Err)
		}
	}
	return errs
}

// Match returns the rule for a gesture of button b in direction d over
// app: the first rule of that direction whose :app: lines list app, or
// else the first one without an :app: line.
func (s *Set) Match(b Button, d Direction, app workspace.App) (Rule, bool) {
	c, ok := s.config(b)
	if !ok {
		return Rule{}, false
	}
	i := c.match(d, app)
	if i < 0 {
		return Rule{}, false
	}
	return c.Rules[i], true
}

// Available returns the rules of button b that a gesture over app would
// run, one per direction at most, in file order: what Match returns for
// each direction.
func (s *Set) Available(b Button, app workspace.App) []Rule {
	c, ok := s.config(b)
	if !ok {
		return nil
	}
	var out []Rule
	for i, r := range c.Rules {
		if c.match(r.Direction, app) == i {
			out = append(out, r)
		}
	}
	return out
}

// match returns the index of the rule Match picks for direction d over
// app, or -1.
func (c Config) match(d Direction, app workspace.App) int {
	global := -1
	for i, r := range c.Rules {
		if r.Direction != d {
			continue
		}
		if len(r.Apps) == 0 {
			if global < 0 {
				global = i
			}
			continue
		}
		if r.matchesApp(app) {
			return i
		}
	}
	return global
}
