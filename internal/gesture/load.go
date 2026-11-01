package gesture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"burin/internal/field"
	"burin/internal/snippet"
)

// Field keys. hold is a preamble field; app may start a rule's content.
const (
	keyHold = "hold"
	keyApp  = "app"
)

// errLeft rejects a file for the left button, whose clicks and drags
// select and move things everywhere.
var errLeft = errors.New("left button is not supported")

// Load reads the .md files directly in dir, in lexical order. Each file
// is named after its button (right.md, middle.md, button3.md) and holds
// "## <direction> <title>" headings in snippet format, with an optional
// ":hold: <duration>" line before the first heading; other text there is
// a comment. A missing dir is empty.
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
	seen := map[Button]string{}
	// ReadDir returns the entries sorted by name.
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		isMarkdown := strings.EqualFold(filepath.Ext(name), ".md")
		if !isMarkdown || !isFile(path) {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		b, err := ParseButton(base)
		if err != nil {
			s.Errors = append(s.Errors, fmt.Errorf("%s: %w", path, err))
			continue
		}
		// right.md and button1.md are the same button.
		if first, dup := seen[b]; dup {
			s.Errors = append(s.Errors, fmt.Errorf("%s: button %s already has %s", path, b, first))
			continue
		}
		seen[b] = name
		s.Configs = append(s.Configs, loadFile(b, path))
	}
	return s
}

// ParseButton returns the button a file is named after: right, middle or
// buttonN with N the CoreGraphics button number. The name is lowercase.
func ParseButton(name string) (Button, error) {
	switch name {
	case "right":
		return ButtonRight, nil
	case "middle":
		return ButtonMiddle, nil
	case "left":
		return 0, errLeft
	}
	digits, ok := strings.CutPrefix(name, "button")
	// ParseUint takes no sign and no empty string.
	n, err := strconv.ParseUint(digits, 10, 31)
	if !ok || err != nil {
		return 0, fmt.Errorf("%q is not right, middle or buttonN", name)
	}
	if Button(n) == ButtonLeft {
		return 0, errLeft
	}
	return Button(n), nil
}

// isFile reports whether path is, or links to, a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadFile(b Button, path string) Config {
	c := Config{Button: b, File: path, Preamble: Preamble{HoldDelay: DefaultHoldDelay}}
	data, err := os.ReadFile(path)
	if err != nil {
		c.Err = err
		return c
	}
	c.Rules, c.Preamble, c.Err = Parse(string(data))
	// Parse returns no rules only when they did not parse.
	c.holdErrOnly = c.Err != nil && c.Rules != nil
	if c.Err != nil {
		c.Err = fmt.Errorf("%s: %w", filepath.Base(path), c.Err)
	}
	return c
}

// Parse reads the rules of a mouse file and its preamble: the hold
// delay, DefaultHoldDelay without a :hold: line. The rules are
// snippet.Parse with the heading split into the direction, its first
// word, and the title, the rest. The ":app: <name>" lines at the start of
// the content, one app per line, limit the rule to those apps, by name or
// bundle ID; they are not part of the content.
//
// An invalid :hold: line is an error, but the hold delay is then
// DefaultHoldDelay and the rules are still returned, non-nil even when
// there are none. Rules that do not parse return nil rules.
func Parse(data string) ([]Rule, Preamble, error) {
	hold, holdErr := parseHold(data)
	pre := Preamble{HoldDelay: hold}
	rules, err := parseRules(data)
	if err != nil {
		return nil, pre, errors.Join(err, holdErr)
	}
	return rules, pre, holdErr
}

// parseHold reads the :hold: line of the preamble. Zero is valid: the
// compass then shows at once.
func parseHold(data string) (time.Duration, error) {
	value, ok, err := field.Preamble(data, keyHold)
	if err != nil {
		return DefaultHoldDelay, err
	}
	if !ok {
		return DefaultHoldDelay, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return DefaultHoldDelay, fmt.Errorf(":hold: %w", err)
	}
	if d < 0 {
		return DefaultHoldDelay, fmt.Errorf(":hold: %s is negative", value)
	}
	return d, nil
}

func parseRules(data string) ([]Rule, error) {
	sections, err := snippet.Parse(data)
	if err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(sections))
	for _, sec := range sections {
		word, title := splitHeading(sec.Keyword)
		dir, ok := directions[word]
		if !ok {
			return nil, fmt.Errorf("## %s: unknown direction %q, not left, right, up, down, up-left, up-right, down-left or down-right", sec.Keyword, word)
		}
		apps, content, err := parseApps(sec.Content)
		if err != nil {
			return nil, fmt.Errorf("## %s: %w", sec.Keyword, err)
		}
		rules = append(rules, Rule{Direction: dir, Title: title, Apps: apps, Content: content})
	}
	return rules, nil
}

// parseApps reads the run of :app: lines at the start of a rule's
// content, one app per line, and returns the apps and the content after
// them. The first line that is not an :app: line, a blank one included,
// ends the run; an :app: line after it is content. Names are not split
// on commas, as a comma can be part of an app's name.
func parseApps(content string) (apps []string, rest string, err error) {
	rest = dropBlankLines(content)
	for {
		line, after := cutLine(rest)
		name, ok := field.Value(line, keyApp)
		if !ok {
			break
		}
		if name == "" {
			return nil, "", errors.New("empty :app: line")
		}
		apps = append(apps, name)
		rest = after
	}
	if apps == nil {
		return nil, content, nil
	}
	return apps, dropBlankLines(rest), nil
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
func splitHeading(heading string) (word, title string) {
	i := strings.IndexFunc(heading, unicode.IsSpace)
	if i < 0 {
		return strings.ToLower(heading), ""
	}
	return strings.ToLower(heading[:i]), strings.TrimSpace(heading[i:])
}
