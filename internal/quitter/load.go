package quitter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"burin/internal/field"
	"burin/internal/snippet"
)

// FileName is the rules file in the config directory.
const FileName = "quitter.md"

// Set is the result of loading the rules file.
type Set struct {
	Config Config
	// Errors lists the sections that failed to parse, which are skipped,
	// and a file that could not be read.
	Errors []error
}

// Section keys.
const (
	keyAfter  = "after"
	keyAction = "action"
	keyBundle = "bundle"
	keyPath   = "path"
)

var knownKeys = map[string]bool{keyAfter: true, keyAction: true, keyBundle: true, keyPath: true}

var actions = map[string]Action{"hide": Hide, "quit": Quit, "close_window": CloseWindow}

// Load reads the rules file at path. A missing file has no rules.
func Load(path string) Set {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Set{}
		}
		return Set{Errors: []error{err}}
	}
	return Parse(string(data))
}

// Parse reads the rules of a quitter.md file. Each "## " heading starts a
// section of ":key: value" lines; every other line, and the text before
// the first heading, is a comment. A section matches the app its heading
// names, or with a :bundle: or :path: line the app with that bundle
// identifier or bundle path, and the heading only labels it. A section
// that fails to parse is skipped and reported, the others still load.
func Parse(data string) Set {
	var s Set
	sections, err := snippet.Parse(data)
	if err != nil {
		s.Errors = append(s.Errors, err)
		return s
	}
	for _, sec := range sections {
		rule, err := parseRule(sec)
		if err != nil {
			s.Errors = append(s.Errors, fmt.Errorf("## %s: %w", sec.Keyword, err))
			continue
		}
		s.Config.Rules = append(s.Config.Rules, rule)
	}
	return s
}

// sectionValues collects the key lines of a section. No key may repeat.
func sectionValues(content string) (map[string]string, error) {
	values := map[string]string{}
	for line := range strings.Lines(content) {
		key, value, ok := field.Parse(strings.TrimSpace(line))
		if !ok {
			continue
		}
		if !knownKeys[key] {
			return nil, fmt.Errorf("unknown key %q", key)
		}
		if _, seen := values[key]; seen {
			return nil, fmt.Errorf("duplicate :%s:", key)
		}
		values[key] = value
	}
	return values, nil
}

func parseRule(sec snippet.Snippet) (Rule, error) {
	values, err := sectionValues(sec.Content)
	if err != nil {
		return Rule{}, err
	}
	rule := Rule{Name: sec.Keyword}
	if rule.Match, rule.Value, err = parseMatch(sec.Keyword, values); err != nil {
		return Rule{}, err
	}
	after, ok := values[keyAfter]
	if !ok {
		return Rule{}, errors.New(":after: is required")
	}
	if rule.After, err = time.ParseDuration(after); err != nil {
		return Rule{}, fmt.Errorf(":after: %q is not a duration such as 30m", after)
	}
	if rule.After <= 0 {
		return Rule{}, fmt.Errorf(":after: %q is not positive", after)
	}
	action, ok := values[keyAction]
	if !ok {
		return rule, nil
	}
	if rule.Action, ok = actions[action]; !ok {
		return Rule{}, fmt.Errorf(":action: %q is not hide, quit or close_window", action)
	}
	return rule, nil
}

// parseMatch reads what a section matches: the :bundle: or :path: value
// when there is one, else the heading.
func parseMatch(heading string, values map[string]string) (Match, string, error) {
	bundle, hasBundle := values[keyBundle]
	path, hasPath := values[keyPath]
	if hasBundle && hasPath {
		return 0, "", errors.New(":bundle: and :path: exclude each other")
	}
	if hasBundle {
		if bundle == "" {
			return 0, "", errors.New(":bundle: needs a bundle identifier")
		}
		return ByBundle, bundle, nil
	}
	if !hasPath {
		return ByName, heading, nil
	}
	if path == "" {
		return 0, "", errors.New(":path: needs an app path")
	}
	expanded, err := expandHome(path)
	if err != nil {
		return 0, "", fmt.Errorf(":path: %w", err)
	}
	// Clean drops a trailing slash, which bundleURL.path has not.
	return ByPath, filepath.Clean(expanded), nil
}

// expandHome replaces a leading ~ of path with the home directory.
func expandHome(path string) (string, error) {
	isHome := path == "~"
	underHome := strings.HasPrefix(path, "~/")
	if !isHome && !underHome {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + path[1:], nil
}
