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

	keycode int64
}

// DefaultHoldDelay is the hold delay of a file whose hold: line is
// invalid: a typo must not make a tap of its trigger key do nothing.
const DefaultHoldDelay = 200 * time.Millisecond

// holdLine is the preamble line that puts a leader in hold mode.
var holdLine = regexp.MustCompile(`^hold:\s*(.+)$`)

// Config is one leader file: the trigger key it is named after and its
// entries in file order. Err is set when the file could not be read or
// parsed; the leader still holds, and shows the error instead of entries.
type Config struct {
	Key     string
	File    string
	Entries []Entry
	Err     error
	// HoldDelay is how long the trigger must be held before the leader
	// shows, set by a "hold:" line in the preamble. Zero means the leader
	// shows as soon as the trigger goes down. In hold mode a shorter tap
	// types the trigger key as usual.
	HoldDelay time.Duration

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
// headings in snippet format, with an optional "hold: <duration>" line
// before the first heading. A missing dir is empty.
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
	c.Entries, c.HoldDelay, c.Err = Parse(data)
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

// Parse reads the entries of a leader file and its hold delay, zero
// without a hold: line. The entries are snippet.Parse with the heading
// split into the entry's key, its first word, and its title, the rest.
// Keys are case-insensitive and must be unique.
//
// An invalid hold: line is an error, but the hold delay is then
// DefaultHoldDelay and the entries are still returned.
func Parse(data string) ([]Entry, time.Duration, error) {
	hold, holdErr := parseHold(data)
	entries, err := parseEntries(data)
	if err != nil {
		return nil, hold, errors.Join(err, holdErr)
	}
	return entries, hold, holdErr
}

// parseHold reads the hold: line of the preamble, the lines before the
// first heading, which snippet.Parse discards. Other preamble lines are
// a comment.
func parseHold(data string) (time.Duration, error) {
	var hold time.Duration
	for line := range strings.Lines(strings.ReplaceAll(data, "\r\n", "\n")) {
		if strings.HasPrefix(line, "## ") {
			break
		}
		m := holdLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		// hold is only ever set positive, so nonzero means seen.
		if hold != 0 {
			return DefaultHoldDelay, errors.New("duplicate hold: line")
		}
		d, err := time.ParseDuration(m[1])
		if err != nil {
			return DefaultHoldDelay, fmt.Errorf("hold: %w", err)
		}
		if d <= 0 {
			return DefaultHoldDelay, fmt.Errorf("hold: %s is not positive", m[1])
		}
		hold = d
	}
	return hold, nil
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
		entries = append(entries, Entry{
			Key:     name,
			Title:   title,
			Content: s.Content,
			keycode: keycode,
		})
	}
	return entries, nil
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
