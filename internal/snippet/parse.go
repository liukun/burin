// Package snippet loads keyword snippets from Markdown files and watches
// their directory for changes.
package snippet

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	headingPrefix = "## "
	escapedPrefix = `\## `
)

// Snippet is one keyword and the raw content that replaces it, before
// placeholder expansion.
type Snippet struct {
	Keyword string
	Content string
	// File is the path the snippet was read from.
	File string
}

// Parse reads the snippets of one file. Each "## keyword" line starts a
// snippet whose content runs until the next "## " line, without leading
// and trailing blank lines. Text before the first heading is a comment.
// A content line starting with `\## ` stands for one starting with "## ".
// A file without headings has no snippets.
func Parse(data string) ([]Snippet, error) {
	if !utf8.ValidString(data) {
		return nil, errors.New("not valid UTF-8")
	}
	data = strings.ReplaceAll(data, "\r\n", "\n")
	var (
		out     []Snippet
		current *Snippet
		body    []string
	)
	flush := func() {
		if current == nil {
			return
		}
		current.Content = strings.Join(trimBlankLines(body), "\n")
		out = append(out, *current)
	}
	for i, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, headingPrefix) {
			flush()
			keyword := strings.TrimSpace(line[len(headingPrefix):])
			if keyword == "" {
				return nil, fmt.Errorf("line %d: empty keyword", i+1)
			}
			current = &Snippet{Keyword: keyword}
			body = nil
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, escapedPrefix) {
			line = line[1:]
		}
		body = append(body, line)
	}
	flush()
	return out, nil
}

func trimBlankLines(lines []string) []string {
	isBlank := func(s string) bool { return strings.TrimSpace(s) == "" }
	for len(lines) > 0 && isBlank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && isBlank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}
