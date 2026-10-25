// Package placeholder expands {name}, {name(args)} and {a|b|c}
// placeholders in snippet content.
package placeholder

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// shellName is the function whose argument is one raw command line.
const shellName = "shell"

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]*$`)

// Call is one stage of a placeholder pipeline.
type Call struct {
	Name string
	Args []string
}

// scanUnquoted calls visit with each byte of s outside double quotes,
// where \" inside quotes is an escaped quote, until visit returns false.
// It reports whether s ends inside a quote.
func scanUnquoted(s string, visit func(i int, c byte) bool) (unterminated bool) {
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' && i+1 < len(s) && s[i+1] == '"' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		if c == '"' {
			inQuote = true
			continue
		}
		if !visit(i, c) {
			return false
		}
	}
	return inQuote
}

// findClose returns the index in s of the '}' closing a placeholder whose
// body starts at s[0], or -1. The brace must be outside double quotes and
// parentheses.
func findClose(s string) int {
	depth := 0
	end := -1
	scanUnquoted(s, func(i int, c byte) bool {
		switch c {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '}':
			if depth == 0 {
				end = i
				return false
			}
		}
		return true
	})
	return end
}

// splitTop splits s on sep outside double quotes and, when nested is
// true, outside parentheses.
func splitTop(s string, sep byte, nested bool) ([]string, error) {
	var parts []string
	depth := 0
	start := 0
	unbalanced := false
	unterminated := scanUnquoted(s, func(i int, c byte) bool {
		atTopLevel := !nested || depth == 0
		switch {
		case nested && c == '(':
			depth++
		case nested && c == ')':
			depth--
			if depth < 0 {
				unbalanced = true
				return false
			}
		case c == sep && atTopLevel:
			parts = append(parts, s[start:i])
			start = i + 1
		}
		return true
	})
	if unbalanced {
		return nil, fmt.Errorf("unbalanced ')' in %q", s)
	}
	if unterminated {
		return nil, fmt.Errorf("unterminated quote in %q", s)
	}
	if nested && depth != 0 {
		return nil, fmt.Errorf("unbalanced '(' in %q", s)
	}
	return append(parts, s[start:]), nil
}

// Parse parses a placeholder body, the text between the braces, into its
// pipeline stages.
func Parse(body string) ([]Call, error) {
	stages, err := splitTop(body, '|', true)
	if err != nil {
		return nil, err
	}
	calls := make([]Call, 0, len(stages))
	for _, stage := range stages {
		call, err := parseCall(strings.TrimSpace(stage))
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, nil
}

func parseCall(s string) (Call, error) {
	name, rest, hasArgs := strings.Cut(s, "(")
	name = strings.TrimSpace(name)
	if !nameRE.MatchString(name) {
		return Call{}, fmt.Errorf("invalid name %q", name)
	}
	if !hasArgs {
		return Call{Name: name}, nil
	}
	inner, closed := strings.CutSuffix(rest, ")")
	if !closed {
		return Call{}, fmt.Errorf("%s: missing ')'", name)
	}
	if name == shellName {
		return Call{Name: name, Args: []string{parseRaw(inner)}}, nil
	}
	args, err := parseArgs(inner)
	if err != nil {
		return Call{}, fmt.Errorf("%s: %w", name, err)
	}
	return Call{Name: name, Args: args}, nil
}

// parseRaw returns the whole argument of shell as one command, unquoted
// when it is a single quoted string. Commas and pipes stay as they are.
func parseRaw(inner string) string {
	cmd := strings.TrimSpace(inner)
	unquoted, err := unquote(cmd)
	if err != nil {
		// Quotes that only cover part of the command belong to it.
		return cmd
	}
	return unquoted
}

// parseArgs splits a comma-separated argument list. Each argument is
// trimmed, and unquoted when it is a quoted string.
func parseArgs(inner string) ([]string, error) {
	if strings.TrimSpace(inner) == "" {
		return nil, nil
	}
	parts, err := splitTop(inner, ',', false)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, len(parts))
	for _, p := range parts {
		arg, err := unquote(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

// unquote strips the double quotes around s, if any, turning \" into ".
// A quoted string must make up all of s.
func unquote(s string) (string, error) {
	if !strings.HasPrefix(s, `"`) {
		return s, nil
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) && s[i+1] == '"' {
			b.WriteByte('"')
			i++
			continue
		}
		if c != '"' {
			b.WriteByte(c)
			continue
		}
		if i != len(s)-1 {
			return "", fmt.Errorf("text after closing quote in %s", s)
		}
		return b.String(), nil
	}
	return "", errors.New("unterminated quote in " + s)
}
