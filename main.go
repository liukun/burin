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
	"burin/internal/hud"
	"burin/internal/keys"
	"burin/internal/leader"
	"burin/internal/pasteboard"
	"burin/internal/placeholder"
	"burin/internal/quitter"
	"burin/internal/snippet"
	"burin/internal/windowrules"
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

// burin holds the running app's snippet, leader, window rule and quitter
// state.
type burin struct {
	snippetsDir string
	leadersDir  string
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
	return func(c leader.Config, e leader.Entry) error {
		name := entryName(c, e)
		text, errs := ev.Expand(e.Content)
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

// listen starts the only event tap, which offers every key event to the
// leader first and then to the expander.
func (b *burin) listen() (*eventtap.Tap, error) {
	mask := eventtap.MaskBit(eventtap.EventKeyDown) |
		eventtap.MaskBit(eventtap.EventKeyUp) |
		eventtap.MaskBit(eventtap.EventFlagsChanged)
	tap := &eventtap.Tap{}
	if err := tap.Start(mask, chain(b.handleLeader, b.handleExpander)); err != nil {
		return nil, err
	}
	return tap, nil
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

// reload rescans the snippets and leaders directories and rereads the
// window rules and quitter rules.
func (b *burin) reload() {
	b.reloadSnippets()
	b.reloadLeaders()
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

// newLeaderEvaluator is the evaluator of leader entries: the snippet
// built-ins plus reload, which snippets do not get.
func (b *burin) newLeaderEvaluator(scriptsDir string, path func() string) *placeholder.Evaluator {
	ev := newEvaluator(scriptsDir, path)
	ev.Builtins["reload"] = func(args []string, _ string) (string, error) {
		if len(args) != 0 {
			return "", fmt.Errorf("takes no arguments, got %d", len(args))
		}
		b.reload()
		return "", nil
	}
	return ev
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
		windowRulesFile: filepath.Join(dir, windowrules.FileName),
		quitterFile:     filepath.Join(dir, quitter.FileName),
		eval:            newEvaluator(scriptsDir, loginPath),
		ex:              expander.New(nil, expander.DefaultDelay, expander.Delete, expander.Paste),
	}
	b.status = folderItem("Loading snippets", b.snippetsDir)
	b.leaderStatus = folderItem("Loading leaders", b.leadersDir)
	b.rulesStatus = folderItem("Loading window rules", dir)
	b.quitterStatus = folderItem("Loading quitter rules", dir)
	leaderEval := b.newLeaderEvaluator(scriptsDir, loginPath)
	b.leader = b.newLeader(leaderAction(leaderEval, pasteboard.WriteText, notify, placeholderFailed))
	for _, d := range []string{b.snippetsDir, b.leadersDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Print(err)
		}
	}

	mygo.App.WhenReady(func() {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)

		b.reload()
		snippetsWatcher := watch(b.snippetsDir, b.reloadSnippets)
		leadersWatcher := watch(b.leadersDir, b.reloadLeaders)
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
		tap, err := b.listen()
		if err != nil {
			log.Print(err)
		}
		mygo.App.OnQuit(func() {
			b.ex.Stop()
			b.rules.Stop()
			b.quitter.Stop()
			if tap != nil {
				tap.Stop()
			}
			for _, w := range []*snippet.Watcher{snippetsWatcher, leadersWatcher, rulesWatcher} {
				if w != nil {
					w.Close()
				}
			}
		})

		items := []*mygo.MenuItem{b.status, b.leaderStatus, b.rulesStatus, b.quitterStatus}
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
		b.hud.Store(hud.New(tray, b.leader.Snapshot))
	})
	// The HUD flyout is the only window and is never closed, only hidden;
	// keep running regardless.
	mygo.App.OnWindowAllClosed(func() {})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
