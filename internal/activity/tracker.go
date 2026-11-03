// Package activity tracks how long the user has been working without a
// break, from the system's idle time, and reminds them to rest after the
// configured work time. It is a port of a Hammerspoon script: the tracker
// keeps its state machine and is a section of the bar, which shows its
// title on every display.
package activity

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Defaults of the thresholds timer.md may set.
const (
	// DefaultIdle is how long the user must be idle in a row for an
	// active period to end.
	DefaultIdle = 120 * time.Second
	// DefaultWork is how long an active period runs before the reminder.
	DefaultWork = 25 * time.Minute
)

// DefaultConfig is the tracker's config without a timer.md.
var DefaultConfig = Config{Idle: DefaultIdle, Work: DefaultWork}

// Config is the tracker's thresholds the user may set. Both are
// positive.
type Config struct {
	// Idle is how long the user must be idle in a row for an active
	// period to end.
	Idle time.Duration
	// Work is how long an active period runs before the reminder.
	Work time.Duration
}

// Internal thresholds of the tracker.
const (
	// ActiveThreshold is how long the user must be active in a row for an
	// idle period to end; it is counted back into the new active period.
	ActiveThreshold = 20 * time.Second
	// ActivityWindow is how recent the last input must be for the user to
	// count as active.
	ActivityWindow = 10 * time.Second
	// PollInterval is how often Tick is meant to be called.
	PollInterval = 2 * time.Second
	// SaveInterval is how often Tick persists the state besides on
	// transitions.
	SaveInterval = 60 * time.Second
	// renotifyInterval is how long after a reminder another one follows
	// while the user keeps working past the work time.
	renotifyInterval = 60 * time.Second
	// manualRestGrace is how long after Rest input is ignored, so the
	// click that chose it does not end the rest right away.
	manualRestGrace = 10 * time.Second
	// overtimeStep adds a warning sign to the title for every so long
	// past the work time.
	overtimeStep = 5 * time.Minute
	// maxWarnings caps the warning signs, half before the title and half
	// after.
	maxWarnings = 6
	// dateLayout is the day the today counters belong to.
	dateLayout = "2006-01-02"
)

// Status is whether the user is in an active or an idle period.
type Status string

// Statuses of the tracker, as persisted.
const (
	Active Status = "active"
	Idle   Status = "idle"
)

// Snapshot is what the bar shows of the tracker.
type Snapshot struct {
	Status Status
	// Elapsed is how long the current period, active or idle, has run.
	Elapsed time.Duration
	// IdleIn is how long the user must stay idle for the active period to
	// end; zero while idle.
	IdleIn time.Duration
	// Paused is set while active when the last tick found the user idle.
	Paused bool
	// Resuming is set while idle when the user has been back for less
	// than ActiveThreshold: the idle period may end or go on.
	Resuming    bool
	TodayActive time.Duration
	Sessions    int
	// Overtime is set while active past Work.
	Overtime bool
	// Work is the work time in force, which Overtime is past.
	Work time.Duration
}

// Tracker is the state machine of active and idle periods. Its methods
// are safe from any goroutine; its callbacks run without its lock held,
// so they may call back into it.
type Tracker struct {
	// OnWorkLimit is called when the active period reaches the work time,
	// and
	// again every minute while the user keeps working, with how long the
	// period has run.
	OnWorkLimit func(elapsed time.Duration)
	// Persist is called with the state to save, on transitions and every
	// SaveInterval.
	Persist func(State)
	// OnChange is called after every tick and action, as what Snapshot
	// returns may have changed.
	OnChange func()

	mu          sync.Mutex
	cfg         Config
	status      Status
	activeStart time.Time
	// idleStart is zero while active.
	idleStart   time.Time
	todayActive time.Duration
	sessions    int
	todayDate   string
	lastUpdate  time.Time
	sleeping    bool
	// disabled is set while timer.md turns the timer off: like sleeping,
	// but by the user's choice and apart from it.
	disabled bool
	// notified is set once the active period's first reminder went out.
	notified   bool
	lastNotify time.Time
	// manualIdleUntil ignores input until then, after Rest.
	manualIdleUntil time.Time
	// consecIdle counts idle time while active, consecActive active time
	// while idle.
	consecIdle   time.Duration
	consecActive time.Duration
	lastSave     time.Time
	// paused is whether the last tick found the user idle.
	paused bool
}

// NewTracker starts a tracker with cfg in an active period beginning at
// now.
func NewTracker(now time.Time, cfg Config) *Tracker {
	return &Tracker{
		cfg:         cfg,
		status:      Active,
		activeStart: now,
		todayDate:   now.Format(dateLayout),
		lastUpdate:  now,
	}
}

// Tick advances the tracker to now, given how long the system has had
// no input and whether the frontmost app is an idle app, whose use does
// not end a break. It does nothing while asleep or disabled.
func (t *Tracker) Tick(now time.Time, systemIdle time.Duration, frontIdle bool) {
	t.mu.Lock()
	if t.disabled {
		t.mu.Unlock()
		return
	}
	var notify func()
	var save *State
	defer func() {
		t.mu.Unlock()
		t.after(notify, save)
	}()
	if t.sleeping {
		return
	}

	// A clock set back counts nothing rather than taking time away.
	delta := max(now.Sub(t.lastUpdate), 0)
	t.rollover(now)
	// The front app only matters during a break: chatting while working
	// is still time at the computer, but a glance at a message while
	// away should not count as coming back.
	noInput := systemIdle >= ActivityWindow
	onBreak := t.status == Idle
	isIdle := noInput || (onBreak && frontIdle)
	t.paused = isIdle
	t.lastUpdate = now

	switch t.status {
	case Active:
		if isIdle {
			t.consecIdle += delta
		} else {
			t.consecIdle = 0
			t.todayActive += delta
		}
		notify = t.checkWorkLimit(now, isIdle)
		if t.consecIdle >= t.cfg.Idle {
			t.status = Idle
			t.idleStart = now.Add(-t.consecIdle)
			t.consecActive = 0
		}
	case Idle:
		inGrace := now.Before(t.manualIdleUntil)
		if !isIdle && !inGrace {
			t.consecActive += delta
		} else {
			t.consecActive = 0
		}
		if t.consecActive >= ActiveThreshold {
			// The time it took to tell the user is back was work too.
			resumed := t.consecActive
			t.todayActive += resumed
			t.resetToActive(now.Add(-resumed))
			// The time counted back is in todayActive already: the next
			// tick counts from now.
			t.lastUpdate = now
		}
	}

	if now.Sub(t.lastSave) >= SaveInterval {
		s := t.state(now)
		save = &s
		t.lastSave = now
	}
}

// checkWorkLimit returns the reminder due at now, if any. t.mu is held.
func (t *Tracker) checkWorkLimit(now time.Time, isIdle bool) func() {
	elapsed := now.Sub(t.activeStart)
	if elapsed < t.cfg.Work {
		return nil
	}
	if !t.notified {
		t.notified = true
		t.sessions++
		return t.remind(now, elapsed)
	}
	renotifyDue := now.Sub(t.lastNotify) >= renotifyInterval
	if renotifyDue && !isIdle {
		return t.remind(now, elapsed)
	}
	return nil
}

// remind records a reminder sent at now. t.mu is held.
func (t *Tracker) remind(now time.Time, elapsed time.Duration) func() {
	t.lastNotify = now
	fn := t.OnWorkLimit
	if fn == nil {
		return nil
	}
	return func() { fn(elapsed) }
}

// after runs the reminder and the save a call left due, then OnChange,
// without t.mu.
func (t *Tracker) after(notify func(), save *State) {
	if save != nil && t.Persist != nil {
		t.Persist(*save)
	}
	if notify != nil {
		notify()
	}
	if t.OnChange != nil {
		t.OnChange()
	}
}

// update applies change under t.mu, then saves the state at now.
func (t *Tracker) update(now time.Time, change func()) {
	t.mu.Lock()
	change()
	s := t.state(now)
	t.mu.Unlock()
	t.after(nil, &s)
}

// rollover clears the today counters on a new day. t.mu is held.
func (t *Tracker) rollover(now time.Time) {
	today := now.Format(dateLayout)
	if today == t.todayDate {
		return
	}
	t.todayActive = 0
	t.sessions = 0
	t.todayDate = today
}

// resetToActive starts an active period at start. t.mu is held.
func (t *Tracker) resetToActive(start time.Time) {
	t.status = Active
	t.activeStart = start
	t.idleStart = time.Time{}
	t.notified = false
	t.consecIdle = 0
	t.consecActive = 0
	t.lastUpdate = start
}

// toIdle starts an idle period at now, counting the time since the last
// update as active if it was. t.mu is held.
func (t *Tracker) toIdle(now time.Time) {
	if t.status == Active {
		t.todayActive += now.Sub(t.lastUpdate)
	}
	t.status = Idle
	t.idleStart = now
	t.lastUpdate = now
	t.consecIdle = 0
	t.consecActive = 0
	t.paused = true
}

// Rest starts an idle period at now on the user's request, ignoring
// input for a few seconds so the click that asked does not end it.
func (t *Tracker) Rest(now time.Time) {
	t.update(now, func() {
		t.toIdle(now)
		t.notified = false
		t.manualIdleUntil = now.Add(manualRestGrace)
	})
}

// Restart starts a new active period at now.
func (t *Tracker) Restart(now time.Time) {
	t.update(now, func() {
		t.resetToActive(now)
		t.paused = false
	})
}

// TestReminder starts an active period that reaches the work time ten
// seconds after now, to try the reminder.
func (t *Tracker) TestReminder(now time.Time) {
	t.update(now, func() {
		t.resetToActive(now)
		t.activeStart = now.Add(-t.cfg.Work + 10*time.Second)
		t.paused = false
	})
}

// SetConfig changes the thresholds, as timer.md is reloaded. The current
// period carries on: past a lowered threshold, the next tick ends it or
// reminds.
func (t *Tracker) SetConfig(cfg Config) {
	t.mu.Lock()
	t.cfg = cfg
	t.mu.Unlock()
	t.after(nil, nil)
}

// ResetToday clears the today counters.
func (t *Tracker) ResetToday(now time.Time) {
	t.update(now, func() {
		t.todayActive = 0
		t.sessions = 0
	})
}

// Sleep ends the active period at now, as the system sleeps or the
// screen locks, and stops Tick until Wake.
func (t *Tracker) Sleep(now time.Time) {
	t.mu.Lock()
	t.sleeping = true
	t.stop(now)
}

// stop ends the active period at now and saves the state, as Tick stops.
// t.mu is held, and released.
func (t *Tracker) stop(now time.Time) {
	if t.status != Active {
		t.consecActive = 0
		t.mu.Unlock()
		t.after(nil, nil)
		return
	}
	t.toIdle(now)
	s := t.state(now)
	t.mu.Unlock()
	t.after(nil, &s)
}

// Wake resumes Tick at now, not counting the time asleep.
func (t *Tracker) Wake(now time.Time) {
	t.mu.Lock()
	t.sleeping = false
	t.resume(now)
}

// resume restarts Tick at now, not counting the time it was stopped.
// t.mu is held, and released.
func (t *Tracker) resume(now time.Time) {
	t.lastUpdate = now
	t.mu.Unlock()
	t.after(nil, nil)
}

// SetEnabled turns the timer on or off at now, as timer.md is reloaded.
// Off ends the active period as Sleep does, stops Tick and the
// reminders, and empties Items; on resumes Tick as Wake does. Setting
// the value in force does nothing.
func (t *Tracker) SetEnabled(enabled bool, now time.Time) {
	t.mu.Lock()
	unchanged := t.disabled == !enabled
	if unchanged {
		t.mu.Unlock()
		return
	}
	t.disabled = !enabled
	if !enabled {
		t.stop(now)
		return
	}
	t.resume(now)
}

// Enabled reports whether the timer is on.
func (t *Tracker) Enabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.disabled
}

// OnBreak reports whether the user is on a break at the Mac: the timer
// is on and idle. A sleeping tracker, between Sleep and Wake, reads Idle
// too, as Sleep ends the active period, but the user is then away from
// the Mac, not on a break at it.
func (t *Tracker) OnBreak() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.disabled && !t.sleeping && t.status == Idle
}

// Snapshot returns what the bar shows at now.
func (t *Tracker) Snapshot(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Status:      t.status,
		Paused:      t.paused,
		TodayActive: t.todayActive,
		Sessions:    t.sessions,
		Work:        t.cfg.Work,
	}
	if t.status == Idle {
		if !t.idleStart.IsZero() {
			s.Elapsed = now.Sub(t.idleStart)
		}
		s.Resuming = t.consecActive > 0
		return s
	}
	s.Elapsed = now.Sub(t.activeStart)
	// A threshold lowered by a reload may be passed until the next tick.
	s.IdleIn = max(t.cfg.Idle-t.consecIdle, 0)
	s.Overtime = s.Elapsed >= t.cfg.Work
	return s
}

// Title is the bar's text for s: "▶12m -2m" while active ("⏸︎" when the
// last tick found the user idle), with warning signs around it past
// s.Work, and "💤 5m" while idle ("❓" while the user is back but not
// yet for ActiveThreshold).
func Title(s Snapshot) string {
	if s.Status == Idle {
		icon := "💤"
		if s.Resuming {
			icon = "❓"
		}
		return icon + " " + FormatDuration(s.Elapsed)
	}
	// The countdown moves in steps of ten seconds, so the bar does not
	// redraw on every tick while it runs.
	idleIn := int(math.Ceil(s.IdleIn.Seconds()/10)) * 10
	countdown := fmt.Sprintf(" -%ds", idleIn)
	if idleIn > 60 {
		countdown = fmt.Sprintf(" -%dm", idleIn/60)
	}
	var prefix, suffix string
	if s.Overtime {
		count := min(maxWarnings, 1+int((s.Elapsed-s.Work)/overtimeStep))
		half := count / 2
		prefix = strings.Repeat("⚠️", count-half)
		suffix = strings.Repeat("⚠️", half)
	}
	icon := "▶"
	if s.Paused {
		icon = "⏸︎"
	}
	return prefix + icon + FormatDuration(s.Elapsed) + countdown + suffix
}

// FormatDuration formats d as "42s", "12m" or "1h05m", rounded down.
func FormatDuration(d time.Duration) string {
	secs := int(d / time.Second)
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm", secs/60)
	}
	return fmt.Sprintf("%dh%02dm", secs/3600, secs%3600/60)
}
