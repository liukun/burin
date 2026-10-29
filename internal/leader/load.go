// Package leader holds leader keys: while a trigger key is held, a HUD
// lists the entries of its file, and pressing an entry's key runs it.
package leader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"burin/internal/snippet"
)

// Entry is one key of a leader and the raw content it runs, before
// placeholder expansion.
type Entry struct {
	Key     string
	Title   string
	Content string
	// RowBreak is set by a "row: break" line at the start of the entry's
	// content: in rows layout, the entry starts a new row.
	RowBreak bool

	keycode int64
}

// Layout is how the HUD arranges a leader's entries.
type Layout int

// Layouts.
const (
	// LayoutList puts the entries in columns, top to bottom.
	LayoutList Layout = iota
	// LayoutRows puts the entries in rows, left to right, starting a new
	// row at each entry with RowBreak.
	LayoutRows
)

// layouts maps the values of a layout: line to their Layout.
var layouts = map[string]Layout{"list": LayoutList, "rows": LayoutRows}

// Preamble is what the lines before the first heading set.
type Preamble struct {
	// HoldDelay is how long the trigger must be held before the leader
	// shows, set by a "hold:" line. Zero means the leader shows as soon
	// as the trigger goes down. In hold mode a shorter tap types the
	// trigger key as usual.
	HoldDelay time.Duration
	// Layout is set by a "layout:" line.
	Layout Layout
}

// DefaultHoldDelay is the hold delay of a file whose hold: line is
// invalid: a typo must not make a tap of its trigger key do nothing.
const DefaultHoldDelay = 200 * time.Millisecond

// Directive lines, matched trimmed. holdLine and layoutLine are preamble
// lines; rowLine starts an entry's content.
var (
	holdLine   = regexp.MustCompile(`^hold:\s*(.+)$`)
	layoutLine = regexp.MustCompile(`^layout:\s*(.+)$`)
	rowLine    = regexp.MustCompile(`^row:\s*(.+)$`)
)

// Config is one leader file: the trigger key it is named after and its
// entries in file order. Err is set when the file could not be read or
// parsed; the leader still holds, and shows the error instead of entries.
type Config struct {
	Key     string
	File    string
	Entries []Entry
	Err     error
	Preamble

	trigger trigger
}

// Set is the result of scanning a leaders directory.
type Set struct {
	Configs []Config
	// Errors lists the files that do not name a trigger key, and
	// directory errors. Such files are skipped.
	Errors []error
}

// Load reads the .md files directly in dir, in lexical order. Each file
// is named after its trigger key (cmd_r.md) and holds "## <key> <title>"
// headings in snippet format, with optional "hold: <duration>" and
// "layout: list|rows" lines before the first heading. A missing dir is
// empty.
func Load(dir string) Set {
	var s Set
	dir = snippet.ResolveRoot(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.Errors = append(s.Errors, err)
		}
		return s
	}
	// ReadDir returns the entries sorted by name.
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		isMarkdown := strings.EqualFold(filepath.Ext(name), ".md")
		if !isMarkdown || !isFile(path) {
			continue
		}
		key := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		t, ok := triggers[key]
		if !ok {
			s.Errors = append(s.Errors, fmt.Errorf("%s: %q is not a trigger key", path, key))
			continue
		}
		s.Configs = append(s.Configs, loadFile(key, t, path))
	}
	return s
}

// isFile reports whether path is, or links to, a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadFile(key string, t trigger, path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{Key: key, File: path, Err: err, trigger: t}
	}
	c := parseConfig(key, t, string(data))
	c.File = path
	if c.Err == nil {
		return c
	}
	c.Err = fmt.Errorf("%s: %w", filepath.Base(path), c.Err)
	return c
}

// parseConfig parses the leader file of trigger t, named key.
func parseConfig(key string, t trigger, data string) Config {
	c := Config{Key: key, trigger: t}
	c.Entries, c.Preamble, c.Err = Parse(data)
	// A modifier never reaches the app as a keystroke of its own, so
	// there is no tap to keep: the leader holds as usual.
	if t.isModifier() && c.HoldDelay > 0 {
		c.HoldDelay = 0
		c.Err = errors.Join(c.Err, errors.New("hold: is only for ordinary trigger keys, not modifiers"))
	}
	if c.Err != nil {
		return c
	}
	// The trigger's own keyDown while it is held is its auto-repeat, so
	// such an entry could never run.
	for _, e := range c.Entries {
		if e.keycode == t.keycode {
			c.Err = fmt.Errorf("## %s: the trigger key %q cannot be an entry", e.Key, key)
			return c
		}
	}
	return c
}

// Parse reads the entries of a leader file and its preamble: the hold
// delay, zero without a hold: line, and the layout, LayoutList without a
// layout: line. The entries are snippet.Parse with the heading split into
// the entry's key, its first word, and its title, the rest, and a leading
// "row: break" line taken out of the content into RowBreak. Keys are
// case-insensitive and must be unique.
//
// An invalid hold: or layout: line is an error, but the hold delay is
// then DefaultHoldDelay, the layout LayoutList, and the entries are still
// returned.
func Parse(data string) ([]Entry, Preamble, error) {
	hold, holdErr := parseHold(data)
	layout, layoutErr := parseLayout(data)
	pre := Preamble{HoldDelay: hold, Layout: layout}
	preambleErr := errors.Join(holdErr, layoutErr)
	entries, err := parseEntries(data)
	if err != nil {
		return nil, pre, errors.Join(err, preambleErr)
	}
	// With an invalid layout: line, the row: lines are likely meant for
	// rows layout: only that error is shown.
	rowsAllowed := layout == LayoutRows || layoutErr != nil
	for _, e := range entries {
		if e.RowBreak && !rowsAllowed {
			return nil, pre, errors.Join(fmt.Errorf("## %s: row: needs layout: rows", e.Key), preambleErr)
		}
	}
	return entries, pre, preambleErr
}

// preambleValue returns the value of the preamble line that matches re,
// named name, and whether there is one; more than one is an error. The
// preamble is the lines before the first heading, which snippet.Parse
// discards; lines that match no directive are a comment.
func preambleValue(data string, re *regexp.Regexp, name string) (value string, ok bool, err error) {
	for line := range strings.Lines(strings.ReplaceAll(data, "\r\n", "\n")) {
		if strings.HasPrefix(line, "## ") {
			break
		}
		m := re.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if ok {
			return "", false, fmt.Errorf("duplicate %s: line", name)
		}
		value, ok = m[1], true
	}
	return value, ok, nil
}

// parseHold reads the hold: line of the preamble.
func parseHold(data string) (time.Duration, error) {
	value, ok, err := preambleValue(data, holdLine, "hold")
	if err != nil {
		return DefaultHoldDelay, err
	}
	if !ok {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return DefaultHoldDelay, fmt.Errorf("hold: %w", err)
	}
	if d <= 0 {
		return DefaultHoldDelay, fmt.Errorf("hold: %s is not positive", value)
	}
	return d, nil
}

// parseLayout reads the layout: line of the preamble.
func parseLayout(data string) (Layout, error) {
	value, ok, err := preambleValue(data, layoutLine, "layout")
	if err != nil {
		return LayoutList, err
	}
	if !ok {
		return LayoutList, nil
	}
	layout, known := layouts[value]
	if !known {
		return LayoutList, fmt.Errorf("layout: %q is not list or rows", value)
	}
	return layout, nil
}

func parseEntries(data string) ([]Entry, error) {
	snippets, err := snippet.Parse(data)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(snippets))
	seen := map[string]bool{}
	for _, s := range snippets {
		name, title := splitHeading(s.Keyword)
		keycode, ok := entryKeys[name]
		if !ok {
			return nil, fmt.Errorf("## %s: unknown key %q", s.Keyword, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("## %s: duplicate key %q", s.Keyword, name)
		}
		seen[name] = true
		rowBreak, content, err := parseRow(s.Content)
		if err != nil {
			return nil, fmt.Errorf("## %s: %w", s.Keyword, err)
		}
		entries = append(entries, Entry{
			Key:      name,
			Title:    title,
			Content:  content,
			RowBreak: rowBreak,
			keycode:  keycode,
		})
	}
	return entries, nil
}

// parseRow reads the row: line that is the first non-blank line of an
// entry's content, and returns whether it breaks the row and the content
// after it, from its next non-blank line. A row: line further down is
// content.
func parseRow(content string) (rowBreak bool, rest string, err error) {
	first, rest := cutLine(dropBlankLines(content))
	m := rowLine.FindStringSubmatch(first)
	if m == nil {
		return false, content, nil
	}
	if m[1] != "break" {
		return false, "", fmt.Errorf("row: %q is not break", m[1])
	}
	rest = dropBlankLines(rest)
	next, _ := cutLine(rest)
	if rowLine.MatchString(next) {
		return false, "", errors.New("duplicate row: line")
	}
	return true, rest, nil
}

// cutLine returns the first line of s, trimmed, and the lines after it.
func cutLine(s string) (line, rest string) {
	line, rest, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(line), rest
}

// dropBlankLines drops the blank lines at the start of s.
func dropBlankLines(s string) string {
	for s != "" {
		line, rest := cutLine(s)
		if line != "" {
			return s
		}
		s = rest
	}
	return s
}

// splitHeading splits a heading into its first word, lowercased, and the
// rest, trimmed.
func splitHeading(heading string) (key, title string) {
	i := strings.IndexFunc(heading, unicode.IsSpace)
	if i < 0 {
		return strings.ToLower(heading), ""
	}
	return strings.ToLower(heading[:i]), strings.TrimSpace(heading[i:])
}
