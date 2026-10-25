package placeholder

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// listMarkerRE matches lines that start a list item, heading or quote.
var listMarkerRE = regexp.MustCompile(`^(?:[-*+#>]|\d+[.)])(?:\s|$)`)

// Unwrap strips the common indent and merges soft-wrapped lines:
//
//  1. The common indent is the smallest count of leading spaces among
//     non-blank lines after the first, skipping lines whose indent holds a
//     tab. Every line with at least that many leading spaces loses them.
//  2. Consecutive non-blank lines merge unless the later one starts with
//     a list or heading marker. Blank lines stay as paragraph breaks.
//  3. Merged lines are joined with a space when either adjacent character
//     is printable ASCII, and with nothing otherwise, so CJK stays tight.
func Unwrap(text string) string {
	trailingNewline := strings.HasSuffix(text, "\n")
	lines := splitLines(text)
	if len(lines) == 0 {
		return text
	}

	indent := commonIndent(lines[1:])
	var out []string
	for _, line := range lines {
		if indent > 0 && leadingSpaces(line) >= indent {
			line = line[indent:]
		}
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		content := strings.TrimLeft(line, " ")
		prevIsBreak := len(out) == 0 || out[len(out)-1] == ""
		startsNew := prevIsBreak || listMarkerRE.MatchString(content)
		if startsNew {
			out = append(out, line)
			continue
		}
		left := strings.TrimRightFunc(out[len(out)-1], unicode.IsSpace)
		out[len(out)-1] = left + joinSeparator(left, content) + content
	}

	result := strings.Join(out, "\n")
	if trailingNewline {
		result += "\n"
	}
	return result
}

// splitLines splits like Python's str.splitlines for \n, \r\n and \r,
// without a final empty element.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func commonIndent(lines []string) int {
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		allLead := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
		spaceLead := leadingSpaces(line)
		if allLead != spaceLead {
			continue
		}
		if indent < 0 || spaceLead < indent {
			indent = spaceLead
		}
	}
	return max(indent, 0)
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func isASCIIPrintable(r rune) bool { return r >= 0x21 && r <= 0x7e }

func joinSeparator(left, right string) string {
	l, _ := utf8.DecodeLastRuneInString(left)
	r, _ := utf8.DecodeRuneInString(right)
	if isASCIIPrintable(l) || isASCIIPrintable(r) {
		return " "
	}
	return ""
}
