package snippet

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	data := strings.Join([]string{
		"# Dates",
		"intro is a comment",
		"## gcb",
		"",
		"git checkout -b liukun--{date}--",
		"",
		"## yypp  ",
		"{date} Today's plan: ",
		"  - indented",
		"### not a heading",
		"##nospace",
		`\## escaped`,
		"",
		"## empty",
		"",
	}, "\n")
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []Snippet{
		{Keyword: "gcb", Content: "git checkout -b liukun--{date}--"},
		{Keyword: "yypp", Content: "{date} Today's plan: \n  - indented\n### not a heading\n##nospace\n## escaped"},
		{Keyword: "empty", Content: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestParseNoHeadings(t *testing.T) {
	got, err := Parse("# Title\nplain text\n### deeper\n")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseCRLF(t *testing.T) {
	got, err := Parse("## a\r\nline1\r\nline2\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "line1\nline2" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, data := range []string{"## \nbody", "##    \n", "## a\n\xff"} {
		if _, err := Parse(data); err == nil {
			t.Errorf("Parse(%q) succeeded", data)
		}
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "## one\nfirst\n## dup\nfrom a\n")
	writeFile(t, filepath.Join(dir, "sub", "b.md"), "## dup\nfrom b\n## two\nsecond\n")
	writeFile(t, filepath.Join(dir, "bad.md"), "## \nbroken\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "## txt\nignored\n")
	writeFile(t, filepath.Join(dir, "plain.md"), "no headings\n")

	tbl := Load(dir)
	got := map[string]string{}
	var order []string
	for _, s := range tbl.Snippets {
		got[s.Keyword] = s.Content
		order = append(order, s.Keyword)
	}
	want := map[string]string{"one": "first", "dup": "from b", "two": "second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snippets %v, want %v", got, want)
	}
	if !reflect.DeepEqual(order, []string{"one", "dup", "two"}) {
		t.Fatalf("order %v", order)
	}
	if len(tbl.Errors) != 1 || !strings.Contains(tbl.Errors[0].Error(), "bad.md") {
		t.Fatalf("errors %v", tbl.Errors)
	}
}

func TestLoadMissingDir(t *testing.T) {
	tbl := Load(filepath.Join(t.TempDir(), "missing"))
	if len(tbl.Snippets) != 0 || len(tbl.Errors) != 0 {
		t.Fatalf("got %+v", tbl)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatchDebouncesChanges(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	w, err := Watch(dir, 50*time.Millisecond, func() { calls.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := range 5 {
		writeFile(t, filepath.Join(dir, "a.md"), strings.Repeat("x", i))
	}
	waitFor(t, "first reload", func() bool { return calls.Load() >= 1 })
	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("burst of writes reloaded %d times", n)
	}

	// Files in a directory created after Watch are seen too.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reload after mkdir", func() bool { return calls.Load() >= 2 })
	writeFile(t, filepath.Join(sub, "b.md"), "## b\nx\n")
	waitFor(t, "reload after nested write", func() bool { return calls.Load() >= 3 })
}

func TestWatchCloseCancelsPending(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	w, err := Watch(dir, 100*time.Millisecond, func() { calls.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.md"), "x")
	time.Sleep(30 * time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("called %d times after Close", n)
	}
}

func TestLoadSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeFile(t, filepath.Join(real, "a.md"), "## a\nx\n")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	tbl := Load(link)
	if len(tbl.Errors) != 0 || len(tbl.Snippets) != 1 {
		t.Fatalf("Load via symlink = %+v", tbl)
	}
}

func TestWatchSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	w, err := Watch(link, 50*time.Millisecond, func() { calls.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	writeFile(t, filepath.Join(real, "a.md"), "## a\nx\n")
	waitFor(t, "reload via symlink root", func() bool { return calls.Load() >= 1 })
}
