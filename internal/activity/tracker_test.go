package activity

import (
	"path/filepath"
	"testing"
	"time"
)

var base = time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)

// run ticks t every PollInterval from *now for d, with the given idle
// time and frontmost app, and leaves *now at the last tick.
func run(t *Tracker, now *time.Time, d time.Duration, systemIdle time.Duration, frontIdle bool) {
	for end := now.Add(d); now.Before(end); {
		*now = now.Add(PollInterval)
		t.Tick(*now, systemIdle, frontIdle)
	}
}

func TestActiveToIdleAfterThreshold(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, time.Minute, 0, false)
	run(tr, &now, DefaultIdle-PollInterval, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Status != Active {
		t.Fatalf("status = %v before the threshold, want active", s.Status)
	}
	run(tr, &now, PollInterval, ActivityWindow, false)
	s := tr.Snapshot(now)
	if s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
	// The idle period started when the user stopped, not when it was told.
	if s.Elapsed != DefaultIdle {
		t.Errorf("idle elapsed = %v, want %v", s.Elapsed, DefaultIdle)
	}
	if s.TodayActive != time.Minute {
		t.Errorf("today active = %v, want 1m", s.TodayActive)
	}
}

func TestFrontIdleAppCountsAsWorkWhileActive(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, 3*DefaultIdle, 0, true)
	s := tr.Snapshot(now)
	if s.Status != Active {
		t.Fatalf("status = %v, want active", s.Status)
	}
	if s.TodayActive != 3*DefaultIdle {
		t.Errorf("today active = %v, want %v", s.TodayActive, 3*DefaultIdle)
	}
}

func TestFrontIdleAppDoesNotEndBreak(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, DefaultIdle, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
	run(tr, &now, 5*ActiveThreshold, 0, true)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v with an idle app in front, want idle", s.Status)
	}
}

func TestResumingWhileBackUnderThreshold(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, DefaultIdle, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Resuming {
		t.Fatal("resuming right after going idle")
	}
	run(tr, &now, PollInterval, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle || !s.Resuming {
		t.Fatalf("status = %v resuming = %v after input, want idle and resuming", s.Status, s.Resuming)
	}
	run(tr, &now, PollInterval, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Resuming {
		t.Fatal("still resuming after the input stopped")
	}
	run(tr, &now, ActiveThreshold, 0, false)
	if s := tr.Snapshot(now); s.Status != Active || s.Resuming {
		t.Fatalf("status = %v resuming = %v past the threshold, want active", s.Status, s.Resuming)
	}
}

func TestIdleToActiveCountsBack(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, DefaultIdle, ActivityWindow, false)
	run(tr, &now, 5*time.Minute, ActivityWindow, false)
	run(tr, &now, ActiveThreshold-PollInterval, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v before the threshold, want idle", s.Status)
	}
	run(tr, &now, PollInterval, 0, false)
	s := tr.Snapshot(now)
	if s.Status != Active {
		t.Fatalf("status = %v, want active", s.Status)
	}
	if s.Elapsed != ActiveThreshold {
		t.Errorf("active elapsed = %v, want %v counted back", s.Elapsed, ActiveThreshold)
	}
	if s.TodayActive != ActiveThreshold {
		t.Errorf("today active = %v, want %v", s.TodayActive, ActiveThreshold)
	}
}

func TestBriefIdleDoesNotCountBack(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, DefaultIdle, ActivityWindow, false)
	run(tr, &now, ActiveThreshold-PollInterval, 0, false)
	// One idle tick in between starts the count over.
	run(tr, &now, PollInterval, ActivityWindow, false)
	run(tr, &now, ActiveThreshold-PollInterval, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
}

func TestDateRollover(t *testing.T) {
	start := time.Date(2026, 11, 2, 23, 58, 0, 0, time.Local)
	tr := NewTracker(start, DefaultConfig)
	now := start
	run(tr, &now, time.Minute, 0, false)
	if s := tr.Snapshot(now); s.TodayActive != time.Minute {
		t.Fatalf("today active = %v, want 1m", s.TodayActive)
	}
	run(tr, &now, 2*time.Minute, 0, false)
	s := tr.Snapshot(now)
	// The ticks from midnight on count to the new day, the one at
	// midnight with the whole poll before it.
	if s.TodayActive != time.Minute+PollInterval {
		t.Errorf("today active after midnight = %v, want %v", s.TodayActive, time.Minute+PollInterval)
	}
	if s.Status != Active || s.Elapsed != 3*time.Minute {
		t.Errorf("snapshot = %+v, want the active period to go on", s)
	}
}

func TestWorkLimitReminds(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	var reminders []time.Duration
	tr.OnWorkLimit = func(elapsed time.Duration) { reminders = append(reminders, elapsed) }
	now := base
	run(tr, &now, DefaultWork-PollInterval, 0, false)
	if len(reminders) != 0 {
		t.Fatalf("reminded at %v before the limit", reminders)
	}
	run(tr, &now, PollInterval, 0, false)
	if len(reminders) != 1 || reminders[0] != DefaultWork {
		t.Fatalf("reminders = %v, want [%v]", reminders, DefaultWork)
	}
	if s := tr.Snapshot(now); s.Sessions != 1 || !s.Overtime {
		t.Errorf("snapshot = %+v, want 1 session in overtime", s)
	}
	run(tr, &now, renotifyInterval-PollInterval, 0, false)
	if len(reminders) != 1 {
		t.Fatalf("reminded again within a minute: %v", reminders)
	}
	run(tr, &now, PollInterval, 0, false)
	if len(reminders) != 2 {
		t.Fatalf("reminders = %v, want a second after a minute", reminders)
	}
	// Not while the user is away from the keyboard.
	run(tr, &now, 90*time.Second, ActivityWindow, false)
	if len(reminders) != 2 {
		t.Errorf("reminded while idle: %v", reminders)
	}
	if s := tr.Snapshot(now); s.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", s.Sessions)
	}
}

func TestOnWorkLimitMayCallBack(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	tr.OnWorkLimit = func(time.Duration) { tr.Rest(base) }
	now := base
	run(tr, &now, DefaultWork, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
}

func TestRestIgnoresInputBriefly(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, time.Minute, 0, false)
	tr.Rest(now)
	// Busy for the grace period and then just short of the threshold.
	run(tr, &now, manualRestGrace-PollInterval, 0, false)
	run(tr, &now, ActiveThreshold-PollInterval, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle: input in the grace period counted", s.Status)
	}
	run(tr, &now, PollInterval, 0, false)
	if s := tr.Snapshot(now); s.Status != Active {
		t.Fatalf("status = %v, want active", s.Status)
	}
}

func TestRestartAndTestReminder(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	reminded := 0
	tr.OnWorkLimit = func(time.Duration) { reminded++ }
	now := base
	run(tr, &now, 10*time.Minute, 0, false)
	tr.Restart(now)
	if s := tr.Snapshot(now); s.Elapsed != 0 {
		t.Fatalf("elapsed after restart = %v, want 0", s.Elapsed)
	}
	tr.TestReminder(now)
	run(tr, &now, 10*time.Second-PollInterval, 0, false)
	if reminded != 0 {
		t.Fatal("reminded early")
	}
	run(tr, &now, PollInterval, 0, false)
	if reminded != 1 {
		t.Fatalf("reminded %d times, want 1", reminded)
	}
}

func TestResetToday(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, DefaultWork, 0, false)
	tr.ResetToday(now)
	s := tr.Snapshot(now)
	if s.TodayActive != 0 || s.Sessions != 0 {
		t.Fatalf("snapshot = %+v, want the today counters cleared", s)
	}
	if s.Elapsed != DefaultWork {
		t.Errorf("elapsed = %v, want the active period kept", s.Elapsed)
	}
}

func TestSleepAndWake(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, time.Minute, 0, false)
	tr.Sleep(now.Add(time.Second))
	if s := tr.Snapshot(now); s.Status != Idle || s.TodayActive != time.Minute+time.Second {
		t.Fatalf("snapshot = %+v, want idle with 1m1s today", s)
	}
	// Ticks while asleep do nothing.
	run(tr, &now, time.Hour, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v while asleep, want idle", s.Status)
	}
	if tr.OnBreak() {
		t.Error("OnBreak = true while asleep, want false")
	}
	tr.Wake(now)
	run(tr, &now, ActiveThreshold, 0, false)
	if s := tr.Snapshot(now); s.Status != Active || s.Elapsed != ActiveThreshold {
		t.Fatalf("snapshot = %+v, want active for %v after waking", s, ActiveThreshold)
	}
}

func TestPersistOnInterval(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	var saves []State
	tr.Persist = func(s State) { saves = append(saves, s) }
	now := base
	run(tr, &now, SaveInterval, 0, false)
	// The first tick saves, then one every SaveInterval.
	if len(saves) != 1 {
		t.Fatalf("saves = %d, want 1", len(saves))
	}
	run(tr, &now, PollInterval, 0, false)
	if len(saves) != 2 {
		t.Fatalf("saves = %d, want 2", len(saves))
	}
	tr.Rest(now)
	if len(saves) != 3 || saves[2].State != Idle {
		t.Fatalf("saves = %+v, want an idle save after Rest", saves)
	}
}

func TestRestoreActiveCountsGap(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, 10*time.Minute, 0, false)
	st := tr.State(now)

	later := now.Add(30 * time.Second)
	restored := NewTracker(later, DefaultConfig)
	restored.Restore(st, later)
	s := restored.Snapshot(later)
	if s.Status != Active || s.Elapsed != 10*time.Minute+30*time.Second {
		t.Fatalf("snapshot = %+v, want the active period to go on", s)
	}
	if s.TodayActive != 10*time.Minute+30*time.Second {
		t.Errorf("today active = %v, want the gap counted", s.TodayActive)
	}
}

func TestRestoreIdleAndOtherDay(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, 5*time.Minute, 0, false)
	tr.Rest(now)
	st := tr.State(now)

	tomorrow := now.Add(24 * time.Hour)
	restored := NewTracker(tomorrow, DefaultConfig)
	restored.Restore(st, tomorrow)
	s := restored.Snapshot(tomorrow)
	if s.Status != Idle || s.Elapsed != 24*time.Hour {
		t.Fatalf("snapshot = %+v, want the idle period to go on", s)
	}
	if s.TodayActive != 0 {
		t.Errorf("today active = %v, want yesterday's dropped", s.TodayActive)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sub", StateFileName))
	if _, ok, err := store.Load(); ok || err != nil {
		t.Fatalf("Load of a missing file = %v, %v; want none", ok, err)
	}
	tr := NewTracker(base, DefaultConfig)
	tr.Rest(base.Add(time.Minute))
	want := tr.State(base.Add(time.Minute))
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Load()
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if got.IdleStartTime == nil || *got.IdleStartTime != *want.IdleStartTime {
		t.Fatalf("idle start = %v, want %v", got.IdleStartTime, *want.IdleStartTime)
	}
	got.IdleStartTime, want.IdleStartTime = nil, nil
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestCountBackNotCountedTwice(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, DefaultIdle, ActivityWindow, false)
	run(tr, &now, ActiveThreshold, 0, false)
	run(tr, &now, PollInterval, 0, false)
	s := tr.Snapshot(now)
	if s.Status != Active || s.TodayActive != ActiveThreshold+PollInterval {
		t.Fatalf("snapshot = %+v, want active with %v today", s, ActiveThreshold+PollInterval)
	}
	// Nor does an idle tick right after add it to the countdown.
	run(tr, &now, PollInterval, ActivityWindow, false)
	if s := tr.Snapshot(now); s.IdleIn != DefaultIdle-PollInterval {
		t.Errorf("idle in = %v, want %v", s.IdleIn, DefaultIdle-PollInterval)
	}
}

func TestClockSetBackCountsNothing(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, time.Minute, 0, false)
	tr.Tick(now.Add(-time.Hour), 0, false)
	if s := tr.Snapshot(now); s.TodayActive != time.Minute {
		t.Fatalf("today active = %v, want 1m", s.TodayActive)
	}
}

func TestSleepWhileIdle(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	var saves int
	tr.Persist = func(State) { saves++ }
	now := base
	run(tr, &now, DefaultIdle, ActivityWindow, false)
	run(tr, &now, ActiveThreshold-PollInterval, 0, false)
	saves = 0
	tr.Sleep(now)
	if saves != 0 {
		t.Errorf("saved %d times, want none for an idle period", saves)
	}
	tr.Wake(now)
	// The activity before sleeping no longer counts towards the threshold.
	run(tr, &now, PollInterval, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
	if s := tr.Snapshot(now); s.Elapsed != DefaultIdle+ActiveThreshold {
		t.Errorf("idle elapsed = %v, want the idle period kept", s.Elapsed)
	}
}

func TestRestoreWithoutSavedAt(t *testing.T) {
	st := State{
		State:              Active,
		ActiveStartTime:    base.Unix(),
		TodayActiveSeconds: 60,
		TodayDate:          base.Format(dateLayout),
	}
	now := base.Add(5 * time.Minute)
	tr := NewTracker(now, DefaultConfig)
	tr.Restore(st, now)
	s := tr.Snapshot(now)
	if s.Status != Active || s.Elapsed != 5*time.Minute {
		t.Fatalf("snapshot = %+v, want active for 5m", s)
	}
	if s.TodayActive != time.Minute {
		t.Errorf("today active = %v, want 1m with no gap counted", s.TodayActive)
	}
}

func TestRestoreIdleWithoutStart(t *testing.T) {
	st := State{State: Idle, ActiveStartTime: base.Unix(), TodayDate: base.Format(dateLayout)}
	now := base.Add(time.Minute)
	tr := NewTracker(now, DefaultConfig)
	tr.Restore(st, now)
	// Nothing to go on: the tracker keeps its fresh active period.
	if s := tr.Snapshot(now); s.Status != Active || s.Elapsed != 0 {
		t.Fatalf("snapshot = %+v, want a fresh active period", s)
	}
}

func TestCustomConfig(t *testing.T) {
	cfg := Config{Idle: 30 * time.Second, Work: 5 * time.Minute}
	tr := NewTracker(base, cfg)
	var reminders []time.Duration
	tr.OnWorkLimit = func(elapsed time.Duration) { reminders = append(reminders, elapsed) }
	now := base
	run(tr, &now, cfg.Work, 0, false)
	if len(reminders) != 1 || reminders[0] != cfg.Work {
		t.Fatalf("reminders = %v, want [%v]", reminders, cfg.Work)
	}
	if s := tr.Snapshot(now); !s.Overtime || s.Work != cfg.Work || s.IdleIn != cfg.Idle {
		t.Fatalf("snapshot = %+v, want overtime past %v, idle in %v", s, cfg.Work, cfg.Idle)
	}
	run(tr, &now, cfg.Idle, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v after %v idle, want idle", s.Status, cfg.Idle)
	}
}

func TestSetConfig(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	changed := 0
	tr.OnChange = func() { changed++ }
	now := base
	run(tr, &now, time.Minute, ActivityWindow, false)
	tr.SetConfig(Config{Idle: 30 * time.Second, Work: 10 * time.Minute})
	if changed == 0 {
		t.Error("SetConfig did not call OnChange")
	}
	// A minute idle is past the lowered threshold: the countdown stops at
	// zero, and the next tick ends the period.
	if s := tr.Snapshot(now); s.IdleIn != 0 || s.Work != 10*time.Minute {
		t.Fatalf("snapshot = %+v, want idle in 0 with the new work time", s)
	}
	run(tr, &now, PollInterval, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Errorf("status = %v, want idle past the lowered threshold", s.Status)
	}
}

func TestSetConfigRaisedWorkRemindsAgain(t *testing.T) {
	tr := NewTracker(base, Config{Idle: DefaultIdle, Work: 10 * time.Minute})
	var reminders []time.Duration
	tr.OnWorkLimit = func(elapsed time.Duration) { reminders = append(reminders, elapsed) }
	now := base
	run(tr, &now, 10*time.Minute+30*time.Second, 0, false)
	if len(reminders) != 1 {
		t.Fatalf("reminders = %v, want one past the work time", reminders)
	}
	tr.SetConfig(Config{Idle: DefaultIdle, Work: 20 * time.Minute})
	run(tr, &now, 9*time.Minute+30*time.Second-PollInterval, 0, false)
	if len(reminders) != 1 {
		t.Fatalf("reminders = %v before the raised work time, want still one", reminders)
	}
	run(tr, &now, PollInterval, 0, false)
	if len(reminders) != 2 || reminders[1] != 20*time.Minute {
		t.Errorf("reminders = %v, want a second at 20m", reminders)
	}
	// The period is still one session.
	if s := tr.Snapshot(now); s.Sessions != 1 {
		t.Errorf("sessions = %d, want 1 for one period", s.Sessions)
	}
}

func TestSetEnabledFalseStopsCounting(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	var reminders int
	tr.OnWorkLimit = func(time.Duration) { reminders++ }
	var saved []State
	tr.Persist = func(s State) { saved = append(saved, s) }
	now := base
	run(tr, &now, time.Minute, 0, false)
	saved = nil
	tr.SetEnabled(false, now)
	if len(saved) != 1 {
		t.Fatalf("saved %d times on disable, want once", len(saved))
	}
	if tr.Enabled() {
		t.Error("Enabled = true after SetEnabled(false)")
	}
	if items := tr.Items(); items != nil {
		t.Errorf("Items = %+v while disabled, want nil", items)
	}
	// Well past the work time: nothing counts and nothing reminds.
	run(tr, &now, 2*DefaultWork, 0, false)
	if reminders != 0 {
		t.Errorf("reminded %d times while disabled, want none", reminders)
	}
	s := tr.Snapshot(now)
	if s.Status != Idle || s.TodayActive != time.Minute {
		t.Errorf("snapshot = %+v, want idle with 1m today", s)
	}
}

func TestSetEnabledTrueStartsFresh(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	var reminders int
	tr.OnWorkLimit = func(time.Duration) { reminders++ }
	now := base
	run(tr, &now, time.Minute, 0, false)
	tr.SetEnabled(false, now)
	now = now.Add(time.Hour)
	tr.SetEnabled(true, now)
	if !tr.Enabled() || len(tr.Items()) != 1 {
		t.Fatalf("enabled = %v, items %+v; want enabled with the title", tr.Enabled(), tr.Items())
	}
	resumed := now
	run(tr, &now, ActiveThreshold, 0, false)
	s := tr.Snapshot(now)
	if s.Status != Active {
		t.Fatalf("status = %v after working again, want active", s.Status)
	}
	// The new period began when the user came back, not before the hour
	// off, which counts nothing.
	if s.Elapsed != now.Sub(resumed) {
		t.Errorf("elapsed = %v, want %v", s.Elapsed, now.Sub(resumed))
	}
	if s.TodayActive != time.Minute+ActiveThreshold {
		t.Errorf("today active = %v, want %v", s.TodayActive, time.Minute+ActiveThreshold)
	}
	if reminders != 0 {
		t.Errorf("reminded %d times, want none", reminders)
	}
}

func TestSetEnabledSameValueIsNoOp(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	var changes, saves int
	tr.OnChange = func() { changes++ }
	tr.Persist = func(State) { saves++ }
	now := base
	tr.SetEnabled(true, now)
	if changes != 0 || saves != 0 {
		t.Errorf("SetEnabled(true) while enabled: %d changes, %d saves; want none", changes, saves)
	}
	if s := tr.Snapshot(now); s.Status != Active {
		t.Errorf("status = %v, want active", s.Status)
	}
	tr.SetEnabled(false, now)
	changes, saves = 0, 0
	tr.SetEnabled(false, now.Add(time.Minute))
	if changes != 0 || saves != 0 {
		t.Errorf("SetEnabled(false) while disabled: %d changes, %d saves; want none", changes, saves)
	}
}

func TestWakeWhileDisabledKeepsTickStopped(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	tr.SetEnabled(false, now)
	tr.Sleep(now)
	tr.Wake(now)
	run(tr, &now, 5*time.Minute, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle || s.TodayActive != 0 {
		t.Errorf("snapshot = %+v, want idle with nothing counted", s)
	}
}

func TestOnBreak(t *testing.T) {
	tr := NewTracker(base, DefaultConfig)
	now := base
	run(tr, &now, ActiveThreshold, 0, false)
	if tr.Snapshot(now).Status != Active || tr.OnBreak() {
		t.Fatal("OnBreak = true while active, want false")
	}
	tr.Rest(now)
	if !tr.OnBreak() {
		t.Fatal("OnBreak = false while idle, want true")
	}
	tr.SetEnabled(false, now)
	if tr.OnBreak() {
		t.Error("OnBreak = true with the timer off, want false")
	}
}
