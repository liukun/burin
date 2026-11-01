package leader

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// DefaultDoneDelay is how long an entry shows done before it is idle again.
const DefaultDoneDelay = time.Second

// DefaultSuppressExpire is how long a hold-mode trigger stays suppressed
// without any event of it, in case its keyUp never comes, as when Secure
// Input hides the keyboard from the tap.
const DefaultSuppressExpire = 5 * time.Second

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
	// RowBreak starts a new row in rows layout.
	RowBreak bool
}

// Snapshot is the active leader as the HUD shows it.
type Snapshot struct {
	// Active is false while no leader is held; the rest is then empty.
	Active  bool
	Key     string
	Err     error
	Layout  Layout
	Entries []EntryState
	// Plain, when set, is shown instead of the rest: lines of plain text
	// without keys or a header. The Leader never sets it; the mouse
	// gestures share the HUD through it.
	Plain []string
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
// A trigger in hold mode (Config.HoldDelay) is first only pending: the
// leader shows once the trigger has been held for the delay. Released
// earlier, the trigger is typed with OnTap instead. Another key pressed
// while pending cancels the hold: the trigger and that key are typed, in
// that order, and the trigger is suppressed until its keyUp. A trigger
// pressed with Shift, Control, Option or Command is left alone.
//
// The callbacks are called without the Leader's lock held. OnHide and
// OnTap run on the caller of Handle, the event tap thread; OnShow runs
// there too, or on a timer goroutine in hold mode. They must return
// quickly.
type Leader struct {
	OnShow func()
	OnHide func()
	// OnTap types a key: keyDown and keyUp with the given flags. The
	// events must not come back to Handle.
	OnTap func(keycode int64, flags uint64)
	// OnRun runs an entry on its own goroutine. An error marks it failed.
	OnRun func(c Config, e Entry) error
	// OnChange is called after the snapshot changes while a leader is
	// active, from any goroutine.
	OnChange  func()
	DoneDelay time.Duration
	// SuppressExpire defaults to DefaultSuppressExpire.
	SuppressExpire time.Duration

	// callbackMu serializes OnShow, OnHide and OnTap, so that a show of
	// the hold timer cannot overtake the hide that follows it.
	callbackMu sync.Mutex

	mu      sync.Mutex
	configs []Config
	active  *Config
	// pending is set while the active leader waits for its hold delay,
	// which holdTimer counts down.
	pending   bool
	holdTimer *time.Timer
	// gen counts activations and releases, so that runs and timers of an
	// earlier hold leave the current one alone.
	gen    uint64
	status []entryStatus
	// down holds the keys whose keyDown was consumed and whose keyUp has
	// not come yet. A second keyDown is auto-repeat; the keyUp is
	// consumed even after the leader is released.
	down map[int64]bool
	// swallowUp holds the keys typed with OnTap when they cancelled a
	// hold, whose real keyUp is still to come and is consumed.
	swallowUp map[int64]bool
	// suppressed maps hold-mode triggers that are still down after their
	// hold was cancelled to the time their suppression expires.
	suppressed map[int64]time.Time
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
	// taps are typed in order, after hide and before show.
	taps []keystroke
}

type keystroke struct {
	keycode int64
	flags   uint64
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
// consumed. repeat is set for the auto-repeat keyDowns of a held key.
func (l *Leader) Handle(eventType uint32, keycode int64, flags uint64, repeat bool) bool {
	eff := l.lockedHandle(eventType, keycode, flags, repeat)
	l.apply(eff)
	return eff.consume
}

// lockedHandle runs handleLocked under the lock, released even on panic.
func (l *Leader) lockedHandle(eventType uint32, keycode int64, flags uint64, repeat bool) effects {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.handleLocked(eventType, keycode, flags, repeat)
}

func (l *Leader) handleLocked(eventType uint32, keycode int64, flags uint64, repeat bool) effects {
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
		eff = l.keyDownLocked(keycode, flags, repeat)
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

func (l *Leader) keyDownLocked(keycode int64, flags uint64, repeat bool) effects {
	consume := effects{consume: true}
	if l.suppressedLocked(keycode) {
		l.suppressLocked(keycode)
		return consume
	}
	if l.active == nil {
		// A fresh press outside a hold: a keyUp lost after an earlier
		// hold must not make this press's keyUp be consumed. An
		// auto-repeat of a key that cancelled a hold passes too, and so
		// must its keyUp.
		delete(l.down, keycode)
		delete(l.swallowUp, keycode)
		return l.activateByKeyLocked(keycode, flags, repeat)
	}
	if l.pending {
		return l.interruptLocked(keycode, flags, repeat)
	}
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

// activateByKeyLocked activates the leader of an ordinary trigger key. A
// hold-mode leader only becomes pending, and a timer shows it later. It
// is left alone when pressed as a shortcut, and on an auto-repeat, which
// is of a press that went through as a shortcut once its modifier is up.
func (l *Leader) activateByKeyLocked(keycode int64, flags uint64, repeat bool) effects {
	for i := range l.configs {
		c := l.configs[i]
		isTrigger := !c.trigger.isModifier() && c.trigger.keycode == keycode
		if !isTrigger {
			continue
		}
		if c.HoldDelay <= 0 {
			l.activateLocked(c)
			return effects{consume: true, show: true}
		}
		isShortcut := flags&shortcutFlags != 0
		if isShortcut || repeat {
			return effects{}
		}
		l.activateLocked(c)
		l.pending = true
		gen := l.gen
		l.holdTimer = time.AfterFunc(c.HoldDelay, func() { l.showHeld(gen) })
		return effects{consume: true}
	}
	return effects{}
}

// interruptLocked handles a keyDown while the active leader is pending.
// Any key but the trigger's auto-repeat cancels the hold: the trigger is
// typed, then the key, unless it is itself a trigger and activates its
// own leader. The key's real keyDown is consumed, as the real event would
// overtake the typed trigger.
func (l *Leader) interruptLocked(keycode int64, flags uint64, repeat bool) effects {
	trig := l.active.trigger.keycode
	if keycode == trig {
		return effects{consume: true}
	}
	l.deactivateLocked()
	// The trigger is still down: its auto-repeat and keyUp follow.
	l.suppressLocked(trig)
	eff := l.activateByKeyLocked(keycode, flags, repeat)
	eff.taps = []keystroke{{keycode: trig}}
	activatedOther := eff.consume
	if activatedOther {
		return eff
	}
	eff.consume = true
	eff.taps = append(eff.taps, keystroke{keycode: keycode, flags: flags})
	if l.swallowUp == nil {
		l.swallowUp = map[int64]bool{}
	}
	l.swallowUp[keycode] = true
	return eff
}

// showHeld shows the leader of activation gen if it is still pending
// once its hold delay has passed. It holds callbackMu across the check
// and OnShow, so a release handled meanwhile hides after the show.
func (l *Leader) showHeld(gen uint64) {
	l.callbackMu.Lock()
	defer l.callbackMu.Unlock()
	l.mu.Lock()
	isSameHold := gen == l.gen && l.pending
	if isSameHold {
		l.pending = false
	}
	l.mu.Unlock()
	if isSameHold && l.OnShow != nil {
		l.OnShow()
	}
}

func (l *Leader) keyUpLocked(keycode int64) effects {
	consume := effects{consume: true}
	if l.swallowUp[keycode] {
		delete(l.swallowUp, keycode)
		return consume
	}
	if l.suppressedLocked(keycode) {
		delete(l.suppressed, keycode)
		return consume
	}
	isTriggerRelease := l.active != nil && !l.active.trigger.isModifier() && keycode == l.active.trigger.keycode
	if isTriggerRelease {
		// Released while pending, it was a tap: type it.
		wasTap := l.pending
		l.deactivateLocked()
		if wasTap {
			consume.taps = []keystroke{{keycode: keycode}}
			return consume
		}
		consume.hide = true
		return consume
	}
	if l.down[keycode] {
		delete(l.down, keycode)
		return consume
	}
	// The keyUp of a key pressed before a pending hold goes through, as
	// its keyDown did.
	isHeldLeader := l.active != nil && !l.pending
	return effects{consume: isHeldLeader}
}

// suppressedLocked reports whether keycode is a suppressed trigger,
// dropping its suppression once expired.
func (l *Leader) suppressedLocked(keycode int64) bool {
	expires, ok := l.suppressed[keycode]
	if !ok {
		return false
	}
	if time.Now().After(expires) {
		delete(l.suppressed, keycode)
		return false
	}
	return true
}

// suppressLocked suppresses keycode, or extends its suppression.
func (l *Leader) suppressLocked(keycode int64) {
	if l.suppressed == nil {
		l.suppressed = map[int64]time.Time{}
	}
	expire := l.SuppressExpire
	if expire <= 0 {
		expire = DefaultSuppressExpire
	}
	l.suppressed[keycode] = time.Now().Add(expire)
}

func (l *Leader) activateLocked(c Config) {
	l.active = &c
	l.gen++
	l.status = make([]entryStatus, len(c.Entries))
	l.down = map[int64]bool{}
}

func (l *Leader) deactivateLocked() {
	l.active = nil
	l.pending = false
	// A hold timer that already fired finds gen changed and does nothing.
	if l.holdTimer != nil {
		l.holdTimer.Stop()
		l.holdTimer = nil
	}
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
	l.callback(eff)
	if eff.run == nil {
		return
	}
	l.changed()
	go l.run(*eff.run)
}

// callback calls OnHide, OnTap and OnShow as eff asks, in that order.
func (l *Leader) callback(eff effects) {
	l.callbackMu.Lock()
	defer l.callbackMu.Unlock()
	if eff.hide && l.OnHide != nil {
		l.OnHide()
	}
	for _, k := range eff.taps {
		if l.OnTap != nil {
			l.OnTap(k.keycode, k.flags)
		}
	}
	if eff.show && l.OnShow != nil {
		l.OnShow()
	}
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

// Active reports whether a leader is held, without copying its state.
func (l *Leader) Active() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active != nil
}

// Snapshot returns the active leader and the status of its entries.
func (l *Leader) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		return Snapshot{}
	}
	s := Snapshot{Active: true, Key: l.active.Key, Err: l.active.Err, Layout: l.active.Layout}
	s.Entries = make([]EntryState, len(l.active.Entries))
	for i, e := range l.active.Entries {
		s.Entries[i] = EntryState{Key: e.Key, Title: e.Title, Status: l.status[i].status, RowBreak: e.RowBreak}
	}
	return s
}
