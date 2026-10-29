package main

import (
	"errors"
	"fmt"
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
	"burin/internal/snippet"
)

const (
	grantAccessibility = "Grant Accessibility (click again to relaunch)"
	maxNotifyBody      = 300
	maxCopiedPreview   = 40
)

// burin holds the running app's snippet and leader state.
type burin struct {
	snippetsDir string
	leadersDir  string
	eval        *placeholder.Evaluator
	ex          *expander.Expander
	leader      *leader.Leader
	// hud is set once the tray exists; the tap may fire before.
	hud          atomic.Pointer[hud.HUD]
	status       *mygo.MenuItem
	leaderStatus *mygo.MenuItem

	reloadMu sync.Mutex
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

// reload rescans the snippets and leaders directories.
func (b *burin) reload() {
	b.reloadSnippets()
	b.reloadLeaders()
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
	dir := configDir()
	scriptsDir := filepath.Join(dir, "scripts")
	loginPath := placeholder.StartLoginPath()
	b := &burin{
		snippetsDir:  filepath.Join(dir, "snippets"),
		leadersDir:   filepath.Join(dir, "leaders"),
		eval:         newEvaluator(scriptsDir, loginPath),
		ex:           expander.New(nil, expander.DefaultDelay, expander.Delete, expander.Paste),
		status:       &mygo.MenuItem{Label: "Loading snippets", Disabled: true},
		leaderStatus: &mygo.MenuItem{Label: "Loading leaders", Disabled: true},
	}
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
			if tap != nil {
				tap.Stop()
			}
			for _, w := range []*snippet.Watcher{snippetsWatcher, leadersWatcher} {
				if w != nil {
					w.Close()
				}
			}
		})

		items := []*mygo.MenuItem{b.status, b.leaderStatus}
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
			&mygo.MenuItem{Label: "Open Snippets Folder", Click: func(*mygo.MenuItem, *mygo.Window) {
				if err := mygo.Shell.OpenPath(b.snippetsDir); err != nil {
					log.Print(err)
				}
			}},
			mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleQuit},
		)
		title := "Burin"
		if mygo.IsDev() {
			title = "Burin (dev)"
		}
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:      "burin",
			Title:   title,
			ToolTip: title,
			Menu:    mygo.NewMenu(items),
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
