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
	"time"

	"burin/internal/field"
	"burin/internal/snippet"
)

// FileName is the badge apps file in the config directory.
const FileName = "badges.md"

// Keys of badges.md: enabled and the speak window before the first
// heading, the rest in a section.
const (
	keyEnabled    = "enabled"
	keySpeakFrom  = "speak_from"
	keySpeakUntil = "speak_until"
	keyBundle     = "bundle"
	keyCountOnly  = "count_only"
	keySpeak      = "speak"
)

// sectionKeys are the keys a section may have.
var sectionKeys = map[string]bool{keyBundle: true, keyCountOnly: true, keySpeak: true}

// The speak window without a badges.md, or without its keys, in minutes
// since midnight: 10:00 to 22:00.
const (
	DefaultSpeakFrom  = 10 * 60
	DefaultSpeakUntil = 22 * 60
)

// clockLayout is how the speak window's times are written, 24-hour.
const clockLayout = "15:04"

// App is an app whose badge shows: by its title in the Dock, or with
// Bundle by its bundle identifier. With CountOnly a badge without a
// count, the dot an app shows for unread activity, is not shown. With
// Speak its count is spoken during a break.
type App struct {
	Name      string
	Bundle    string
	CountOnly bool
	Speak     bool
}

// DefaultApps are the apps whose badges show without a badges.md.
var DefaultApps = []App{{Name: "Slack"}, {Name: "WhatsApp"}, {Name: "Telegram"}, {Name: "微信"}}

// Set is the result of loading badges.md.
type Set struct {
	Apps []App
	// Enabled is whether the badges show; ":enabled: no" turns them off.
	Enabled bool
	// SpeakFrom and SpeakUntil are the time of day the counts of Speak
	// apps may be spoken, in minutes since midnight: from SpeakFrom up to
	// SpeakUntil, past midnight when SpeakUntil is earlier, and never
	// when the two are the same.
	SpeakFrom, SpeakUntil int
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
	s := Set{Apps: DefaultApps, Enabled: true, SpeakFrom: DefaultSpeakFrom, SpeakUntil: DefaultSpeakUntil, Defaulted: true}
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
// badge when it is a dot and not a count, and a ":speak: yes" line speaks
// its count during a break. An ":enabled: no" (or "false") line
// before the first heading turns the badges off; a missing or invalid
// one leaves them on. ":speak_from:" and ":speak_until:" lines there set
// the speak window, the default for a missing or invalid one. A section that fails to parse is skipped and
// reported. A file that fails to parse as a whole has the
// default apps.
func Parse(data string) Set {
	sections, err := snippet.Parse(data)
	if err != nil {
		return defaultSet(err)
	}
	s := Set{Apps: []App{}}
	s.Enabled = s.enabled(data)
	s.SpeakFrom = s.clock(data, keySpeakFrom, DefaultSpeakFrom)
	s.SpeakUntil = s.clock(data, keySpeakUntil, DefaultSpeakUntil)
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

// clock reads the time of day key before the first heading, HH:MM in
// minutes since midnight, def when it is missing or invalid, which is
// reported.
func (s *Set) clock(data, key string, def int) int {
	value, ok, err := field.Preamble(data, key)
	if err != nil {
		s.Errors = append(s.Errors, err)
		return def
	}
	if !ok {
		return def
	}
	t, err := time.Parse(clockLayout, value)
	if err != nil {
		s.Errors = append(s.Errors, fmt.Errorf(":%s: %q is not a time as HH:MM", key, value))
		return def
	}
	return minuteOfDay(t)
}

// parseApp reads a section: the app it names, by bundle identifier
// with ":bundle:", whether only a count of its badge shows, and whether
// its count is spoken.
func parseApp(sec snippet.Snippet) (App, error) {
	values, err := field.Section(sec.Content, sectionKeys)
	if err != nil {
		return App{}, err
	}
	app := App{Name: sec.Keyword}
	if bundle, ok := values[keyBundle]; ok {
		if bundle == "" {
			return App{}, errors.New(":bundle: needs a bundle identifier")
		}
		app.Bundle = bundle
	}
	if app.CountOnly, err = boolField(values, keyCountOnly); err != nil {
		return App{}, err
	}
	if app.Speak, err = boolField(values, keySpeak); err != nil {
		return App{}, err
	}
	return app, nil
}

// boolField reads the yes-or-no field key of a section's values, false
// when it is missing.
func boolField(values map[string]string, key string) (bool, error) {
	v, ok := values[key]
	if !ok {
		return false, nil
	}
	return field.ParseBool(key, v)
}
