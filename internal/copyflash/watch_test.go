package copyflash

import "testing"

func TestCounterChangeFires(t *testing.T) {
	c := counter{last: 5}
	if !c.observe(6) {
		t.Fatal("change did not fire")
	}
	if c.observe(6) {
		t.Fatal("same count fired again")
	}
	if !c.observe(9) {
		t.Fatal("second change did not fire")
	}
}

func TestCounterIgnoredCountNotAnnounced(t *testing.T) {
	c := counter{last: 5}
	c.ignore(6)
	if c.observe(6) {
		t.Fatal("ignored count fired")
	}
	if !c.observe(7) {
		t.Fatal("change after the ignored one did not fire")
	}
}

func TestCounterCopyInSameWindowAnnounced(t *testing.T) {
	c := counter{last: 5}
	c.ignore(6)
	// burin's write is 6 and a user copy 7, both before one poll.
	if !c.observe(7) {
		t.Fatal("user copy after an ignored write did not fire")
	}
}

func TestCounterTwoIgnoredCounts(t *testing.T) {
	c := counter{last: 5}
	c.ignore(6)
	c.ignore(7)
	if c.observe(6) || c.observe(7) {
		t.Fatal("ignored count fired")
	}
	if !c.observe(8) {
		t.Fatal("change after the ignored ones did not fire")
	}
}

func TestCounterObservePrunesStale(t *testing.T) {
	c := counter{last: 5}
	c.ignore(6)
	c.ignore(7)
	c.ignore(9)
	if !c.observe(8) {
		t.Fatal("unignored count did not fire")
	}
	if len(c.ignored) != 1 {
		t.Fatalf("ignored = %v, want only 9 left", c.ignored)
	}
	if _, ok := c.ignored[9]; !ok {
		t.Fatalf("ignored = %v, want 9 kept", c.ignored)
	}
}

func TestOwnFailingWriteRecordsNothing(t *testing.T) {
	w := &Watcher{read: func() int { return 1 }, onChange: func() {}}
	w.SetEnabled(true)
	defer w.Stop()
	w.Own(func() (int, bool) { return 2, false })
	w.mu.Lock()
	n := len(w.counter.ignored)
	w.mu.Unlock()
	if n != 0 {
		t.Fatalf("ignored = %d after a failed write, want 0", n)
	}
}

func TestOwnWhileDisabledRunsWriteOnly(t *testing.T) {
	w := &Watcher{read: func() int { return 1 }, onChange: func() {}}
	ran := false
	w.Own(func() (int, bool) {
		ran = true
		return 2, true
	})
	if !ran {
		t.Fatal("write did not run while disabled")
	}
	w.SetEnabled(true)
	w.Stop()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.counter.ignored) != 0 {
		t.Fatalf("ignored = %v after a write while disabled, want none", w.counter.ignored)
	}
	if !w.counter.observe(2) {
		t.Fatal("count written while disabled not announced once enabled")
	}
}

func TestSetEnabledIsIdempotent(t *testing.T) {
	w := &Watcher{read: func() int { return 1 }, onChange: func() {}}
	w.SetEnabled(true)
	w.Own(func() (int, bool) { return 2, true })
	w.SetEnabled(true)
	w.mu.Lock()
	_, kept := w.counter.ignored[2]
	w.mu.Unlock()
	if !kept {
		t.Error("ignored count lost after a second enable")
	}
	w.Stop()
	w.Stop()
	w.mu.Lock()
	stopped := w.stop == nil
	w.mu.Unlock()
	if !stopped {
		t.Error("still polling after Stop")
	}
}
