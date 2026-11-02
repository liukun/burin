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
	tr := NewTracker(base)
	now := base
	run(tr, &now, time.Minute, 0, false)
	run(tr, &now, IdleThreshold-PollInterval, ActivityWindow, false)
	if s := tr.Snapshot(now); s.Status != Active {
		t.Fatalf("status = %v before the threshold, want active", s.Status)
	}
	run(tr, &now, PollInterval, ActivityWindow, false)
	s := tr.Snapshot(now)
	if s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
	// The idle period started when the user stopped, not when it was told.
	if s.Elapsed != IdleThreshold {
		t.Errorf("idle elapsed = %v, want %v", s.Elapsed, IdleThreshold)
	}
	if s.TodayActive != time.Minute {
		t.Errorf("today active = %v, want 1m", s.TodayActive)
	}
}

func TestFrontIdleAppCountsAsIdle(t *testing.T) {
	tr := NewTracker(base)
	now := base
	run(tr, &now, IdleThreshold, 0, true)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
}

func TestResumingWhileBackUnderThreshold(t *testing.T) {
	tr := NewTracker(base)
	now := base
	run(tr, &now, IdleThreshold, ActivityWindow, false)
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
	tr := NewTracker(base)
	now := base
	run(tr, &now, IdleThreshold, ActivityWindow, false)
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
	tr := NewTracker(base)
	now := base
	run(tr, &now, IdleThreshold, ActivityWindow, false)
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
	tr := NewTracker(start)
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
	tr := NewTracker(base)
	var reminders []time.Duration
	tr.OnWorkLimit = func(elapsed time.Duration) { reminders = append(reminders, elapsed) }
	now := base
	run(tr, &now, WorkLimit-PollInterval, 0, false)
	if len(reminders) != 0 {
		t.Fatalf("reminded at %v before the limit", reminders)
	}
	run(tr, &now, PollInterval, 0, false)
	if len(reminders) != 1 || reminders[0] != WorkLimit {
		t.Fatalf("reminders = %v, want [%v]", reminders, WorkLimit)
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
	tr := NewTracker(base)
	tr.OnWorkLimit = func(time.Duration) { tr.Rest(base) }
	now := base
	run(tr, &now, WorkLimit, 0, false)
	if s := tr.Snapshot(now); s.Status != Idle {
		t.Fatalf("status = %v, want idle", s.Status)
	}
}

func TestRestIgnoresInputBriefly(t *testing.T) {
	tr := NewTracker(base)
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
	tr := NewTracker(base)
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
	tr := NewTracker(base)
	now := base
	run(tr, &now, WorkLimit, 0, false)
	tr.ResetToday(now)
	s := tr.Snapshot(now)
	if s.TodayActive != 0 || s.Sessions != 0 {
		t.Fatalf("snapshot = %+v, want the today counters cleared", s)
	}
	if s.Elapsed != WorkLimit {
		t.Errorf("elapsed = %v, want the active period kept", s.Elapsed)
	}
}

func TestSleepAndWake(t *testing.T) {
	tr := NewTracker(base)
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
	tr.Wake(now)
	run(tr, &now, ActiveThreshold, 0, false)
	if s := tr.Snapshot(now); s.Status != Active || s.Elapsed != ActiveThreshold {
		t.Fatalf("snapshot = %+v, want active for %v after waking", s, ActiveThreshold)
	}
}

func TestPersistOnInterval(t *testing.T) {
	tr := NewTracker(base)
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
	tr := NewTracker(base)
	now := base
	run(tr, &now, 10*time.Minute, 0, false)
	st := tr.State(now)

	later := now.Add(30 * time.Second)
	restored := NewTracker(later)
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
	tr := NewTracker(base)
	now := base
	run(tr, &now, 5*time.Minute, 0, false)
	tr.Rest(now)
	st := tr.State(now)

	tomorrow := now.Add(24 * time.Hour)
	restored := NewTracker(tomorrow)
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
	tr := NewTracker(base)
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
	tr := NewTracker(base)
	now := base
	run(tr, &now, IdleThreshold, ActivityWindow, false)
	run(tr, &now, ActiveThreshold, 0, false)
	run(tr, &now, PollInterval, 0, false)
	s := tr.Snapshot(now)
	if s.Status != Active || s.TodayActive != ActiveThreshold+PollInterval {
		t.Fatalf("snapshot = %+v, want active with %v today", s, ActiveThreshold+PollInterval)
	}
	// Nor does an idle tick right after add it to the countdown.
	run(tr, &now, PollInterval, ActivityWindow, false)
	if s := tr.Snapshot(now); s.IdleIn != IdleThreshold-PollInterval {
		t.Errorf("idle in = %v, want %v", s.IdleIn, IdleThreshold-PollInterval)
	}
}

func TestClockSetBackCountsNothing(t *testing.T) {
	tr := NewTracker(base)
	now := base
	run(tr, &now, time.Minute, 0, false)
	tr.Tick(now.Add(-time.Hour), 0, false)
	if s := tr.Snapshot(now); s.TodayActive != time.Minute {
		t.Fatalf("today active = %v, want 1m", s.TodayActive)
	}
}

func TestSleepWhileIdle(t *testing.T) {
	tr := NewTracker(base)
	var saves int
	tr.Persist = func(State) { saves++ }
	now := base
	run(tr, &now, IdleThreshold, ActivityWindow, false)
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
	if s := tr.Snapshot(now); s.Elapsed != IdleThreshold+ActiveThreshold {
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
	tr := NewTracker(now)
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
	tr := NewTracker(now)
	tr.Restore(st, now)
	// Nothing to go on: the tracker keeps its fresh active period.
	if s := tr.Snapshot(now); s.Status != Active || s.Elapsed != 0 {
		t.Fatalf("snapshot = %+v, want a fresh active period", s)
	}
}
