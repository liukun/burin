package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"burin/internal/eventtap"
	"burin/internal/expander"
	"burin/internal/pasteboard"
	"burin/internal/placeholder"
	"burin/internal/snippet"
)

const (
	needAccessibility = "Grant Accessibility and Input Monitoring, then relaunch"
	maxNotifyBody     = 300
)

// burin holds the running app's snippet state.
type burin struct {
	snippetsDir string
	eval        *placeholder.Evaluator
	ex          *expander.Expander
	status      *mygo.MenuItem

	reloadMu sync.Mutex
}

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(home, ".config", "burin")
}

func newEvaluator(scriptsDir string, path string) *placeholder.Evaluator {
	runner := &placeholder.Runner{
		Dir: scriptsDir,
		Env: placeholder.EnvWithPath(os.Environ(), path),
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

func placeholderFailed(keyword string, errs []error) {
	msg := errors.Join(errs...).Error()
	log.Printf("snippet %q: %s", keyword, msg)
	notify(keyword, msg)
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

// reload rescans the snippets directory and swaps the keyword table.
func (b *burin) reload() {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	tbl := snippet.Load(b.snippetsDir)
	b.ex.SetSnippets(expanderSnippets(tbl, b.eval, placeholderFailed))
	status := statusText(tbl)
	log.Printf("loaded %s from %s", status, b.snippetsDir)
	b.status.SetLabel(status)
}

func main() {
	dir := configDir()
	b := &burin{
		snippetsDir: filepath.Join(dir, "snippets"),
		eval:        newEvaluator(filepath.Join(dir, "scripts"), placeholder.LoginPath()),
		ex:          expander.New(nil, expander.DefaultDelay, expander.Delete, expander.Paste),
		status:      &mygo.MenuItem{Label: "Loading snippets", Disabled: true},
	}
	if err := os.MkdirAll(b.snippetsDir, 0o755); err != nil {
		log.Print(err)
	}

	mygo.App.WhenReady(func() {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)

		b.reload()
		watcher, err := snippet.Watch(b.snippetsDir, snippet.DefaultWatchDelay, b.reload)
		if err != nil {
			log.Printf("watch %s: %v", b.snippetsDir, err)
		}

		tapOK := eventtap.AccessibilityTrusted(true)
		tap, err := expander.Listen(b.ex)
		if err != nil {
			log.Print(err)
			tapOK = false
		}
		mygo.App.OnQuit(func() {
			b.ex.Stop()
			if tap != nil {
				tap.Stop()
			}
			if watcher != nil {
				watcher.Close()
			}
		})

		items := []*mygo.MenuItem{b.status}
		if !tapOK {
			items = append(items, &mygo.MenuItem{Label: needAccessibility, Disabled: true})
		}
		items = append(items,
			mygo.Separator(),
			&mygo.MenuItem{Label: "Reload", Click: func(*mygo.MenuItem, *mygo.Window) { go b.reload() }},
			&mygo.MenuItem{Label: "Open Snippets Folder", Click: func(*mygo.MenuItem, *mygo.Window) {
				if err := mygo.Shell.OpenPath(b.snippetsDir); err != nil {
					log.Print(err)
				}
			}},
			mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleQuit},
		)
		_, err = mygo.NewTray(mygo.TrayOptions{
			ID:      "burin",
			Title:   "Burin",
			ToolTip: "Burin",
			Menu:    mygo.NewMenu(items),
		})
		if err != nil {
			log.Fatal(err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
