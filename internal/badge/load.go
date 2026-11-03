// Package badge shows the Dock badges of chosen apps on the bar: each app
// with a badge is its icon and the badge's text, and a click activates
// it. The badges are read from the Dock's Accessibility tree, as the
// Dock is what draws them.
package badge

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"burin/internal/field"
	"burin/internal/snippet"
)

// FileName is the badge apps file in the config directory.
const FileName = "badges.md"

// Keys of badges.md: enabled before the first heading, the rest in a
// section.
const (
	keyEnabled   = "enabled"
	keyBundle    = "bundle"
	keyCountOnly = "count_only"
)

// sectionKeys are the keys a section may have.
var sectionKeys = map[string]bool{keyBundle: true, keyCountOnly: true}

// App is an app whose badge shows: by its title in the Dock, or with
// Bundle by its bundle identifier. With CountOnly a badge without a
// count, the dot an app shows for unread activity, is not shown.
type App struct {
	Name      string
	Bundle    string
	CountOnly bool
}

// DefaultApps are the apps whose badges show without a badges.md.
var DefaultApps = []App{{Name: "Slack"}, {Name: "WhatsApp"}, {Name: "Telegram"}, {Name: "微信"}}

// Set is the result of loading badges.md.
type Set struct {
	Apps []App
	// Enabled is whether the badges show; ":enabled: no" turns them off.
	Enabled bool
	// Errors lists the sections that failed to parse, which are skipped,
	// and a file that could not be read or parsed.
	Errors []error
	// Defaulted is set when the file is missing or could not be read or
	// parsed as a whole, so the apps are the default ones.
	Defaulted bool
}

// defaultSet is the set without a usable badges.md, with the error
// that made it so, if any.
func defaultSet(err error) Set {
	s := Set{Apps: DefaultApps, Enabled: true, Defaulted: true}
	if err != nil {
		s.Errors = []error{err}
	}
	return s
}

// Load reads badges.md at path. A missing file, or one that cannot be
// read, has the default apps.
func Load(path string) Set {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return defaultSet(nil)
	}
	if err != nil {
		return defaultSet(err)
	}
	return Parse(string(data))
}

// Parse reads a badges.md file: each "## " heading names an app, in the
// order the bar shows them, as the Dock titles it, or with a ":bundle:"
// line by its bundle identifier; a ":count_only: yes" line hides its
// badge when it is a dot and not a count. An ":enabled: no" (or "false") line
// before the first heading turns the badges off; a missing or invalid
// one leaves them on. A section that fails to parse is skipped and
// reported. A file that fails to parse as a whole has the
// default apps.
func Parse(data string) Set {
	sections, err := snippet.Parse(data)
	if err != nil {
		return defaultSet(err)
	}
	s := Set{Apps: []App{}}
	s.Enabled = s.enabled(data)
	for _, sec := range sections {
		app, err := parseApp(sec)
		if err != nil {
			s.Errors = append(s.Errors, fmt.Errorf("## %s: %w", sec.Keyword, err))
			continue
		}
		s.Apps = append(s.Apps, app)
	}
	return s
}

// enabled reads the ":enabled:" key before the first heading, true when
// it is missing or invalid, which is reported.
func (s *Set) enabled(data string) bool {
	enabled, err := field.PreambleBool(data, keyEnabled, true)
	if err != nil {
		s.Errors = append(s.Errors, err)
	}
	return enabled
}

// parseApp reads a section: the app it names, by bundle identifier
// with ":bundle:", and whether only a count of its badge shows.
func parseApp(sec snippet.Snippet) (App, error) {
	values := map[string]string{}
	for line := range strings.Lines(sec.Content) {
		key, value, ok := field.Parse(strings.TrimSpace(line))
		if !ok {
			continue
		}
		if !sectionKeys[key] {
			return App{}, fmt.Errorf("unknown key %q", key)
		}
		if _, seen := values[key]; seen {
			return App{}, fmt.Errorf("duplicate :%s:", key)
		}
		values[key] = value
	}
	app := App{Name: sec.Keyword}
	if bundle, ok := values[keyBundle]; ok {
		if bundle == "" {
			return App{}, errors.New(":bundle: needs a bundle identifier")
		}
		app.Bundle = bundle
	}
	if v, ok := values[keyCountOnly]; ok {
		countOnly, err := field.ParseBool(keyCountOnly, v)
		if err != nil {
			return App{}, err
		}
		app.CountOnly = countOnly
	}
	return app, nil
}
