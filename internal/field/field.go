// Package field parses field lines, the one syntax every burin config
// format uses for its metadata: an org-mode style property line
//
//	:key: value
//
// with the key between two colons at the very start of the line. Any
// other line is not a field, and each format treats it as content or a
// comment, so prose such as "Note: ..." never reads as a key. Leader,
// mouse gesture, quitter and window rule files use it; snippet files have
// no fields today but would use the same syntax if they gain some.
package field

import (
	"regexp"
	"strings"
	"unicode"
)

// line is a field line. The key starts with a letter or underscore.
var line = regexp.MustCompile(`^:([A-Za-z_][A-Za-z0-9_]*):[ \t]*(.*)$`)

// Parse reports whether s is a ":key: value" line and returns its key and
// value. The value has its trailing whitespace, a line ending included,
// trimmed and may be empty. A line with leading whitespace is not a
// field; callers that allow indentation trim it first.
func Parse(s string) (key, value string, ok bool) {
	m := line.FindStringSubmatch(strings.TrimRightFunc(s, unicode.IsSpace))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// Value reports whether s is a field line with the given key and returns
// its value, as Parse does.
func Value(s, key string) (value string, ok bool) {
	k, v, ok := Parse(s)
	if !ok || k != key {
		return "", false
	}
	return v, true
}
