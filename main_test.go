package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"burin/internal/activity"
	"burin/internal/badge"
	"burin/internal/gesture"
	"burin/internal/leader"
	"burin/internal/placeholder"
	"burin/internal/snippet"
	"burin/internal/windowrules"
)

func TestExpanderSnippets(t *testing.T) {
	ev := &placeholder.Evaluator{Builtins: map[string]placeholder.Func{
		"name": func([]string, string) (string, error) { return "liukun", nil },
		"fail": func([]string, string) (string, error) { return "", errors.New("boom") },
	}}
	tbl := snippet.Table{Snippets: []snippet.Snippet{
		{Keyword: "xname", Content: "hi {name}"},
		{Keyword: "xfail", Content: "x {fail}"},
	}}
	var failed []string
	got := expanderSnippets(tbl, ev, func(keyword string, errs []error) {
		failed = append(failed, keyword)
	})
	want := map[string]string{"xname": "hi liukun", "xfail": "x {fail}"}
	for _, s := range got {
		if out := s.Content(); out != want[s.Keyword] {
			t.Errorf("%s expands to %q, want %q", s.Keyword, out, want[s.Keyword])
		}
	}
	if len(failed) != 1 || failed[0] != "xfail" {
		t.Fatalf("failures reported for %v", failed)
	}
}

func TestStatusText(t *testing.T) {
	tbl := snippet.Table{
		Snippets: make([]snippet.Snippet, 3),
		Errors:   []error{errors.New("x")},
	}
	if got := statusText(tbl); got != "3 snippets, 1 error" {
		t.Fatalf("got %q", got)
	}
}

func TestLeaderStatusText(t *testing.T) {
	set := leader.Set{
		Configs: []leader.Config{{}, {Err: errors.New("parse")}},
		Errors:  []error{errors.New("not a trigger key")},
	}
	if got := leaderStatusText(set); got != "2 leaders, 2 errors" {
		t.Fatalf("got %q", got)
	}
}

func TestMouseStatusText(t *testing.T) {
	set := gesture.Set{
		Configs: []gesture.Config{{Button: 1}, {Button: 3, Err: errors.New("parse")}},
		Errors:  []error{errors.New("left button is not supported")},
	}
	if got := mouseStatusText(set); got != "2 mouse buttons, 2 errors" {
		t.Fatalf("got %q", got)
	}
}

// TestKeyBuiltinRejects covers the arguments that fail before any key is
// posted.
func TestKeyBuiltinRejects(t *testing.T) {
	for _, args := range [][]string{nil, {"cmd+a", "x"}, {"cmd+nokey"}, {"super+a"}, {""}} {
		if _, err := keyBuiltin(args, ""); err == nil {
			t.Errorf("%q: no error", args)
		}
	}
}

func TestWindowRulesStatusText(t *testing.T) {
	set := windowrules.Set{
		Config: windowrules.Config{
			Pins:    make([]windowrules.Rule, 3),
			Margins: make([]windowrules.MarginRule, 1),
		},
		Errors: []error{errors.New("x")},
	}
	if got := windowRulesStatusText(set); got != "4 window rules, 1 error" {
		t.Fatalf("got %q", got)
	}
}

func TestTimerStatusText(t *testing.T) {
	on := activity.Settings{Config: activity.DefaultConfig, Enabled: true, IdleApps: activity.Apps{{Name: "Slack"}}}
	if got := timerStatusText(on); got != "Timer 2m idle, 25m work, 1 break apps, 0 errors" {
		t.Errorf("on: got %q", got)
	}
	off := activity.Settings{Config: activity.DefaultConfig}
	if got := timerStatusText(off); got != "Timer off" {
		t.Errorf("off: got %q", got)
	}
	off.Errors = []error{errors.New("x")}
	if got := timerStatusText(off); got != "Timer off, 1 error" {
		t.Errorf("off with an error: got %q", got)
	}
}

func TestBadgesStatusText(t *testing.T) {
	on := badge.Set{Apps: []badge.App{{Name: "Slack"}}, Enabled: true}
	if got := badgesStatusText(on); got != "1 badge apps, 0 errors" {
		t.Errorf("on: got %q", got)
	}
	off := badge.Set{Apps: []badge.App{{Name: "Slack"}}, Errors: []error{errors.New("x"), errors.New("y")}}
	if got := badgesStatusText(off); got != "Badges off, 2 errors" {
		t.Errorf("off with errors: got %q", got)
	}
}

type actionLog struct {
	copied   []string
	notified []string
	failed   []string
}

func newAction(copyOK bool) (func(leader.Config, leader.Entry) error, *actionLog) {
	ev := &placeholder.Evaluator{Builtins: map[string]placeholder.Func{
		"date": func([]string, string) (string, error) { return "2026-10-26", nil },
		"null": func([]string, string) (string, error) { return "", nil },
		"fail": func([]string, string) (string, error) { return "", errors.New("boom") },
	}}
	l := &actionLog{}
	run := leaderAction(ev,
		func(s string) bool { l.copied = append(l.copied, s); return copyOK },
		func(title, body string) { l.notified = append(l.notified, title+": "+body) },
		func(name string, errs []error) { l.failed = append(l.failed, name) },
	)
	return run, l
}

func TestLeaderActionCopiesResult(t *testing.T) {
	run, l := newAction(true)
	err := run(leader.Config{Key: "cmd_r"}, leader.Entry{Key: "d", Title: "复制今日日期", Content: "{date}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.copied) != 1 || l.copied[0] != "2026-10-26" {
		t.Fatalf("copied %q", l.copied)
	}
	if len(l.notified) != 1 || l.notified[0] != "复制今日日期: Copied: 2026-10-26" {
		t.Fatalf("notified %q", l.notified)
	}
}

func TestLeaderActionEmptyResultDoesNothing(t *testing.T) {
	run, l := newAction(true)
	if err := run(leader.Config{Key: "cmd_r"}, leader.Entry{Key: "k", Content: "{date|null}"}); err != nil {
		t.Fatal(err)
	}
	if len(l.copied)+len(l.notified)+len(l.failed) != 0 {
		t.Fatalf("got %+v", l)
	}
}

func TestLeaderActionFailure(t *testing.T) {
	run, l := newAction(true)
	if err := run(leader.Config{Key: "cmd_r"}, leader.Entry{Key: "x", Content: "{fail}"}); err == nil {
		t.Fatal("no error")
	}
	if len(l.copied) != 0 || len(l.failed) != 1 || l.failed[0] != "cmd_r x" {
		t.Fatalf("got %+v", l)
	}

	run, l = newAction(false)
	if err := run(leader.Config{Key: "cmd_r"}, leader.Entry{Key: "d", Content: "{date}"}); err == nil {
		t.Fatal("no error when the pasteboard fails")
	}
	if len(l.notified) != 0 || len(l.failed) != 1 {
		t.Fatalf("got %+v", l)
	}
}

func TestPreview(t *testing.T) {
	if got := preview("短文本", 40); got != "短文本" {
		t.Fatalf("got %q", got)
	}
	if got := preview(strings.Repeat("字", 40), 40); got != strings.Repeat("字", 40) {
		t.Fatalf("exact length cut: %q", got)
	}
	long := strings.Repeat("字", 41)
	if got := preview(long, 40); got != strings.Repeat("字", 40)+"…" {
		t.Fatalf("got %q", got)
	}
}

func TestChainStopsAtFirstConsumer(t *testing.T) {
	var calls []string
	handler := func(name string, consume bool) func(uint32, uintptr) bool {
		return func(uint32, uintptr) bool {
			calls = append(calls, name)
			return consume
		}
	}
	if !chain(handler("leader", true), handler("expander", false))(10, 0) {
		t.Fatal("not consumed")
	}
	if strings.Join(calls, ",") != "leader" {
		t.Fatalf("calls %v", calls)
	}
	calls = nil
	if chain(handler("leader", false), handler("expander", false))(10, 0) {
		t.Fatal("consumed")
	}
	if strings.Join(calls, ",") != "leader,expander" {
		t.Fatalf("calls %v", calls)
	}
}

func TestReloadOnlyForLeaders(t *testing.T) {
	path := func() string { return "" }
	b := &burin{}
	if _, ok := b.newLeaderEvaluator(t.TempDir(), path).Builtins["reload"]; !ok {
		t.Fatal("leader evaluator lacks reload")
	}
	if _, ok := newEvaluator(t.TempDir(), path).Builtins["reload"]; ok {
		t.Fatal("snippet evaluator has reload")
	}
	if _, errs := b.newLeaderEvaluator(t.TempDir(), path).Expand("{reload(x)}"); len(errs) != 1 {
		t.Fatalf("reload with arguments: %v", errs)
	}
}

func TestAppBuiltinsOnlyForLeaders(t *testing.T) {
	path := func() string { return "" }
	b := &burin{}
	leaderEval := b.newLeaderEvaluator(t.TempDir(), path)
	snippetEval := newEvaluator(t.TempDir(), path)
	for _, name := range []string{"open", "toggle"} {
		if _, ok := leaderEval.Builtins[name]; !ok {
			t.Errorf("leader evaluator lacks %s", name)
		}
		if _, ok := snippetEval.Builtins[name]; ok {
			t.Errorf("snippet evaluator has %s", name)
		}
	}
	// None of these reach the workspace calls: the arguments are checked
	// first.
	for _, in := range []string{"{open()}", "{toggle(a,b)}", `{toggle("")}`} {
		if _, errs := leaderEval.Expand(in); len(errs) != 1 {
			t.Errorf("%s: %v", in, errs)
		}
	}
}

func TestEnsureAndOpen(t *testing.T) {
	src := fstest.MapFS{"quitter.md": {Data: []byte("## Preview\n")}}
	dir := filepath.Join(t.TempDir(), "burin")
	var opened []string
	open := func(path string) error {
		opened = append(opened, path)
		return nil
	}
	path := filepath.Join(dir, "quitter.md")

	if err := ensureAndOpen(src, dir, "quitter.md", open); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "## Preview\n" {
		t.Fatalf("missing file written as %q", data)
	}

	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureAndOpen(src, dir, "quitter.md", open); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Fatalf("existing file changed to %q", data)
	}
	if len(opened) != 2 || opened[0] != dir || opened[1] != dir {
		t.Fatalf("opened %v, want %s twice", opened, dir)
	}
}

func TestTildePath(t *testing.T) {
	tests := []struct{ path, home, want string }{
		{"/Users/a/.config/burin", "/Users/a", "~/.config/burin"},
		{"/Users/a", "/Users/a", "~"},
		{"/Users/ab/x", "/Users/a", "/Users/ab/x"},
		{"/tmp/x", "/Users/a", "/tmp/x"},
		{"/tmp/x", "", "/tmp/x"},
	}
	for _, tt := range tests {
		if got := tildePath(tt.path, tt.home); got != tt.want {
			t.Errorf("tildePath(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
		}
	}
}

func TestErrorsText(t *testing.T) {
	for n, want := range map[int]string{0: "0 errors", 1: "1 error", 2: "2 errors"} {
		if got := errorsText(n); got != want {
			t.Errorf("errorsText(%d) = %q, want %q", n, got, want)
		}
	}
}
