package main

import (
	"errors"
	"strings"
	"testing"

	"burin/internal/leader"
	"burin/internal/placeholder"
	"burin/internal/snippet"
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
	if got := statusText(tbl); got != "3 snippets, 1 errors" {
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
