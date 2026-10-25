package expander

import (
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

type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) expand(keyword, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, keyword+"="+text)
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

const testDelay = 30 * time.Millisecond

func newTestExpander(r *recorder) *Expander {
	return New([]Snippet{{Keyword: "xname", Content: func() string { return "liukun" }}}, testDelay, r.expand)
}

func typeInto(e *Expander, s string) {
	for _, c := range s {
		e.HandleKey(Key{Chars: string(c)})
	}
}

func TestExpanderExpandsAfterDelay(t *testing.T) {
	r := &recorder{}
	e := newTestExpander(r)
	typeInto(e, "hi xname")
	if got := r.get(); len(got) != 0 {
		t.Fatalf("expanded before delay: %v", got)
	}
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 1 || got[0] != "xname=liukun" {
		t.Fatalf("calls %v", got)
	}
	if b := e.Buffer(); b != "" {
		t.Fatalf("buffer %q after match", b)
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
	time.Sleep(3 * testDelay)
	if got := r.get(); len(got) != 1 {
		t.Fatalf("calls %v", got)
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
	e := New([]Snippet{{Keyword: "xname", Content: func() string { return "x" }}}, testDelay, func(string, string) {
		close(started)
		<-release
	})
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
