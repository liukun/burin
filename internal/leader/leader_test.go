package leader

import (
	"errors"
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
	runs  chan string
}

func newLeader(t *testing.T, files map[string]string) (*Leader, *recorder) {
	t.Helper()
	rec := &recorder{runs: make(chan string, 16)}
	var configs []Config
	for key, data := range files {
		entries, err := Parse(data)
		configs = append(configs, Config{Key: key, Entries: entries, Err: err, trigger: triggers[key]})
	}
	l := &Leader{
		OnShow: func() { rec.mu.Lock(); rec.shows++; rec.mu.Unlock() },
		OnHide: func() { rec.mu.Lock(); rec.hides++; rec.mu.Unlock() },
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
	down = l.Handle(EventKeyDown, keycode, flags)
	up = l.Handle(EventKeyUp, keycode, flags)
	return down, up
}

func TestHoldShowsAndReleaseHides(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	if l.Handle(EventFlagsChanged, keyRCmd, rCmdDown) {
		t.Fatal("flagsChanged press consumed")
	}
	if s, h := rec.counts(); s != 1 || h != 0 {
		t.Fatalf("after press shows=%d hides=%d", s, h)
	}
	snap := l.Snapshot()
	if !snap.Active || snap.Key != "cmd_r" || len(snap.Entries) != 2 || snap.Entries[1].Title != "Activity Monitor" {
		t.Fatalf("snapshot %+v", snap)
	}
	if l.Handle(EventFlagsChanged, keyRCmd, noFlags) {
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
	l.Handle(EventFlagsChanged, 55, cmdFlag|deviceLCmd)
	if down, _ := press(l, cmdFlag|deviceLCmd, keyK); down {
		t.Fatal("cmd_l+k consumed")
	}
	if s, _ := rec.counts(); s != 0 {
		t.Fatal("shown")
	}
}

func TestActiveConsumesKeysAndRunsMultipleTimes(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)

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
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	l.Handle(EventKeyDown, keyK, rCmdDown)
	rec.wantRun(t, "cmd_r k")
	if !l.Handle(EventKeyDown, keyK, rCmdDown) {
		t.Fatal("repeat not consumed")
	}
	rec.wantNoRun(t)
}

func TestKeyUpAfterReleaseIsConsumed(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	l.Handle(EventKeyDown, keyK, rCmdDown)
	rec.wantRun(t, "cmd_r k")
	l.Handle(EventFlagsChanged, keyRCmd, noFlags)
	if !l.Handle(EventKeyUp, keyK, noFlags) {
		t.Fatal("keyUp of a consumed keyDown passed")
	}
	if l.Handle(EventKeyDown, keyK, noFlags) || l.Handle(EventKeyUp, keyK, noFlags) {
		t.Fatal("key consumed after release")
	}
}

func TestLostKeyUpDoesNotSwallowLaterKeyUp(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	l.Handle(EventKeyDown, keyK, rCmdDown)
	rec.wantRun(t, "cmd_r k")
	// The release comes, but the keyUp of k is lost.
	l.Handle(EventFlagsChanged, keyRCmd, noFlags)
	if l.Handle(EventKeyDown, keyK, noFlags) {
		t.Fatal("keyDown consumed while inactive")
	}
	if l.Handle(EventKeyUp, keyK, noFlags) {
		t.Fatal("keyUp of an unconsumed keyDown consumed")
	}
}

func TestSecondLeaderIgnored(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR, "opt_r": "## k Other\nx\n"})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	l.Handle(EventFlagsChanged, keyRAlt, rCmdDown|rAltDown)
	if s, _ := rec.counts(); s != 1 {
		t.Fatalf("shows=%d", s)
	}
	press(l, rCmdDown|rAltDown, keyK)
	rec.wantRun(t, "cmd_r k")
	// Releasing the ignored leader does not release the active one.
	l.Handle(EventFlagsChanged, keyRAlt, rCmdDown)
	if !l.Snapshot().Active {
		t.Fatal("released by the other trigger")
	}
}

func TestNoLeadersDoesNotIntercept(t *testing.T) {
	l, rec := newLeader(t, nil)
	if l.Handle(EventFlagsChanged, keyRCmd, rCmdDown) {
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
	if !l.Handle(EventKeyDown, keyF18, noFlags) {
		t.Fatal("trigger keyDown passed")
	}
	if !l.Handle(EventKeyDown, keyF18, noFlags) {
		t.Fatal("trigger repeat passed")
	}
	press(l, noFlags, keyK)
	rec.wantRun(t, "f18 k")
	if !l.Handle(EventKeyUp, keyF18, noFlags) {
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
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	// The release never came; a keyDown without the modifier proves it up.
	if l.Handle(EventKeyDown, keyK, noFlags) {
		t.Fatal("key consumed after missed release")
	}
	if _, h := rec.counts(); h != 1 {
		t.Fatal("not hidden")
	}
	rec.wantNoRun(t)
}

func TestParseErrorConsumesWithoutRunning(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": "## k A\nx\n## k B\ny\n"})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
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

	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
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
	l.Handle(EventFlagsChanged, keyRCmd, noFlags)
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
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
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	press(l, rCmdDown, keyK)
	l.Handle(EventFlagsChanged, keyRCmd, noFlags)
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	close(release)
	<-done
	time.Sleep(10 * time.Millisecond)
	if st := l.Snapshot().Entries[0].Status; st != Idle {
		t.Fatalf("status %v", st)
	}
}

func TestSetConfigsKeepsActiveHold(t *testing.T) {
	l, rec := newLeader(t, map[string]string{"cmd_r": cmdR})
	l.Handle(EventFlagsChanged, keyRCmd, rCmdDown)
	l.SetConfigs(nil)
	press(l, rCmdDown, keyK)
	rec.wantRun(t, "cmd_r k")
	l.Handle(EventFlagsChanged, keyRCmd, noFlags)
	if l.Handle(EventFlagsChanged, keyRCmd, rCmdDown); l.Snapshot().Active {
		t.Fatal("activated after configs were removed")
	}
}
