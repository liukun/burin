// Package leader holds leader keys: while a trigger key is held, a HUD
// lists the entries of its file, and pressing an entry's key runs it.
package leader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Config is one leader file: the trigger key it is named after and its
// entries in file order. Err is set when the file could not be read or
// parsed; the leader still holds, and shows the error instead of entries.
type Config struct {
	Key     string
	File    string
	Entries []Entry
	Err     error

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
// headings in snippet format. A missing dir is empty.
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
		c := Config{Key: key, File: path, trigger: t}
		c.Entries, c.Err = loadFile(path)
		s.Configs = append(s.Configs, c)
	}
	return s
}

// isFile reports whether path is, or links to, a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return entries, nil
}

// Parse reads the entries of a leader file. It is snippet.Parse with the
// heading split into the entry's key, its first word, and its title, the
// rest. Keys are case-insensitive and must be unique.
func Parse(data string) ([]Entry, error) {
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
