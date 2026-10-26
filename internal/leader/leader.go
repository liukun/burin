package leader

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// DefaultDoneDelay is how long an entry shows done before it is idle again.
const DefaultDoneDelay = time.Second

// Status is the state of an entry during one hold of its leader.
type Status int

// Statuses.
const (
	Idle Status = iota
	Running
	Done
	Failed
)

// EntryState is an entry as the HUD shows it.
type EntryState struct {
	Key    string
	Title  string
	Status Status
}

// Snapshot is the active leader as the HUD shows it.
type Snapshot struct {
	// Active is false while no leader is held; the rest is then empty.
	Active  bool
	Key     string
	Err     error
	Entries []EntryState
}

// Leader is the state machine of the leader keys. It sees every keyboard
// event through Handle and decides which ones to consume; it does not
// depend on the event tap, so it can be driven by tests.
//
// While a trigger is held, every keyDown and keyUp is consumed, and the
// keyDown of an entry's key runs it on its own goroutine, once per press.
// flagsChanged events are never consumed, so the system's modifier state
// stays right. Only one leader is active at a time.
//
// The callbacks are called without the Leader's lock held. OnShow and
// OnHide run on the caller of Handle, the event tap thread, and must
// return quickly.
type Leader struct {
	OnShow func()
	OnHide func()
	// OnRun runs an entry on its own goroutine. An error marks it failed.
	OnRun func(c Config, e Entry) error
	// OnChange is called after the snapshot changes while a leader is
	// active, from any goroutine.
	OnChange  func()
	DoneDelay time.Duration

	mu      sync.Mutex
	configs []Config
	active  *Config
	// gen counts activations and releases, so that runs and timers of an
	// earlier hold leave the current one alone.
	gen    uint64
	status []entryStatus
	// down holds the keys whose keyDown was consumed and whose keyUp has
	// not come yet. A second keyDown is auto-repeat; the keyUp is
	// consumed even after the leader is released.
	down map[int64]bool
}

type entryStatus struct {
	status  Status
	running int
	// seq counts status changes, so that a done timer only resets the
	// done it was started for.
	seq uint64
}

// effects are what Handle does once it has released the lock.
type effects struct {
	consume bool
	show    bool
	hide    bool
	run     *runRequest
}

type runRequest struct {
	gen    uint64
	index  int
	config Config
	entry  Entry
}

// SetConfigs replaces the leaders from the next activation on. A leader
// held now keeps the configuration it was activated with.
func (l *Leader) SetConfigs(configs []Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configs = configs
}

// Handle processes one keyboard event and reports whether it must be
// consumed.
func (l *Leader) Handle(eventType uint32, keycode int64, flags uint64) bool {
	eff := l.lockedHandle(eventType, keycode, flags)
	l.apply(eff)
	return eff.consume
}

// lockedHandle runs handleLocked under the lock, released even on panic.
func (l *Leader) lockedHandle(eventType uint32, keycode int64, flags uint64) effects {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.handleLocked(eventType, keycode, flags)
}

func (l *Leader) handleLocked(eventType uint32, keycode int64, flags uint64) effects {
	// The flagsChanged of the trigger's release, or any later event once
	// that release was missed (as while the tap was disabled): the flags
	// say the modifier is up.
	released := l.modifierUpLocked(flags)
	if released {
		l.deactivateLocked()
	}
	var eff effects
	switch eventType {
	case EventFlagsChanged:
		eff = l.flagsChangedLocked(keycode, flags)
	case EventKeyDown:
		eff = l.keyDownLocked(keycode)
	case EventKeyUp:
		eff = l.keyUpLocked(keycode)
	}
	eff.hide = eff.hide || released
	return eff
}

// modifierUpLocked reports whether a modifier leader is active while the
// event's flags say its modifier is up.
func (l *Leader) modifierUpLocked(flags uint64) bool {
	if l.active == nil || !l.active.trigger.isModifier() {
		return false
	}
	return flags&l.active.trigger.mask == 0
}

func (l *Leader) flagsChangedLocked(keycode int64, flags uint64) effects {
	if l.active != nil {
		return effects{}
	}
	for i := range l.configs {
		t := l.configs[i].trigger
		isPress := t.isModifier() && t.keycode == keycode && flags&t.mask != 0
		if isPress {
			l.activateLocked(l.configs[i])
			return effects{show: true}
		}
	}
	return effects{}
}

func (l *Leader) keyDownLocked(keycode int64) effects {
	if l.active == nil {
		// A fresh press outside a hold: a keyUp lost after an earlier
		// hold must not make this press's keyUp be consumed.
		delete(l.down, keycode)
		return l.activateByKeyLocked(keycode)
	}
	consume := effects{consume: true}
	isTriggerRepeat := keycode == l.active.trigger.keycode
	if isTriggerRepeat || l.down[keycode] {
		return consume
	}
	l.down[keycode] = true
	if l.active.Err != nil {
		return consume
	}
	for i, e := range l.active.Entries {
		if e.keycode == keycode {
			consume.run = l.startLocked(i)
			return consume
		}
	}
	return consume
}

// activateByKeyLocked activates the leader of an ordinary trigger key.
func (l *Leader) activateByKeyLocked(keycode int64) effects {
	for i := range l.configs {
		t := l.configs[i].trigger
		if !t.isModifier() && t.keycode == keycode {
			l.activateLocked(l.configs[i])
			return effects{consume: true, show: true}
		}
	}
	return effects{}
}

func (l *Leader) keyUpLocked(keycode int64) effects {
	isTriggerRelease := l.active != nil && !l.active.trigger.isModifier() && keycode == l.active.trigger.keycode
	if isTriggerRelease {
		l.deactivateLocked()
		return effects{consume: true, hide: true}
	}
	if l.down[keycode] {
		delete(l.down, keycode)
		return effects{consume: true}
	}
	return effects{consume: l.active != nil}
}

func (l *Leader) activateLocked(c Config) {
	l.active = &c
	l.gen++
	l.status = make([]entryStatus, len(c.Entries))
	l.down = map[int64]bool{}
}

func (l *Leader) deactivateLocked() {
	l.active = nil
	l.gen++
	l.status = nil
}

func (l *Leader) startLocked(i int) *runRequest {
	st := &l.status[i]
	st.running++
	st.status = Running
	st.seq++
	return &runRequest{gen: l.gen, index: i, config: *l.active, entry: l.active.Entries[i]}
}

func (l *Leader) apply(eff effects) {
	if eff.hide && l.OnHide != nil {
		l.OnHide()
	}
	if eff.show && l.OnShow != nil {
		l.OnShow()
	}
	if eff.run == nil {
		return
	}
	l.changed()
	go l.run(*eff.run)
}

func (l *Leader) run(r runRequest) {
	err := l.call(r)
	if err != nil {
		log.Printf("leader %s %s: %v", r.config.Key, r.entry.Key, err)
	}
	l.finish(r, err)
}

// call runs OnRun, turning a panic into an error.
func (l *Leader) call(r runRequest) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	if l.OnRun == nil {
		return nil
	}
	return l.OnRun(r.config, r.entry)
}

func (l *Leader) finish(r runRequest, err error) {
	l.mu.Lock()
	if r.gen != l.gen {
		l.mu.Unlock()
		return
	}
	st := &l.status[r.index]
	st.running--
	if st.running > 0 {
		l.mu.Unlock()
		return
	}
	st.seq++
	if err != nil {
		st.status = Failed
	} else {
		st.status = Done
		seq := st.seq
		time.AfterFunc(l.doneDelay(), func() { l.resetDone(r.gen, r.index, seq) })
	}
	l.mu.Unlock()
	l.changed()
}

func (l *Leader) resetDone(gen uint64, i int, seq uint64) {
	l.mu.Lock()
	isSameDone := gen == l.gen && l.status[i].seq == seq
	if !isSameDone {
		l.mu.Unlock()
		return
	}
	l.status[i].status = Idle
	l.mu.Unlock()
	l.changed()
}

func (l *Leader) doneDelay() time.Duration {
	if l.DoneDelay > 0 {
		return l.DoneDelay
	}
	return DefaultDoneDelay
}

func (l *Leader) changed() {
	if l.OnChange != nil {
		l.OnChange()
	}
}

// Snapshot returns the active leader and the status of its entries.
func (l *Leader) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		return Snapshot{}
	}
	s := Snapshot{Active: true, Key: l.active.Key, Err: l.active.Err}
	s.Entries = make([]EntryState, len(l.active.Entries))
	for i, e := range l.active.Entries {
		s.Entries[i] = EntryState{Key: e.Key, Title: e.Title, Status: l.status[i].status}
	}
	return s
}
