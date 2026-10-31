package quitter

import (
	"fmt"
	"time"
)

// State holds, per pid, when each running app was last frontmost, or
// first seen when it has not been frontmost since. The zero value is
// empty.
type State map[int32]time.Time

// Step is one action the engine takes on an app.
type Step struct {
	App  App
	Rule Rule
	// Idle is how long the app has not been frontmost.
	Idle time.Duration
}

// String describes the step for the log.
func (s Step) String() string {
	return fmt.Sprintf("%s %s (pid %d), not frontmost for %v (rule ## %s: %s)",
		s.Rule.Action, s.App.Name, s.App.PID, s.Idle.Round(time.Second), s.Rule.Name, s.Rule)
}

// Plan decides what to do with the running apps at now. It returns the
// steps to take and the state for the next round:
//
//   - the frontmost app, and an app seen for the first time, count as
//     active now, so a freshly launched app gets a full period;
//   - apps no longer running are dropped;
//   - an app not frontmost for longer than its rule's period is hidden,
//     unless it already is, or quit. A quit app counts as active now, so
//     one that stays open, e.g. asking about unsaved changes, is asked
//     again only after another full period;
//   - an app whose rule is close_window has its standard windows closed
//     every round until none are left, hidden or not. Closing them does
//     not reset its timer: the app was not used, only tidied.
func Plan(cfg Config, apps []App, prev State, now time.Time) ([]Step, State) {
	next := make(State, len(apps))
	var steps []Step
	for _, app := range apps {
		last, seen := prev[app.PID]
		if app.Active || !seen {
			next[app.PID] = now
			continue
		}
		next[app.PID] = last
		rule, ok := cfg.ruleFor(app)
		if !ok {
			continue
		}
		idle := now.Sub(last)
		if idle <= rule.After {
			continue
		}
		alreadyHidden := rule.Action == Hide && app.Hidden
		if alreadyHidden {
			continue
		}
		steps = append(steps, Step{App: app, Rule: rule, Idle: idle})
		if rule.Action == Quit {
			next[app.PID] = now
		}
	}
	return steps, next
}

// MarkActive records that the app of pid was frontmost at now and returns
// the state, which it updates in place unless it is nil. A pid the state
// does not know yet is added: it is clearly running. A pid of 0 or less,
// the frontmost pid when there is none, leaves the state alone. Plan
// drops pids that are not among the running apps it is given, such as
// burin's own.
func MarkActive(state State, pid int32, now time.Time) State {
	if pid <= 0 {
		return state
	}
	if state == nil {
		state = State{}
	}
	state[pid] = now
	return state
}

// withoutPID returns apps without the process pid, so the engine never
// acts on its own app.
func withoutPID(apps []App, pid int32) []App {
	out := make([]App, 0, len(apps))
	for _, app := range apps {
		if app.PID != pid {
			out = append(out, app)
		}
	}
	return out
}
