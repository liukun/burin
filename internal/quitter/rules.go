// Package quitter hides or quits apps that have not been the frontmost
// app for a while, after Quitter for Mac. Each rule names an app, a
// period and an action; the engine notes when every running app was last
// frontmost and acts on those whose period has passed. Background
// activity does not count: only switching to an app resets its timer.
//
// rules.go, plan.go and load.go are plain Go so their tests run on any
// platform; the NSWorkspace calls live in the darwin files.
package quitter

import (
	"fmt"
	"time"
)

// Action is what a rule does to an app that has been inactive too long.
type Action int

// Actions.
const (
	// Hide hides the app, as Cmd-H does. An app already hidden is left
	// alone.
	Hide Action = iota
	// Quit asks the app to terminate, as Cmd-Q does: an app with unsaved
	// documents asks about them first. It is never force-quit.
	Quit
)

func (a Action) String() string {
	if a == Quit {
		return "quit"
	}
	return "hide"
}

// Match is the property of a running app a rule compares its Value with.
type Match int

// Matches.
const (
	// ByName compares the app's localized name, the section heading.
	ByName Match = iota
	// ByBundle compares the app's bundle identifier.
	ByBundle
	// ByPath compares the path of the app's bundle.
	ByPath
)

// Rule hides or quits one app after it has not been frontmost for After.
type Rule struct {
	// Name is the section heading, for the log. With ByName it is also
	// what the app's name must equal.
	Name   string
	Match  Match
	Value  string
	After  time.Duration
	Action Action
}

// Matches reports whether the rule is about app. Names, bundle
// identifiers and paths are compared exactly.
func (r Rule) Matches(app App) bool {
	switch r.Match {
	case ByBundle:
		return app.BundleID == r.Value
	case ByPath:
		return app.Path == r.Value
	default:
		return app.Name == r.Value
	}
}

func (r Rule) String() string {
	return fmt.Sprintf("%s after %v", r.Action, r.After)
}

// Config is the rules of quitter.md, in file order. Of several rules
// matching one app, the first runs.
type Config struct {
	Rules []Rule
}

// Len is the number of rules.
func (c Config) Len() int { return len(c.Rules) }

// ruleFor returns the first rule matching app.
func (c Config) ruleFor(app App) (Rule, bool) {
	for _, r := range c.Rules {
		if r.Matches(app) {
			return r, true
		}
	}
	return Rule{}, false
}

// App is one running app as NSWorkspace lists it.
type App struct {
	PID      int32
	Name     string
	BundleID string
	// Path is the path of the app's bundle, empty for a process without
	// one.
	Path string
	// Active is set for the frontmost app.
	Active bool
	Hidden bool
}
