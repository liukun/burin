package main

import (
	"testing"
	"time"
)

func TestSnippets(t *testing.T) {
	want := map[string]string{
		"xdate": time.Now().Format("2006-01-02"),
		"xname": "liukun",
	}
	for _, s := range snippets() {
		if got := s.Content(); got != want[s.Keyword] {
			t.Errorf("%s expands to %q, want %q", s.Keyword, got, want[s.Keyword])
		}
	}
}
