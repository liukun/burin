// Package expander watches typed text for snippet keywords and replaces
// them.
package expander

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxBuffer is the longest keyword that can match.
	MaxBuffer = 128
	// DefaultDelay is how long a match waits for more typing before it
	// expands.
	DefaultDelay = 200 * time.Millisecond

	keycodeDelete = 51

	flagMaskControl   = 1 << 18
	flagMaskAlternate = 1 << 19
	flagMaskCommand   = 1 << 20
	modifierMask      = flagMaskCommand | flagMaskControl | flagMaskAlternate
)

// clearKeycodes are navigation and control keys that reset the buffer.
var clearKeycodes = map[int64]bool{
	36:  true, // return
	48:  true, // tab
	53:  true, // escape
	76:  true, // enter (numpad)
	117: true, // forward delete
	123: true, // left arrow
	124: true, // right arrow
	125: true, // down arrow
	126: true, // up arrow
}

// Snippet maps a keyword to the text that replaces it. Content runs when the
// snippet expands.
type Snippet struct {
	Keyword string
	Content func() string
}

// Key is one keyDown event.
type Key struct {
	Keycode int64
	Flags   uint64
	Chars   string
}

// Buffer is the rolling text typed since the last reset.
type Buffer struct {
	runes []rune
}

func (b *Buffer) String() string { return string(b.runes) }

func (b *Buffer) Reset() { b.runes = b.runes[:0] }

// Feed applies a key to the buffer and reports whether the key appended
// text, after which the buffer should be checked for a keyword.
func (b *Buffer) Feed(k Key) bool {
	if k.Flags&modifierMask != 0 {
		b.Reset()
		return false
	}
	if k.Keycode == keycodeDelete {
		if len(b.runes) > 0 {
			b.runes = b.runes[:len(b.runes)-1]
		}
		return false
	}
	if clearKeycodes[k.Keycode] {
		b.Reset()
		return false
	}
	if !isPrintable(k.Chars) {
		return false
	}
	b.runes = append(b.runes, []rune(k.Chars)...)
	if over := len(b.runes) - MaxBuffer; over > 0 {
		b.runes = append(b.runes[:0], b.runes[over:]...)
	}
	return true
}

func isPrintable(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Match returns the snippet with the longest keyword that ends text.
func Match(text string, snippets []Snippet) (Snippet, bool) {
	var best Snippet
	for _, s := range snippets {
		isLonger := len(s.Keyword) > len(best.Keyword)
		if isLonger && strings.HasSuffix(text, s.Keyword) {
			best = s
		}
	}
	return best, best.Keyword != ""
}

// DeleteFunc removes the last n characters typed in the focused app.
type DeleteFunc func(n int)

// PasteFunc inserts text into the focused app. keyword names the snippet
// being expanded, for logging.
type PasteFunc func(keyword, text string)

// Expander matches keys against snippets and expands one once typing has
// paused for delay after a match, or right away when the keyword is
// followed by a space. It is safe for concurrent use.
type Expander struct {
	snippets []Snippet
	delay    time.Duration
	delete   DeleteFunc
	paste    PasteFunc

	mu        sync.Mutex
	buf       Buffer
	pending   *time.Timer
	gen       uint64
	expanding atomic.Bool
}

// New returns an Expander that expands a snippet by calling del to remove
// the typed keyword and then paste to insert the content.
func New(snippets []Snippet, delay time.Duration, del DeleteFunc, paste PasteFunc) *Expander {
	return &Expander{snippets: snippets, delay: delay, delete: del, paste: paste}
}

// SetSnippets replaces the snippets matched from the next key on.
func (e *Expander) SetSnippets(snippets []Snippet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snippets = snippets
}

// HandleKey processes one keyDown and reports whether the key must be
// consumed. A plain space that completes a keyword is consumed: it is put
// back after the content, so it must not reach the app, where it could
// race the backspaces. It never blocks on expansion.
func (e *Expander) HandleKey(k Key) bool {
	if e.expanding.Load() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelPendingLocked()
	if isPlainSpace(k) {
		if s, ok := Match(e.buf.String(), e.snippets); ok {
			e.buf.Reset()
			e.expanding.Store(true)
			go e.run(s, true)
			return true
		}
	}
	if !e.buf.Feed(k) {
		return false
	}
	s, ok := Match(e.buf.String(), e.snippets)
	if !ok {
		return false
	}
	// The buffer keeps the keyword until the expansion fires, so a space
	// typed before then still finds it.
	gen := e.gen
	e.pending = time.AfterFunc(e.delay, func() { e.fire(s, gen) })
	return false
}

func isPlainSpace(k Key) bool {
	return k.Chars == " " && k.Flags&modifierMask == 0
}

// Stop cancels a pending expansion and clears the buffer.
func (e *Expander) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelPendingLocked()
	e.buf.Reset()
}

// Buffer returns the text typed since the last reset.
func (e *Expander) Buffer() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buf.String()
}

// cancelPendingLocked also bumps gen, so a timer that already fired but has
// not taken the lock yet gives up.
func (e *Expander) cancelPendingLocked() {
	e.gen++
	if e.pending == nil {
		return
	}
	e.pending.Stop()
	e.pending = nil
}

func (e *Expander) fire(s Snippet, gen uint64) {
	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		return
	}
	e.pending = nil
	e.buf.Reset()
	e.expanding.Store(true)
	e.mu.Unlock()
	e.run(s, false)
}

// run performs the expansion. The caller has set expanding. afterSpace
// says a space typed after the keyword was consumed and must be put back
// after the content.
//
// The keyword is deleted before the content is evaluated: evaluation may
// run scripts that move focus to another app, and the backspaces must land
// in the app where the keyword was typed. If evaluation fails, the keyword
// (and the consumed space) is pasted back, as if nothing had expanded.
// Known limitation: if evaluation itself moves focus (e.g. {shell(open -a X)}
// followed by more text), the paste lands in the newly focused app.
func (e *Expander) run(s Snippet, afterSpace bool) {
	defer e.expanding.Store(false)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("expander: expanding %q: %v", s.Keyword, r)
		}
	}()
	e.delete(utf8.RuneCountInString(s.Keyword))
	content, ok := evaluate(s)
	if !ok {
		e.paste(s.Keyword, plan(s.Keyword, afterSpace))
		return
	}
	text := plan(content, afterSpace)
	if text == "" {
		return
	}
	e.paste(s.Keyword, text)
}

// evaluate runs the snippet content and reports false if it panicked.
func evaluate(s Snippet) (content string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("expander: evaluating %q: %v", s.Keyword, r)
			content, ok = "", false
		}
	}()
	return s.Content(), true
}

// plan returns the text to paste for content once the keyword is deleted.
// After a space, which was consumed and so never reached the app, the
// space is put back after the content, unless the content is empty or
// already ends with a space.
func plan(content string, afterSpace bool) string {
	needsSpace := afterSpace && content != "" && !strings.HasSuffix(content, " ")
	if !needsSpace {
		return content
	}
	return content + " "
}
