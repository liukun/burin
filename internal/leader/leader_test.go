package leader

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

const (
	keyK      = 40
	keyL      = 37
	keyX      = 7
	keyRCmd   = 54
	keyRAlt   = 61
	keyF18    = 79
	keyTab    = 48
	keyD      = 2
	keyKPMin  = 78
	shiftFlag = 1 << 17
	cmdFlag   = 1 << 20
	altFlag   = 1 << 19
	rCmdDown  = cmdFlag | deviceRCmd
	rAltDown  = altFlag | deviceRAlt
	noFlags   = 0
	waitLimit = 2 * time.Second
)

type recorder struct {
	mu    sync.Mutex
	shows int
	hides int
	taps  []keystroke
	runs  chan string
}

func newLeader(t *testing.T, files map[string]string) (*Leader, *recorder) {
	t.Helper()
	rec := &recorder{runs: make(chan string, 16)}
	var configs []Config
	for key, data := range files {
		configs = append(configs, parseConfig(key, triggers[key], data))
	}
	l := &Leader{
		OnShow: func() { rec.mu.Lock(); rec.shows++; rec.mu.Unlock() },
		OnHide: func() { rec.mu.Lock(); rec.hides++; rec.mu.Unlock() },
		OnTap: func(keycode int64, flags uint64) {
			rec.mu.Lock()
			rec.taps = append(rec.taps, keystroke{keycode, flags})
			rec.mu.Unlock()
		},
		OnRun: func(c Config, e Entry) error {
			rec.runs <- c.Key + " " + e.Key
			return nil
		},
		DoneDelay: 20 * time.Millisecond,
	}
	l.SetConfigs(configs)
	return l, rec
}

func (r *recorder) counts() (shows, hides int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.shows, r.hides
}

func (r *recorder) tapped() []keystroke {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]keystroke(nil), r.taps...)
}

func (r *recorder) wantTaps(t *testing.T, want ...keystroke) {
	t.Helper()
	if got := r.tapped(); !slices.Equal(got, want) {
		t.Fatalf("taps %v, want %v", got, want)
	}
}

// waitShows waits until OnShow was called n times.
func (r *recorder) waitShows(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		if s, _ := r.counts(); s >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("OnShow not called %d times", n)
}

func (r *recorder) wantRun(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-r.runs:
		if got != want {
			t.Fatalf("ran %q, want %q", got, want)
		}
	case <-time.After(waitLimit):
		t.Fatalf("%q did not run", want)
	}
}

func (r *recorder) wantNoRun(t *testing.T) {
	t.Helper()
	select {
	case got := <-r.runs:
		t.Fatalf("unexpected run %q", got)
	case <-time.After(30 * time.Millisecond):
	}
}

const cmdR = "## k Kitty\na\n## l Activity Monitor\nb\n"

func press(l *Leader, flags uint64, keycode int64) (down, up bool) {
	down = l.Handle(EventKeyDown, keycode, flags, false)
	up = l.Handle(EventKeyUp, keycode, flags, false)
	return down, up
}

func TestHoldShowsAndReleaseHides(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	if l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false) {
		t.Fatal("flagsChanged press consumed")
	}
	if s, h := rec.counts(); s != 1 || h != 0 {
		t.Fatalf("after press shows=%d hides=%d", s, h)
	}
	snap := l.Snapshot()
	if !snap.Active || snap.Key != "cmd_r" || len(snap.Entries) != 2 || snap.Entries[1].Title != "Activity Monitor" {
		t.Fatalf("snapshot %+v", snap)
	}
	if l.Handle(EventFlagsChanged, keyRCmd, noFlags, false) {
		t.Fatal("flagsChanged release consumed")
	}
	if s, h := rec.counts(); s != 1 || h != 1 {
		t.Fatalf("after release shows=%d hides=%d", s, h)
	}
	if l.Snapshot().Active {
		t.Fatal("still active")
	}
}

func TestLeftCmdDoesNotTrigger(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, 55, cmdFlag|deviceLCmd, false)
	if down, _ := press(l, cmdFlag|deviceLCmd, keyK); down {
		t.Fatal("cmd_l+k consumed")
	}
	if s, _ := rec.counts(); s != 0 {
		t.Fatal("shown")
	}
}

func TestActiveConsumesKeysAndRunsMultipleTimes(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)

	down, up := press(l, rCmdDown, keyK)
	if !down || !up {
		t.Fatalf("mapped key consumed down=%v up=%v", down, up)
	}
	rec.wantRun(t, "cmd_r k")
	press(l, rCmdDown, keyK)
	rec.wantRun(t, "cmd_r k")
	press(l, rCmdDown, keyL)
	rec.wantRun(t, "cmd_r l")

	// Unmapped keys and Escape are consumed without running anything.
	for _, kc := range []int64{keyX, 53} {
		if down, up := press(l, rCmdDown, kc); !down || !up {
			t.Fatalf("key %d consumed down=%v up=%v", kc, down, up)
		}
	}
	rec.wantNoRun(t)
}

func TestAutoRepeatDoesNotRunAgain(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	l.Handle(EventKeyDown, keyK, rCmdDown, false)
	rec.wantRun(t, "cmd_r k")
	if !l.Handle(EventKeyDown, keyK, rCmdDown, false) {
		t.Fatal("repeat not consumed")
	}
	rec.wantNoRun(t)
}

func TestKeyUpAfterReleaseIsConsumed(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	l.Handle(EventKeyDown, keyK, rCmdDown, false)
	rec.wantRun(t, "cmd_r k")
	l.Handle(EventFlagsChanged, keyRCmd, noFlags, false)
	if !l.Handle(EventKeyUp, keyK, noFlags, false) {
		t.Fatal("keyUp of a consumed keyDown passed")
	}
	if l.Handle(EventKeyDown, keyK, noFlags, false) || l.Handle(EventKeyUp, keyK, noFlags, false) {
		t.Fatal("key consumed after release")
	}
}

func TestLostKeyUpDoesNotSwallowLaterKeyUp(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	l.Handle(EventKeyDown, keyK, rCmdDown, false)
	rec.wantRun(t, "cmd_r k")
	// The release comes, but the keyUp of k is lost.
	l.Handle(EventFlagsChanged, keyRCmd, noFlags, false)
	if l.Handle(EventKeyDown, keyK, noFlags, false) {
		t.Fatal("keyDown consumed while inactive")
	}
	if l.Handle(EventKeyUp, keyK, noFlags, false) {
		t.Fatal("keyUp of an unconsumed keyDown consumed")
	}
}

func TestSecondLeaderIgnored(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR, "opt_r": "## k Other\nx\n"})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	l.Handle(EventFlagsChanged, keyRAlt, rCmdDown|rAltDown, false)
	if s, _ := rec.counts(); s != 1 {
		t.Fatalf("shows=%d", s)
	}
	press(l, rCmdDown|rAltDown, keyK)
	rec.wantRun(t, "cmd_r k")
	// Releasing the ignored leader does not release the active one.
	l.Handle(EventFlagsChanged, keyRAlt, rCmdDown, false)
	if !l.Snapshot().Active {
		t.Fatal("released by the other trigger")
	}
}

func TestNoLeadersDoesNotIntercept(t *testing.T) {
	l, rec := newLeader(t, nil)
	if l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false) {
		t.Fatal("flagsChanged consumed")
	}
	if down, up := press(l, rCmdDown, keyK); down || up {
		t.Fatal("key consumed")
	}
	if s, _ := rec.counts(); s != 0 {
		t.Fatal("shown")
	}
}

func TestOrdinaryKeyTrigger(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"f18": cmdR})
	if !l.Handle(EventKeyDown, keyF18, noFlags, false) {
		t.Fatal("trigger keyDown passed")
	}
	if !l.Handle(EventKeyDown, keyF18, noFlags, true) {
		t.Fatal("trigger repeat passed")
	}
	press(l, noFlags, keyK)
	rec.wantRun(t, "f18 k")
	if !l.Handle(EventKeyUp, keyF18, noFlags, false) {
		t.Fatal("trigger keyUp passed")
	}
	if s, h := rec.counts(); s != 1 || h != 1 {
		t.Fatalf("shows=%d hides=%d", s, h)
	}
	if down, _ := press(l, noFlags, keyK); down {
		t.Fatal("consumed after release")
	}
}

func TestMissedReleaseRecovers(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	// The release never came; a keyDown without the modifier proves it up.
	if l.Handle(EventKeyDown, keyK, noFlags, false) {
		t.Fatal("key consumed after missed release")
	}
	if _, h := rec.counts(); h != 1 {
		t.Fatal("not hidden")
	}
	rec.wantNoRun(t)
}

func TestParseErrorConsumesWithoutRunning(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": "## k A\nx\n## k B\ny\n"})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	if s := l.Snapshot(); s.Err == nil || len(s.Entries) != 0 {
		t.Fatalf("snapshot %+v", s)
	}
	if down, _ := press(l, rCmdDown, keyK); !down {
		t.Fatal("key passed")
	}
	rec.wantNoRun(t)
}

func waitStatus(t *testing.T, l *Leader, i int, want Status) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		s := l.Snapshot()
		if len(s.Entries) > i && s.Entries[i].Status == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("entry %d never became %v: %+v", i, want, l.Snapshot())
}

func TestEntryStatus(t *testing.T) {
	l, _ := newLeader(t, map[string]string{"cmd_r": cmdR})
	release := make(chan struct{})
	l.OnRun = func(c Config, e Entry) error {
		<-release
		if e.Key == "l" {
			return errors.New("boom")
		}
		return nil
	}
	changes := make(chan struct{}, 64)
	l.OnChange = func() { changes <- struct{}{} }

	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	press(l, rCmdDown, keyK)
	press(l, rCmdDown, keyL)
	waitStatus(t, l, 0, Running)
	waitStatus(t, l, 1, Running)
	close(release)
	waitStatus(t, l, 1, Failed)
	waitStatus(t, l, 0, Done)
	// Done goes back to idle after DoneDelay; failed stays.
	waitStatus(t, l, 0, Idle)
	time.Sleep(40 * time.Millisecond)
	if st := l.Snapshot().Entries[1].Status; st != Failed {
		t.Fatalf("failed became %v", st)
	}
	if len(changes) == 0 {
		t.Fatal("OnChange never called")
	}

	// Release resets everything; the next hold starts idle.
	l.Handle(EventFlagsChanged, keyRCmd, noFlags, false)
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	for i, e := range l.Snapshot().Entries {
		if e.Status != Idle {
			t.Fatalf("entry %d is %v after a new hold", i, e.Status)
		}
	}
}

func TestRunOfEarlierHoldLeavesNewHoldAlone(t *testing.T) {
	l, _ := newLeader(t, map[string]string{"cmd_r": cmdR})
	release := make(chan struct{})
	done := make(chan struct{})
	l.OnRun = func(Config, Entry) error {
		<-release
		defer close(done)
		return errors.New("late")
	}
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	press(l, rCmdDown, keyK)
	l.Handle(EventFlagsChanged, keyRCmd, noFlags, false)
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	close(release)
	<-done
	time.Sleep(10 * time.Millisecond)
	if st := l.Snapshot().Entries[0].Status; st != Idle {
		t.Fatalf("status %v", st)
	}
}

func TestSetConfigsKeepsActiveHold(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false)
	l.SetConfigs(nil)
	press(l, rCmdDown, keyK)
	rec.wantRun(t, "cmd_r k")
	l.Handle(EventFlagsChanged, keyRCmd, noFlags, false)
	if l.Handle(EventFlagsChanged, keyRCmd, rCmdDown, false); l.Snapshot().Active {
		t.Fatal("activated after configs were removed")
	}
}

// Hold-mode leaders: a long delay where the timer must not fire during
// the test, a short one where it must.
const (
	tabSlow = ":hold: 1s\n## k Kitty\na\n"
	tabFast = ":hold: 30ms\n## k Kitty\na\n"
)

func TestHoldModeTapTypesTrigger(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabSlow})
	down, up := press(l, noFlags, keyTab)
	if !down || !up {
		t.Fatalf("tap consumed down=%v up=%v", down, up)
	}
	rec.wantTaps(t, keystroke{keyTab, 0})
	if s, h := rec.counts(); s != 0 || h != 0 {
		t.Fatalf("shows=%d hides=%d", s, h)
	}
	if l.Snapshot().Active {
		t.Fatal("still active")
	}
	rec.wantNoRun(t)
}

func TestHoldModeLateTimerDoesNotShow(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabFast})
	press(l, noFlags, keyTab)
	time.Sleep(80 * time.Millisecond)
	if s, _ := rec.counts(); s != 0 {
		t.Fatalf("shows=%d after a tap", s)
	}
	rec.wantTaps(t, keystroke{keyTab, 0})
}

func TestHoldModeOtherKeyCancels(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabSlow})
	l.Handle(EventKeyDown, keyTab, noFlags, false)
	if !l.Handle(EventKeyDown, keyTab, noFlags, true) {
		t.Fatal("trigger repeat while pending passed")
	}
	down, up := press(l, noFlags, keyD)
	if !down || !up {
		t.Fatalf("interrupting key consumed down=%v up=%v", down, up)
	}
	rec.wantTaps(t, keystroke{keyTab, 0}, keystroke{keyD, 0})

	// The trigger is still down: suppressed until its keyUp.
	if !l.Handle(EventKeyDown, keyTab, noFlags, true) {
		t.Fatal("suppressed trigger repeat passed")
	}
	if !l.Handle(EventKeyUp, keyTab, noFlags, false) {
		t.Fatal("suppressed trigger keyUp passed")
	}
	if down, up := press(l, noFlags, keyD); down || up {
		t.Fatalf("plain key consumed down=%v up=%v", down, up)
	}
	rec.wantTaps(t, keystroke{keyTab, 0}, keystroke{keyD, 0})
	if s, h := rec.counts(); s != 0 || h != 0 {
		t.Fatalf("shows=%d hides=%d", s, h)
	}

	// A new press starts a new hold.
	if !l.Handle(EventKeyDown, keyTab, noFlags, false) {
		t.Fatal("trigger keyDown passed after release")
	}
	l.Handle(EventKeyUp, keyTab, noFlags, false)
	rec.wantTaps(t, keystroke{keyTab, 0}, keystroke{keyD, 0}, keystroke{keyTab, 0})
}

func TestHoldModeShiftedKeyKeepsFlags(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabSlow})
	l.Handle(EventKeyDown, keyTab, noFlags, false)
	if l.Handle(EventFlagsChanged, 56, shiftFlag|deviceLShift, false) {
		t.Fatal("flagsChanged consumed while pending")
	}
	press(l, shiftFlag|deviceLShift, keyD)
	rec.wantTaps(t, keystroke{keyTab, 0}, keystroke{keyD, shiftFlag | deviceLShift})
}

func TestHoldModeHeldShowsAndRuns(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabFast})
	if !l.Handle(EventKeyDown, keyTab, noFlags, false) {
		t.Fatal("trigger keyDown passed")
	}
	if s, _ := rec.counts(); s != 0 {
		t.Fatal("shown before the delay")
	}
	rec.waitShows(t, 1)
	l.Handle(EventKeyDown, keyTab, noFlags, true)
	if down, up := press(l, noFlags, keyK); !down || !up {
		t.Fatalf("entry key consumed down=%v up=%v", down, up)
	}
	rec.wantRun(t, "tab k")
	if down, _ := press(l, noFlags, keyX); !down {
		t.Fatal("non-entry key passed while shown")
	}
	if !l.Handle(EventKeyUp, keyTab, noFlags, false) {
		t.Fatal("trigger keyUp passed")
	}
	if s, h := rec.counts(); s != 1 || h != 1 {
		t.Fatalf("shows=%d hides=%d", s, h)
	}
	rec.wantTaps(t)
}

func TestHoldModeShortcutPasses(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabSlow})
	for _, flags := range []uint64{cmdFlag | deviceLCmd, shiftFlag | deviceLShift} {
		if down, up := press(l, flags, keyTab); down || up {
			t.Fatalf("flags %#x: consumed down=%v up=%v", flags, down, up)
		}
		if l.Snapshot().Active {
			t.Fatalf("flags %#x: activated", flags)
		}
	}
	rec.wantTaps(t)
}

func TestHoldModeSuppressionExpires(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabSlow})
	l.SuppressExpire = 20 * time.Millisecond
	l.Handle(EventKeyDown, keyTab, noFlags, false)
	press(l, noFlags, keyD)
	// The trigger's keyUp is lost.
	time.Sleep(40 * time.Millisecond)
	if !l.Handle(EventKeyDown, keyTab, noFlags, false) {
		t.Fatal("trigger keyDown passed after expiry")
	}
	if !l.Snapshot().Active {
		t.Fatal("no new hold after expiry")
	}
	l.Handle(EventKeyUp, keyTab, noFlags, false)
	rec.wantTaps(t, keystroke{keyTab, 0}, keystroke{keyD, 0}, keystroke{keyTab, 0})
}

func TestHoldModeOtherTriggerActivates(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabSlow, "kp_minus": "## d Date\n{date}\n"})
	l.Handle(EventKeyDown, keyTab, noFlags, false)
	if !l.Handle(EventKeyDown, keyKPMin, noFlags, false) {
		t.Fatal("other trigger keyDown passed")
	}
	rec.wantTaps(t, keystroke{keyTab, 0})
	if s, _ := rec.counts(); s != 1 {
		t.Fatalf("shows=%d", s)
	}
	if snap := l.Snapshot(); snap.Key != "kp_minus" {
		t.Fatalf("active %q", snap.Key)
	}
	// The suppressed trigger neither runs nor releases the other leader.
	l.Handle(EventKeyDown, keyTab, noFlags, true)
	if !l.Handle(EventKeyUp, keyTab, noFlags, false) || !l.Snapshot().Active {
		t.Fatal("suppressed trigger keyUp released the other leader")
	}
	press(l, noFlags, keyD)
	rec.wantRun(t, "kp_minus d")
	if !l.Handle(EventKeyUp, keyKPMin, noFlags, false) {
		t.Fatal("other trigger keyUp passed")
	}
	if _, h := rec.counts(); h != 1 {
		t.Fatalf("hides=%d", h)
	}
	rec.wantTaps(t, keystroke{keyTab, 0})
}

func TestHoldModeRepeatAfterShortcutPasses(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabFast})
	shift := uint64(shiftFlag | deviceLShift)
	if l.Handle(EventKeyDown, keyTab, shift, false) {
		t.Fatal("shift+tab consumed")
	}
	// Shift goes up while Tab is still held and repeating.
	l.Handle(EventFlagsChanged, 56, noFlags, false)
	if l.Handle(EventKeyDown, keyTab, noFlags, true) {
		t.Fatal("tab repeat after shift+tab consumed")
	}
	time.Sleep(60 * time.Millisecond)
	if l.Handle(EventKeyUp, keyTab, noFlags, false) {
		t.Fatal("tab keyUp after shift+tab consumed")
	}
	if s, _ := rec.counts(); s != 0 || l.Snapshot().Active {
		t.Fatalf("shows=%d active=%v", s, l.Snapshot().Active)
	}
	rec.wantTaps(t)
}

func TestHoldModeWithErrorTapsAndShows(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": ":hold: soon\n## k Kitty\na\n"})
	l.mu.Lock()
	c := l.configs[0]
	l.mu.Unlock()
	if c.Err == nil || c.HoldDelay != DefaultHoldDelay {
		t.Fatalf("config %+v", c)
	}
	press(l, noFlags, keyTab)
	rec.wantTaps(t, keystroke{keyTab, 0})

	l.Handle(EventKeyDown, keyTab, noFlags, false)
	rec.waitShows(t, 1)
	if s := l.Snapshot(); s.Err == nil {
		t.Fatalf("snapshot %+v", s)
	}
	if down, _ := press(l, noFlags, keyK); !down {
		t.Fatal("entry key passed")
	}
	rec.wantNoRun(t)
	l.Handle(EventKeyUp, keyTab, noFlags, false)
	if _, h := rec.counts(); h != 1 {
		t.Fatalf("hides=%d", h)
	}
	rec.wantTaps(t, keystroke{keyTab, 0})
}

func TestHoldModeLateTimerAfterCancelDoesNotShow(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"tab": tabFast})
	l.Handle(EventKeyDown, keyTab, noFlags, false)
	press(l, noFlags, keyD)
	time.Sleep(80 * time.Millisecond)
	if s, _ := rec.counts(); s != 0 || l.Snapshot().Active {
		t.Fatalf("shows=%d after a cancel", s)
	}
	rec.wantTaps(t, keystroke{keyTab, 0}, keystroke{keyD, 0})
}
