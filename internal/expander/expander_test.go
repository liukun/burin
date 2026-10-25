package expander

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func typeText(b *Buffer, s string) {
	for _, r := range s {
		b.Feed(Key{Chars: string(r)})
	}
}

func TestBufferAppendsPrintable(t *testing.T) {
	var b Buffer
	typeText(&b, "héllo 世界")
	if got := b.String(); got != "héllo 世界" {
		t.Fatalf("buffer %q", got)
	}
}

func TestBufferIgnoresNonPrintable(t *testing.T) {
	var b Buffer
	typeText(&b, "ab")
	if b.Feed(Key{Keycode: 99, Chars: ""}) {
		t.Error("empty chars reported as appended")
	}
	if b.Feed(Key{Keycode: 99, Chars: "\x10"}) {
		t.Error("control char reported as appended")
	}
	if got := b.String(); got != "ab" {
		t.Fatalf("buffer %q", got)
	}
}

func TestBufferBackspacePopsOne(t *testing.T) {
	var b Buffer
	typeText(&b, "ad")
	b.Feed(Key{Keycode: keycodeDelete, Chars: "\x7f"})
	typeText(&b, "bc")
	if got := b.String(); got != "abc" {
		t.Fatalf("buffer %q", got)
	}
	var empty Buffer
	empty.Feed(Key{Keycode: keycodeDelete})
	if got := empty.String(); got != "" {
		t.Fatalf("buffer %q after backspace on empty", got)
	}
}

func TestBufferClearKeys(t *testing.T) {
	for code := range clearKeycodes {
		var b Buffer
		typeText(&b, "abc")
		b.Feed(Key{Keycode: code})
		if got := b.String(); got != "" {
			t.Errorf("keycode %d left %q", code, got)
		}
	}
}

func TestBufferModifiersClear(t *testing.T) {
	for _, flag := range []uint64{flagMaskCommand, flagMaskControl, flagMaskAlternate} {
		var b Buffer
		typeText(&b, "abc")
		if b.Feed(Key{Flags: flag, Chars: "v"}) {
			t.Errorf("flag %#x reported as appended", flag)
		}
		if got := b.String(); got != "" {
			t.Errorf("flag %#x left %q", flag, got)
		}
	}
	var b Buffer
	shift := uint64(1 << 17)
	b.Feed(Key{Flags: shift, Chars: "A"})
	if got := b.String(); got != "A" {
		t.Errorf("shift: buffer %q", got)
	}
}

func TestBufferCapsLength(t *testing.T) {
	var b Buffer
	typeText(&b, strings.Repeat("a", MaxBuffer)+"xyz")
	got := b.String()
	if len([]rune(got)) != MaxBuffer || !strings.HasSuffix(got, "xyz") {
		t.Fatalf("buffer len %d, suffix %q", len([]rune(got)), got[len(got)-3:])
	}
}

func TestMatch(t *testing.T) {
	snippets := []Snippet{
		{Keyword: ""},
		{Keyword: "xdate"},
		{Keyword: "date"},
	}
	s, ok := Match("hello xdate", snippets)
	if !ok || s.Keyword != "xdate" {
		t.Fatalf("got %q %v", s.Keyword, ok)
	}
	s, ok = Match("update", snippets)
	if !ok || s.Keyword != "date" {
		t.Fatalf("got %q %v", s.Keyword, ok)
	}
	if _, ok := Match("xdat", snippets); ok {
		t.Fatal("partial keyword matched")
	}
}

func TestMatchPrefersLongest(t *testing.T) {
	snippets := []Snippet{{Keyword: "hm"}, {Keyword: "+yhm"}, {Keyword: "yhm"}}
	s, ok := Match("at +yhm", snippets)
	if !ok || s.Keyword != "+yhm" {
		t.Fatalf("got %q %v", s.Keyword, ok)
	}
	s, ok = Match("=hm", snippets)
	if !ok || s.Keyword != "hm" {
		t.Fatalf("got %q %v", s.Keyword, ok)
	}
}

func TestPlan(t *testing.T) {
	tests := []struct {
		content    string
		afterSpace bool
		want       string
	}{
		{"liukun", false, "liukun"},
		{"liukun", true, "liukun "},
		{"foo ", true, "foo "},
		{"foo ", false, "foo "},
		{"foo\n", true, "foo\n "},
		{"", false, ""},
		{"", true, ""},
	}
	for _, tt := range tests {
		if got := plan(tt.content, tt.afterSpace); got != tt.want {
			t.Errorf("plan(%q, %v) = %q, want %q", tt.content, tt.afterSpace, got, tt.want)
		}
	}
}

// recorder logs delete, paste and content-evaluation events in order.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) record(ev string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) delete(n int) { r.record(fmt.Sprintf("del %d", n)) }

func (r *recorder) paste(keyword, text string) {
	r.record(fmt.Sprintf("paste %s=%q", keyword, text))
}

// content returns a Content func that records its evaluation.
func (r *recorder) content(text string) func() string {
	return func() string {
		r.record("content")
		return text
	}
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *recorder) newExpander(snippets []Snippet, delay time.Duration) *Expander {
	return New(snippets, delay, r.delete, r.paste)
}

const testDelay = 30 * time.Millisecond

func newTestExpander(r *recorder) *Expander {
	return r.newExpander([]Snippet{{Keyword: "xname", Content: r.content("liukun")}}, testDelay)
}

func typeInto(e *Expander, s string) {
	for _, c := range s {
		e.HandleKey(Key{Chars: string(c)})
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func waitEvents(t *testing.T, r *recorder, n int) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		got := r.get()
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(time.Millisecond)
	}
}

func TestExpanderExpandsAfterDelay(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "hi xname")
	if got := r.get(); len(got) != 0 {
		t.Fatalf("expanded before delay: %v", got)
	}
	want := []string{"del 5", "content", `paste xname="liukun"`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	if b := e.Buffer(); b != "" {
		t.Fatalf("buffer %q after match", b)
	}
}

func TestExpanderDeletesBeforeEvaluating(t *testing.T) {
	r := &recorder{}
	e := r.newExpander([]Snippet{{Keyword: "日期", Content: r.content("x")}}, time.Hour)
	typeInto(e, "日期 ")
	want := []string{"del 2", "content", `paste 日期="x "`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestExpanderContentPanicRestoresKeyword(t *testing.T) {
	boom := func() string { panic("boom") }
	tests := []struct {
		typed string
		want  []string
	}{
		{"xname", []string{"del 5", `paste xname="xname"`}},
		{"xname ", []string{"del 5", `paste xname="xname "`}},
	}
	for _, tt := range tests {
		r := &recorder{}
		e := r.newExpander([]Snippet{{Keyword: "xname", Content: boom}}, testDelay)
		typeInto(e, tt.typed)
		if got := waitEvents(t, r, len(tt.want)); !equal(got, tt.want) {
			t.Errorf("typed %q: events %v, want %v", tt.typed, got, tt.want)
		}
	}
}

func TestExpanderDebounceCancels(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "xnames")
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 0 {
		t.Fatalf("expanded despite continued typing: %v", got)
	}
}

func TestExpanderMatchesAfterBackspaceCorrection(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "xnq")
	e.HandleKey(Key{Keycode: keycodeDelete})
	typeInto(e, "ame")
	want := []string{"del 5", "content", `paste xname="liukun"`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestExpanderArrowClears(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "xna")
	e.HandleKey(Key{Keycode: 123})
	typeInto(e, "me")
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 0 {
		t.Fatalf("expanded across arrow key: %v", got)
	}
}

func TestExpanderIgnoresKeysWhileExpanding(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	block := func(int) {
		close(started)
		<-release
	}
	e := New([]Snippet{{Keyword: "xname", Content: func() string { return "x" }}}, testDelay, block, func(string, string) {})
	typeInto(e, "xname")
	<-started
	typeInto(e, "abc")
	close(release)
	if b := e.Buffer(); b != "" {
		t.Fatalf("buffer %q took keys typed during expansion", b)
	}
}

func TestExpanderStopCancelsPending(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "xname")
	e.Stop()
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 0 {
		t.Fatalf("expanded after Stop: %v", got)
	}
}

func TestExpanderSpaceExpandsImmediately(t *testing.T) {
	r := &recorder{}
	e := r.newExpander([]Snippet{{Keyword: "xname", Content: r.content("liukun")}}, time.Hour)
	typeInto(e, "hi xname")
	if !e.HandleKey(Key{Chars: " "}) {
		t.Fatal("matching space not consumed")
	}
	want := []string{"del 5", "content", `paste xname="liukun "`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	if b := e.Buffer(); b != "" {
		t.Fatalf("buffer %q after space expansion", b)
	}
}

func TestExpanderSpaceContentEndingInSpace(t *testing.T) {
	r := &recorder{}
	e := r.newExpander([]Snippet{{Keyword: "xfoo", Content: r.content("foo ")}}, time.Hour)
	typeInto(e, "xfoo ")
	want := []string{"del 4", "content", `paste xfoo="foo "`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestExpanderEmptyContentOnlyDeletes(t *testing.T) {
	for _, typed := range []string{"ttrn", "ttrn "} {
		r := &recorder{}
		e := r.newExpander([]Snippet{{Keyword: "ttrn", Content: r.content("")}}, testDelay)
		typeInto(e, typed)
		want := []string{"del 4", "content"}
		waitEvents(t, r, len(want))
		time.Sleep(3 * testDelay)
		if got := r.get(); !equal(got, want) {
			t.Errorf("typed %q: events %v, want %v", typed, got, want)
		}
	}
}

func TestExpanderSpaceWithoutMatchIsText(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "xnam")
	if e.HandleKey(Key{Chars: " "}) {
		t.Fatal("non-matching space consumed")
	}
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 0 {
		t.Fatalf("events %v", got)
	}
	if b := e.Buffer(); b != "xnam " {
		t.Fatalf("buffer %q", b)
	}
}

func TestExpanderModifiedSpaceDoesNotExpand(t *testing.T) {
	r := &recorder{}
	e := r.newExpander([]Snippet{{Keyword: "xname", Content: r.content("x")}}, time.Hour)
	typeInto(e, "xnam")
	e.HandleKey(Key{Chars: "e"})
	if e.HandleKey(Key{Chars: " ", Flags: flagMaskControl}) {
		t.Fatal("modified space consumed")
	}
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 0 {
		t.Fatalf("events %v", got)
	}
}

func TestExpanderSpaceDuringDelayExpandsOnce(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "xname ")
	want := []string{"del 5", "content", `paste xname="liukun "`}
	waitEvents(t, r, len(want))
	time.Sleep(3 * testDelay)
	if got := r.get(); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestExpanderLongestKeywordWins(t *testing.T) {
	r := &recorder{}
	e := r.newExpander([]Snippet{
		{Keyword: "hm", Content: r.content("short")},
		{Keyword: "+yhm", Content: r.content("long")},
	}, testDelay)
	typeInto(e, "+yhm")
	want := []string{"del 4", "content", `paste +yhm="long"`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestExpanderSetSnippets(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	e.SetSnippets([]Snippet{{Keyword: "xnew", Content: r.content("new")}})
	typeInto(e, "xname ")
	typeInto(e, "xnew ")
	want := []string{"del 4", "content", `paste xnew="new "`}
	if got := waitEvents(t, r, len(want)); !equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}
