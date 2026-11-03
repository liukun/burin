// Package field parses field lines, the one syntax every burin config
// format uses for its metadata: an org-mode style property line
//
//	:key: value
//
// with the key between two colons at the very start of the line. Any
// other line is not a field, and each format treats it as content or a
// comment, so prose such as "Note: ..." never reads as a key. Leader,
// mouse gesture, quitter, window rule, timer and badge files use it;
// snippet files have no fields today but would use the same syntax if
// they gain some.
package field

import (
	"fmt"
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

// Preamble returns the value of the field named key among the lines of
// data before its first "## " heading, and whether there is one; more
// than one is an error. Leader and mouse files keep their file-wide
// settings there, as snippet.Parse discards those lines. Lines are
// trimmed, so they may be indented, and lines that are not a field of
// that key are a comment.
func Preamble(data, key string) (value string, ok bool, err error) {
	// TrimSpace drops a CRLF line's \r, so the data is not copied to
	// normalize line endings.
	for line := range strings.Lines(data) {
		if strings.HasPrefix(line, "## ") {
			break
		}
		v, isField := Value(strings.TrimSpace(line), key)
		if !isField {
			continue
		}
		if ok {
			return "", false, fmt.Errorf("duplicate :%s: line", key)
		}
		value, ok = v, true
	}
	return value, ok, nil
}

// PreambleBool reads the yes-or-no field named key before the first
// "## " heading of data, as Preamble and ParseBool do: def when it is
// missing, or invalid, which is the error.
func PreambleBool(data, key string, def bool) (bool, error) {
	value, ok, err := Preamble(data, key)
	if err != nil {
		return def, err
	}
	if !ok {
		return def, nil
	}
	b, err := ParseBool(key, value)
	if err != nil {
		return def, err
	}
	return b, nil
}

// ParseBool reads the value of the yes-or-no field named key, an error
// unless Bool takes it.
func ParseBool(key, value string) (bool, error) {
	b, ok := Bool(value)
	if !ok {
		return false, fmt.Errorf(":%s: %q is not yes, no, true or false", key, value)
	}
	return b, nil
}

// Bool reads a yes-or-no field value: "yes" or "true" is true, "no" or
// "false" is false, and anything else is not a boolean.
func Bool(value string) (b, ok bool) {
	switch value {
	case "yes", "true":
		return true, true
	case "no", "false":
		return false, true
	}
	return false, false
}

// Section reads the fields of content, the lines of a "## " section:
// the value of each key, which must be in known, else the error, as is
// a key that repeats. Lines are trimmed, so they may be indented, and
// lines that are not a field are a comment.
func Section(content string, known map[string]bool) (map[string]string, error) {
	values := map[string]string{}
	err := eachField(content, known, func(key, value string) error {
		if _, seen := values[key]; seen {
			return fmt.Errorf("duplicate :%s:", key)
		}
		values[key] = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// SectionLists reads the fields of content as Section does, but a key
// in repeatable may repeat, so each key has every value it was given,
// in order.
func SectionLists(content string, known, repeatable map[string]bool) (map[string][]string, error) {
	values := map[string][]string{}
	err := eachField(content, known, func(key, value string) error {
		_, seen := values[key]
		if seen && !repeatable[key] {
			return fmt.Errorf("duplicate :%s:", key)
		}
		values[key] = append(values[key], value)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// eachField calls fn with each field line of content, trimmed, stopping
// at a key not in known or at fn's first error.
func eachField(content string, known map[string]bool, fn func(key, value string) error) error {
	for line := range strings.Lines(content) {
		key, value, ok := Parse(strings.TrimSpace(line))
		if !ok {
			continue
		}
		if !known[key] {
			return fmt.Errorf("unknown key %q", key)
		}
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return nil
}
