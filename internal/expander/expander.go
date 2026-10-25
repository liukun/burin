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

// Match returns the first snippet whose keyword ends text.
func Match(text string, snippets []Snippet) (Snippet, bool) {
	for _, s := range snippets {
		matches := s.Keyword != "" && strings.HasSuffix(text, s.Keyword)
		if matches {
			return s, true
		}
	}
	return Snippet{}, false
}

// ExpandFunc replaces the keyword just typed with text.
type ExpandFunc func(keyword, text string)

// Expander matches keys against snippets and calls expand once typing has
// paused for delay after a match. It is safe for concurrent use.
type Expander struct {
	snippets []Snippet
	delay    time.Duration
	expand   ExpandFunc

	mu        sync.Mutex
	buf       Buffer
	pending   *time.Timer
	gen       uint64
	expanding atomic.Bool
}

func New(snippets []Snippet, delay time.Duration, expand ExpandFunc) *Expander {
	return &Expander{snippets: snippets, delay: delay, expand: expand}
}

// HandleKey processes one keyDown. It never blocks on expansion.
func (e *Expander) HandleKey(k Key) {
	if e.expanding.Load() {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelPendingLocked()
	if !e.buf.Feed(k) {
		return
	}
	s, ok := Match(e.buf.String(), e.snippets)
	if !ok {
		return
	}
	e.buf.Reset()
	gen := e.gen
	e.pending = time.AfterFunc(e.delay, func() { e.fire(s, gen) })
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
	e.expanding.Store(true)
	e.mu.Unlock()
	defer e.expanding.Store(false)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("expander: expanding %q: %v", s.Keyword, r)
		}
	}()
	e.expand(s.Keyword, s.Content())
}
