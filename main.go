package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"burin/internal/eventtap"
	"burin/internal/expander"
	"burin/internal/gesture"
	"burin/internal/hud"
	"burin/internal/keys"
	"burin/internal/leader"
	"burin/internal/pasteboard"
	"burin/internal/placeholder"
	"burin/internal/quitter"
	"burin/internal/snippet"
	"burin/internal/windowrules"
	"burin/internal/workspace"
)

// trayIcon is drawn by scripts/tray-icon.swift.
//
//go:embed resources/tray.png
var trayIcon []byte

const (
	grantAccessibility = "Grant Accessibility (click again to relaunch)"
	maxNotifyBody      = 300
	maxCopiedPreview   = 40
)

// burin holds the running app's snippet, leader, mouse gesture, window
// rule and quitter state.
type burin struct {
	snippetsDir string
	leadersDir  string
	mouseDir    string
	// windowRulesFile and quitterFile sit directly in the config dir,
	// outside snippets/ and leaders/, so neither of their loaders reads
	// them.
	windowRulesFile string
	quitterFile     string
	eval            *placeholder.Evaluator
	ex              *expander.Expander
	leader          *leader.Leader
	// hud is set once the tray exists; the tap may fire before.
	hud           atomic.Pointer[hud.HUD]
	status        *mygo.MenuItem
	leaderStatus  *mygo.MenuItem
	rulesStatus   *mygo.MenuItem
	rules         windowrules.Engine
	quitterStatus *mygo.MenuItem
	quitter       quitter.Engine
	mouseStatus   *mygo.MenuItem
	// gestures is read by the tap on every mouse event and swapped on
	// reload.
	gestures atomic.Pointer[gesture.Set]
	// runGesture runs the content of a gesture rule, named name.
	runGesture func(name, content string) error

	// gestureMu guards the press being tracked: the recognizer and the
	// copy of the press, kept for a replay.
	gestureMu  sync.Mutex
	recognizer gesture.Recognizer
	downCopy   uintptr
	// io reads and posts events; spawn runs the end of a gesture off the
	// tap thread. Both are stood in for by tests.
	io    eventIO
	spawn func(fn func())

	// The HUD lists the gestures of the held button. Its contents are
	// worked out off the tap thread, as the frontmost app is asked on the
	// main thread: hudGen counts the presses and releases, so a show that
	// a later release or press made stale is dropped, and hudMu orders
	// that check and the show with the hides.
	hudGen atomic.Uint64
	hudMu  sync.Mutex
	// showGestureHUD shows lines in the HUD, hideGestureHUD hides them
	// and leaderHeld reports whether a leader holds the HUD. Tests stand
	// in for them.
	showGestureHUD func(lines []string)
	hideGestureHUD func()
	leaderHeld     func() bool
	// gestureLines is what the HUD shows of the gestures, nil while it
	// shows none.
	gestureLines atomic.Pointer[[]string]

	// tapMu guards the event tap, which is rebuilt when the mouse buttons
	// to listen to change. tap is nil until listen succeeds; mouseMask is
	// the mouse part of its mask.
	tapMu     sync.Mutex
	tap       stopper
	mouseMask uint64
	// startTap is startEventTap, or a stand-in in tests.
	startTap func(mouseMask uint64) (stopper, error)

	reloadMu sync.Mutex
}

// logFile is where the log is kept besides stderr, which a login item
// has nowhere to send: ~/Library/Logs/burin.log. A file past
// maxLogSize at startup is moved aside to burin.log.old first, so the
// two never hold more than twice that.
const (
	logFileName = "burin.log"
	maxLogSize  = 5 << 20
)

// openLog adds a log file under ~/Library/Logs to the log output. A
// failure is logged and leaves stderr alone.
func openLog() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Print(err)
		return
	}
	dir := filepath.Join(home, "Library", "Logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Print(err)
		return
	}
	path := filepath.Join(dir, logFileName)
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		if err := os.Rename(path, path+".old"); err != nil {
			log.Print(err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Print(err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(home, ".config", "burin")
}

// newEvaluator returns an evaluator whose commands run with PATH set to
// path(), read on every run.
func newEvaluator(scriptsDir string, path func() string) *placeholder.Evaluator {
	runner := &placeholder.Runner{
		Dir: scriptsDir,
		Env: func() []string { return placeholder.EnvWithPath(os.Environ(), path()) },
		// Only successful commands reach here; a failure carries its
		// stderr in the error, which placeholderFailed reports once.
		OnStderr: func(name, stderr string) {
			log.Printf("%s stderr: %s", name, stderr)
			notify(name, stderr)
		},
	}
	return &placeholder.Evaluator{
		Builtins: placeholder.Builtins(placeholder.Deps{
			Now:       time.Now,
			Clipboard: pasteboard.ReadText,
			Shell:     runner.Shell,
		}),
		Fallback: runner.Script,
	}
}

// expanderSnippets turns loaded snippets into expander snippets whose
// content expands placeholders when the keyword fires.
func expanderSnippets(tbl snippet.Table, ev *placeholder.Evaluator, onError func(keyword string, errs []error)) []expander.Snippet {
	out := make([]expander.Snippet, 0, len(tbl.Snippets))
	for _, s := range tbl.Snippets {
		out = append(out, expander.Snippet{
			Keyword: s.Keyword,
			Content: func() string {
				text, errs := ev.Expand(s.Content)
				if len(errs) > 0 {
					onError(s.Keyword, errs)
				}
				return text
			},
		})
	}
	return out
}

func statusText(tbl snippet.Table) string {
	return fmt.Sprintf("%d snippets, %d errors", len(tbl.Snippets), len(tbl.Errors))
}

// windowRulesStatusText counts the rules that loaded and the sections
// that did not.
func windowRulesStatusText(set windowrules.Set) string {
	return fmt.Sprintf("%d window rules, %d errors", set.Config.Len(), len(set.Errors))
}

// quitterStatusText counts the rules that loaded and the sections that
// did not.
func quitterStatusText(set quitter.Set) string {
	return fmt.Sprintf("%d quitter rules, %d errors", set.Config.Len(), len(set.Errors))
}

// leaderStatusText counts the files that name no trigger key and the
// leaders whose file failed to load as errors.
func leaderStatusText(set leader.Set) string {
	failed := len(set.Errors)
	for _, c := range set.Configs {
		if c.Err != nil {
			failed++
		}
	}
	return fmt.Sprintf("%d leaders, %d errors", len(set.Configs), failed)
}

// mouseStatusText counts the files that name no supported button and the
// buttons whose file failed to load as errors.
func mouseStatusText(set gesture.Set) string {
	return fmt.Sprintf("%d mouse buttons, %d errors", len(set.Configs), len(set.AllErrors()))
}

func placeholderFailed(name string, errs []error) {
	msg := errors.Join(errs...).Error()
	log.Printf("placeholder in %q: %s", name, msg)
	notify(name, msg)
}

// leaderAction returns the action of leader entries: their content is
// expanded with ev, and a non-empty result is copied and announced. Empty
// results, as of {...|null}, do nothing more. Failures go to onError and
// mark the entry failed.
func leaderAction(ev *placeholder.Evaluator, copyText func(string) bool, notify func(title, body string), onError func(name string, errs []error)) func(leader.Config, leader.Entry) error {
	run := expandAction(ev, copyText, notify, onError)
	return func(c leader.Config, e leader.Entry) error {
		return run(entryName(c, e), e.Content)
	}
}

// expandAction returns the action leader entries and mouse gestures
// share: content is expanded with ev, and a non-empty result is copied
// and announced under name. Empty results do nothing more. Failures go to
// onError.
func expandAction(ev *placeholder.Evaluator, copyText func(string) bool, notify func(title, body string), onError func(name string, errs []error)) func(name, content string) error {
	return func(name, content string) error {
		text, errs := ev.Expand(content)
		if len(errs) > 0 {
			onError(name, errs)
			return errors.Join(errs...)
		}
		if text == "" {
			return nil
		}
		if !copyText(text) {
			err := errors.New("pasteboard write failed")
			onError(name, []error{err})
			return err
		}
		notify(name, "Copied: "+preview(text, maxCopiedPreview))
		return nil
	}
}

// entryName names an entry in notifications: its title, or else its
// leader and key.
func entryName(c leader.Config, e leader.Entry) string {
	if e.Title != "" {
		return e.Title
	}
	return c.Key + " " + e.Key
}

// preview returns the first n runes of s, marked when cut.
func preview(s string, n int) string {
	runes := 0
	for i := range s {
		if runes == n {
			return s[:i] + "…"
		}
		runes++
	}
	return s
}

// chain offers each event to handlers in order until one consumes it.
func chain(handlers ...eventtap.Handler) eventtap.Handler {
	return func(eventType uint32, event uintptr) bool {
		for _, h := range handlers {
			if h(eventType, event) {
				return true
			}
		}
		return false
	}
}

// handleLeader offers key events to the leader. The keys the leader types
// itself (keys.Stroke) come back through the tap and skip it, so a typed
// trigger cannot hold it again; they go on to the expander like typed ones.
func (b *burin) handleLeader(eventType uint32, event uintptr) bool {
	if eventtap.UserData(event) == keys.SyntheticMark {
		return false
	}
	repeat := eventType == eventtap.EventKeyDown && eventtap.Autorepeat(event)
	return b.leader.Handle(eventType, eventtap.Keycode(event), eventtap.Flags(event), repeat)
}

// handleGesture tracks the presses of the mouse buttons that have
// gestures. A press is swallowed, since it is not known yet whether it is
// a click or a gesture, and a copy of it kept; its drags are swallowed
// too. On release, its location against the press's tells the gesture,
// and the rest is left to finishGesture on a goroutine, as the tap must
// return quickly. The mouse events that a replay posts come back marked
// and pass.
func (b *burin) handleGesture(eventType uint32, event uintptr) bool {
	kind, isMouse := gesture.KindOf(eventType)
	if !isMouse {
		return false
	}
	if b.io.userData(event) == keys.SyntheticMark {
		return false
	}
	button := gesture.Button(b.io.mouseButton(event))
	untracked := kind == gesture.Press && !b.gestures.Load().Has(button)
	if untracked {
		return false
	}
	var x, y float64
	if kind != gesture.Drag {
		x, y = b.io.location(event)
	}
	b.gestureMu.Lock()
	defer b.gestureMu.Unlock()
	if kind == gesture.Press {
		return b.press(button, event, x, y)
	}
	res := b.recognizer.Handle(kind, button, x, y)
	if !res.Ended {
		return res.Consume
	}
	b.hideHUD()
	down := b.downCopy
	b.downCopy = 0
	up := b.io.copyEvent(event)
	// Without a copy of the release, a replay would leave the button
	// down in the app: the click is lost, but the release goes through.
	if up == 0 {
		b.io.releaseEvent(down)
		return false
	}
	b.spawn(func() { b.finishGesture(button, res, down, up) })
	return true
}

// press starts tracking a press of button at x, y, keeps a copy of it to
// replay and lists its gestures in the HUD. A press that cannot be copied
// could not be replayed, so it is passed on and not tracked. b.gestureMu
// is held.
func (b *burin) press(button gesture.Button, event uintptr, x, y float64) bool {
	// A press of another button may still be tracked: the last one wins,
	// and the earlier one is never replayed.
	b.io.releaseEvent(b.downCopy)
	b.downCopy = b.io.copyEvent(event)
	if b.downCopy == 0 {
		b.recognizer.Reset()
		// The HUD of an earlier press would stay up with no release to
		// hide it.
		b.hideHUD()
		return false
	}
	b.recognizer.Handle(gesture.Press, button, x, y)
	b.showHUD(button)
	return true
}

// showHUD lists the gestures of button over the frontmost app in the HUD,
// on a goroutine: the tap thread must not wait for the main thread. The
// HUD is left to a held leader.
func (b *burin) showHUD(button gesture.Button) {
	gen := b.hudGen.Add(1)
	set := b.gestures.Load()
	b.spawn(func() {
		var lines []string
		if !b.leaderHeld() {
			lines = gestureLines(set.Available(button, b.io.frontApp()))
		}
		b.ifLatestHUD(gen, func() {
			// With nothing to show, the gestures of an earlier press still
			// go: its release's hide may have been dropped as stale.
			if len(lines) == 0 {
				b.hideGestureHUD()
				return
			}
			b.showGestureHUD(lines)
		})
	})
}

// hideHUD hides the gestures in the HUD, on a goroutine like showHUD, and
// drops a show still on its way. A press since makes the hide stale: its
// show, run first, must not be hidden.
func (b *burin) hideHUD() {
	gen := b.hudGen.Add(1)
	b.spawn(func() { b.ifLatestHUD(gen, b.hideGestureHUD) })
}

// ifLatestHUD runs fn under b.hudMu unless a press or release since gen
// made it stale.
func (b *burin) ifLatestHUD(gen uint64, fn func()) {
	b.hudMu.Lock()
	defer b.hudMu.Unlock()
	if b.hudGen.Load() != gen {
		return
	}
	fn()
}

// gestureArrows are the HUD's marks of the directions.
var gestureArrows = map[gesture.Direction]string{
	gesture.Left:  "←",
	gesture.Right: "→",
	gesture.Up:    "↑",
	gesture.Down:  "↓",
}

// gestureLines are the HUD lines of rules: an arrow and the title, or
// the direction without one.
func gestureLines(rules []gesture.Rule) []string {
	lines := make([]string, 0, len(rules))
	for _, r := range rules {
		title := r.Title
		if title == "" {
			title = string(r.Direction)
		}
		lines = append(lines, gestureArrows[r.Direction]+" "+title)
	}
	return lines
}

// showGestureLines is showGestureHUD of the running app.
func (b *burin) showGestureLines(lines []string) {
	b.gestureLines.Store(&lines)
	if h := b.hud.Load(); h != nil {
		h.Show()
	}
}

// hideGestureLines is hideGestureHUD of the running app. The HUD stays up
// when it showed no gestures, or when a leader took it over since.
func (b *burin) hideGestureLines() {
	shown := b.gestureLines.Swap(nil) != nil
	h := b.hud.Load()
	keep := !shown || h == nil || b.leaderHeld()
	if keep {
		return
	}
	h.Hide()
}

// hudSnapshot is what the HUD shows: the held leader, or else the
// gestures of the held mouse button.
func (b *burin) hudSnapshot() leader.Snapshot {
	if s := b.leader.Snapshot(); s.Active {
		return s
	}
	lines := b.gestureLines.Load()
	if lines == nil {
		return leader.Snapshot{}
	}
	return leader.Snapshot{Active: true, Plain: *lines}
}

// finishGesture runs the rule for a recognized gesture, or else replays
// the swallowed press and release, so a click still opens the context
// menu, on release. down and up are copies it posts or releases.
func (b *burin) finishGesture(button gesture.Button, res gesture.Result, down, up uintptr) {
	if res.Gesture && b.runGestureRule(button, res.Direction) {
		b.io.releaseEvent(down)
		b.io.releaseEvent(up)
		return
	}
	b.io.postMarked(keys.SyntheticMark, down, up)
}

// runGestureRule runs the rule for a gesture of button in dir over the
// frontmost app, and reports whether there was one.
func (b *burin) runGestureRule(button gesture.Button, dir gesture.Direction) bool {
	rule, ok := b.gestures.Load().Match(button, dir, b.io.frontApp())
	if !ok {
		return false
	}
	// Failures are already announced by the action.
	_ = b.runGesture(rule.Name(button), rule.Content)
	return true
}

// eventIO is what the gesture handling needs of the event tap and the
// workspace, so that tests can stand in for them.
type eventIO struct {
	userData     func(event uintptr) int64
	mouseButton  func(event uintptr) int64
	location     func(event uintptr) (x, y float64)
	copyEvent    func(event uintptr) uintptr
	postMarked   func(mark int64, events ...uintptr)
	releaseEvent func(event uintptr)
	frontApp     func() workspace.App
}

// systemEventIO is the eventIO of the running app.
func systemEventIO() eventIO {
	return eventIO{
		userData:     eventtap.UserData,
		mouseButton:  eventtap.MouseButton,
		location:     eventtap.Location,
		copyEvent:    eventtap.CopyEvent,
		postMarked:   eventtap.PostMarked,
		releaseEvent: eventtap.ReleaseEvent,
		frontApp:     workspace.FrontmostApp,
	}
}

// goSpawn runs fn on a goroutine of its own.
func goSpawn(fn func()) { go fn() }

// handleExpander feeds keyDown events to the expander.
func (b *burin) handleExpander(eventType uint32, event uintptr) bool {
	if eventType != eventtap.EventKeyDown {
		return false
	}
	return b.ex.HandleKey(expander.Key{
		Keycode: eventtap.Keycode(event),
		Flags:   eventtap.Flags(event),
		Chars:   eventtap.UnicodeString(event),
	})
}

// keyMask is the part of the tap's mask that is always on.
const keyMask = 1<<eventtap.EventKeyDown | 1<<eventtap.EventKeyUp | 1<<eventtap.EventFlagsChanged

// stopper is a running event tap; Stop reports whether it stopped.
type stopper interface {
	Stop() bool
}

// startEventTap starts an event tap for the key events and the mouse
// events of mouseMask, which offers every event to the gestures first,
// then to the leader and then to the expander.
func (b *burin) startEventTap(mouseMask uint64) (stopper, error) {
	tap := &eventtap.Tap{}
	if err := tap.Start(keyMask|mouseMask, chain(b.handleGesture, b.handleLeader, b.handleExpander)); err != nil {
		return nil, err
	}
	return tap, nil
}

// listen starts the only event tap, with the mouse events of the gestures
// loaded so far.
func (b *burin) listen() error {
	b.tapMu.Lock()
	defer b.tapMu.Unlock()
	tap, err := b.startTap(b.mouseMask)
	if err != nil {
		return err
	}
	b.tap = tap
	return nil
}

// stopTap stops the event tap, if it runs.
func (b *burin) stopTap() {
	b.tapMu.Lock()
	defer b.tapMu.Unlock()
	if b.tap != nil {
		b.tap.Stop()
	}
}

// setMouseMask rebuilds the event tap when the mouse events to listen to
// change, as a tap's mask is fixed when it starts. Before listen, or when
// the tap could not start, the mask is only kept for listen.
func (b *burin) setMouseMask(mask uint64) {
	b.tapMu.Lock()
	defer b.tapMu.Unlock()
	if mask == b.mouseMask {
		return
	}
	if b.tap == nil {
		b.mouseMask = mask
		return
	}
	if !b.tap.Stop() {
		log.Print("event tap did not stop; mouse gestures keep the old buttons")
		return
	}
	// Only the old tap saw the release of a press it tracked.
	b.gestureMu.Lock()
	b.recognizer.Reset()
	b.io.releaseEvent(b.downCopy)
	b.downCopy = 0
	b.hideHUD()
	b.gestureMu.Unlock()
	tap, err := b.startTap(mask)
	if err == nil {
		b.tap, b.mouseMask = tap, mask
		return
	}
	log.Printf("event tap: %v; restarting with the old mouse buttons", err)
	if tap, err = b.startTap(b.mouseMask); err != nil {
		log.Printf("event tap: %v", err)
		b.tap = nil
		return
	}
	b.tap = tap
}

// newLeader returns the leader state machine, wired to the HUD and to the
// entry action.
func (b *burin) newLeader(run func(leader.Config, leader.Entry) error) *leader.Leader {
	withHUD := func(fn func(h *hud.HUD)) func() {
		return func() {
			if h := b.hud.Load(); h != nil {
				fn(h)
			}
		}
	}
	showHUD := withHUD((*hud.HUD).Show)
	return &leader.Leader{
		OnShow: func() {
			// The expander no longer sees the keys of a hold, which used to
			// reset its buffer as modified keys: reset it here instead, and
			// cancel a pending expansion.
			b.ex.Stop()
			showHUD()
		},
		OnHide:   withHUD((*hud.HUD).Hide),
		OnTap:    keys.Stroke,
		OnChange: withHUD((*hud.HUD).Invalidate),
		OnRun:    run,
	}
}

// folderItem is a status line of the tray menu that opens dir, so the
// counts lead to the files they count. A missing dir is created first.
func folderItem(label, dir string) *mygo.MenuItem {
	return &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Print(err)
			return
		}
		if err := mygo.Shell.OpenPath(dir); err != nil {
			log.Print(err)
		}
	}}
}

// openAtLoginItem is the checkbox that registers the app as a login item.
// A development build is a temporary bundle no login item should point
// at, so the item is disabled there.
func openAtLoginItem() *mygo.MenuItem {
	if mygo.IsDev() {
		return &mygo.MenuItem{Label: "Open at Login (dev build)", Disabled: true}
	}
	return &mygo.MenuItem{
		Label:   "Open at Login",
		Type:    mygo.MenuItemCheckbox,
		Checked: mygo.App.OpenAtLogin(),
		// Checked is already toggled when Click runs.
		Click: func(it *mygo.MenuItem, _ *mygo.Window) {
			open := it.IsChecked()
			if err := mygo.App.SetOpenAtLogin(open); err != nil {
				log.Printf("open at login: %v", err)
				notify("Open at Login", err.Error())
				it.SetChecked(!open)
			}
		},
	}
}

// notify shows a system notification without blocking the caller, which
// may be the event tap thread. Notification.Show hops to the main thread
// itself and waits for the user's permission on first use.
func notify(title, body string) {
	if len(body) > maxNotifyBody {
		body = strings.ToValidUTF8(body[:maxNotifyBody], "") + "…"
	}
	go func() {
		n := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body})
		if err := n.Show(); err != nil {
			log.Printf("notification %q: %v", title, err)
		}
	}()
}

// reload rescans the snippets, leaders and mouse directories and rereads
// the window rules and quitter rules.
func (b *burin) reload() {
	b.reloadSnippets()
	b.reloadLeaders()
	b.reloadMouse()
	b.reloadWindowRules()
	b.reloadQuitter()
}

// reloadSnippets rescans the snippets directory and swaps the keyword
// table.
func (b *burin) reloadSnippets() {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	tbl := snippet.Load(b.snippetsDir)
	b.ex.SetSnippets(expanderSnippets(tbl, b.eval, placeholderFailed))
	status := statusText(tbl)
	log.Printf("loaded %s from %s", status, b.snippetsDir)
	b.status.SetLabel(status)
}

// reloadLeaders rescans the leaders directory. Files that name no trigger
// key are announced; parse errors show in the HUD instead.
func (b *burin) reloadLeaders() {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	set := leader.Load(b.leadersDir)
	b.leader.SetConfigs(set.Configs)
	for _, err := range set.Errors {
		log.Printf("leader: %v", err)
		notify("Leader skipped", err.Error())
	}
	for _, c := range set.Configs {
		if c.Err != nil {
			log.Printf("leader %s: %v", c.Key, c.Err)
		}
	}
	status := leaderStatusText(set)
	log.Printf("loaded %s from %s", status, b.leadersDir)
	b.leaderStatus.SetLabel(status)
}

// reloadMouse rescans the mouse directory, swaps the gesture rules and
// rebuilds the tap when the buttons change. Errors are announced, as
// there is no HUD to show them.
func (b *burin) reloadMouse() {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	set := gesture.Load(b.mouseDir)
	b.gestures.Store(&set)
	b.setMouseMask(set.Mask())
	for _, err := range set.AllErrors() {
		log.Printf("mouse: %v", err)
		notify("Mouse gestures skipped", err.Error())
	}
	status := mouseStatusText(set)
	log.Printf("loaded %s from %s", status, b.mouseDir)
	b.mouseStatus.SetLabel(status)
}

// reloadWindowRules rereads the window rules file and hands its rules to
// the engine. A missing file has no rules.
func (b *burin) reloadWindowRules() {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	set := windowrules.Load(b.windowRulesFile)
	b.rules.SetConfig(set.Config)
	for _, err := range set.Errors {
		log.Printf("window rules: %v", err)
	}
	status := windowRulesStatusText(set)
	log.Printf("loaded %s from %s", status, b.windowRulesFile)
	b.rulesStatus.SetLabel(status)
}

// reloadQuitter rereads the quitter rules file and hands its rules to the
// engine. A missing file has no rules.
func (b *burin) reloadQuitter() {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	set := quitter.Load(b.quitterFile)
	b.quitter.SetConfig(set.Config)
	for _, err := range set.Errors {
		log.Printf("quitter: %v", err)
	}
	status := quitterStatusText(set)
	log.Printf("loaded %s from %s", status, b.quitterFile)
	b.quitterStatus.SetLabel(status)
}

// newLeaderEvaluator is the evaluator of leader entries and mouse
// gestures: the snippet built-ins plus reload, open, toggle and key,
// which snippets do not get.
func (b *burin) newLeaderEvaluator(scriptsDir string, path func() string) *placeholder.Evaluator {
	ev := newEvaluator(scriptsDir, path)
	ev.Builtins["reload"] = func(args []string, _ string) (string, error) {
		if len(args) != 0 {
			return "", fmt.Errorf("takes no arguments, got %d", len(args))
		}
		b.reload()
		return "", nil
	}
	// open and toggle switch apps in process rather than forking
	// open -a, and return nothing so an entry needs no |null.
	ev.Builtins["open"] = appBuiltin(workspace.Open)
	ev.Builtins["toggle"] = appBuiltin(workspace.Toggle)
	ev.Builtins["key"] = keyBuiltin
	return ev
}

// keyBuiltin presses a key combination such as cmd+[ or ctrl+up, its one
// argument. The keys are marked, so the leader passes them on.
func keyBuiltin(args []string, _ string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("takes one argument, the keys such as cmd+[, got %d", len(args))
	}
	keycode, flags, err := leader.ParseStroke(args[0])
	if err != nil {
		return "", err
	}
	keys.Stroke(keycode, flags)
	return "", nil
}

// appBuiltin makes a builtin of fn, which takes the app name as its one
// argument.
func appBuiltin(fn func(name string) error) placeholder.Func {
	return func(args []string, _ string) (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("takes one argument, the app name, got %d", len(args))
		}
		// An empty name would match every process without a bundle ID.
		if args[0] == "" {
			return "", errors.New("needs an app name")
		}
		return "", fn(args[0])
	}
}

func watch(dir string, onChange func()) *snippet.Watcher {
	w, err := snippet.Watch(dir, snippet.DefaultWatchDelay, onChange)
	if err != nil {
		log.Printf("watch %s: %v", dir, err)
		return nil
	}
	return w
}

func main() {
	openLog()
	dir := configDir()
	scriptsDir := filepath.Join(dir, "scripts")
	loginPath := placeholder.StartLoginPath()
	b := &burin{
		snippetsDir:     filepath.Join(dir, "snippets"),
		leadersDir:      filepath.Join(dir, "leaders"),
		mouseDir:        filepath.Join(dir, "mouse"),
		windowRulesFile: filepath.Join(dir, windowrules.FileName),
		quitterFile:     filepath.Join(dir, quitter.FileName),
		eval:            newEvaluator(scriptsDir, loginPath),
		ex:              expander.New(nil, expander.DefaultDelay, expander.Delete, expander.Paste),
		io:              systemEventIO(),
		spawn:           goSpawn,
	}
	b.startTap = b.startEventTap
	b.showGestureHUD = b.showGestureLines
	b.hideGestureHUD = b.hideGestureLines
	b.leaderHeld = func() bool { return b.leader.Active() }
	b.status = folderItem("Loading snippets", b.snippetsDir)
	b.leaderStatus = folderItem("Loading leaders", b.leadersDir)
	b.rulesStatus = folderItem("Loading window rules", dir)
	b.quitterStatus = folderItem("Loading quitter rules", dir)
	b.mouseStatus = folderItem("Loading mouse gestures", b.mouseDir)
	leaderEval := b.newLeaderEvaluator(scriptsDir, loginPath)
	b.leader = b.newLeader(leaderAction(leaderEval, pasteboard.WriteText, notify, placeholderFailed))
	b.runGesture = expandAction(leaderEval, pasteboard.WriteText, notify, placeholderFailed)
	for _, d := range []string{b.snippetsDir, b.leadersDir, b.mouseDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Print(err)
		}
	}

	mygo.App.WhenReady(func() {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)

		b.reload()
		snippetsWatcher := watch(b.snippetsDir, b.reloadSnippets)
		leadersWatcher := watch(b.leadersDir, b.reloadLeaders)
		mouseWatcher := watch(b.mouseDir, b.reloadMouse)
		// The watch is recursive and reports no file names, so any change
		// under the config dir rereads the rules files, which is cheap.
		rulesWatcher := watch(dir, func() {
			b.reloadWindowRules()
			b.reloadQuitter()
		})
		// A display that drops and returns moves every window twice;
		// rounds hold meanwhile so the rules do not clamp the windows to
		// the display they pass through.
		mygo.Screen.OnDisplaysChanged(b.rules.DisplaysChanged)
		b.rules.Start(windowrules.DefaultInterval)
		b.quitter.Start(quitter.DefaultInterval)

		// Only check here: the Grant item below prompts on request.
		// Accessibility covers Input Monitoring, which an active tap
		// could not run on alone.
		trusted := eventtap.AccessibilityTrusted(false)
		if err := b.listen(); err != nil {
			log.Print(err)
		}
		mygo.App.OnQuit(func() {
			b.ex.Stop()
			b.rules.Stop()
			b.quitter.Stop()
			b.stopTap()
			for _, w := range []*snippet.Watcher{snippetsWatcher, leadersWatcher, mouseWatcher, rulesWatcher} {
				if w != nil {
					w.Close()
				}
			}
		})

		items := []*mygo.MenuItem{b.status, b.leaderStatus, b.mouseStatus, b.rulesStatus, b.quitterStatus}
		if !trusted {
			items = append(items, &mygo.MenuItem{Label: grantAccessibility, Click: func(*mygo.MenuItem, *mygo.Window) {
				// Granted meanwhile: the tap needs a fresh process to see it.
				if eventtap.AccessibilityTrusted(false) {
					notify("Accessibility granted", "Relaunching Burin")
					mygo.App.Relaunch()
					return
				}
				go eventtap.AccessibilityTrusted(true)
			}})
		}
		items = append(items,
			mygo.Separator(),
			&mygo.MenuItem{Label: "Reload", Click: func(*mygo.MenuItem, *mygo.Window) { go b.reload() }},
			openAtLoginItem(),
		)
		if stamp := buildStampItem(); stamp != nil {
			items = append(items, stamp)
		}
		items = append(items,
			mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleQuit},
		)
		// The dev build titles its icon, to tell it from the installed app.
		tip, title := "Burin", ""
		if mygo.IsDev() {
			tip, title = "Burin (dev)", "dev"
		}
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:             "burin",
			Icon:           trayIcon,
			IconIsTemplate: true,
			Title:          title,
			ToolTip:        tip,
			Menu:           mygo.NewMenu(items),
		})
		if err != nil {
			log.Fatal(err)
		}
		b.hud.Store(hud.New(tray, b.hudSnapshot))
	})
	// The HUD flyout is the only window and is never closed, only hidden;
	// keep running regardless.
	mygo.App.OnWindowAllClosed(func() {})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
