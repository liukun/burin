package main

import (
	"errors"
	"testing"

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
