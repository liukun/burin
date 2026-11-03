package activity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"burin/internal/field"
	"burin/internal/snippet"
)

// FileName is the timer settings file in the config directory.
const FileName = "timer.md"

// Keys of timer.md: the thresholds before the first heading, the others
// in a section.
const (
	keyEnabled = "enabled"
	keyIdle    = "idle"
	keyWork    = "work"
	keyIdleApp = "idle_app"
	keyBundle  = "bundle"
)

var sectionKeys = map[string]bool{keyIdleApp: true, keyBundle: true}

// App names an app: by its name as the Dock shows it, or with Bundle by
// its bundle identifier.
type App struct {
	Name   string
	Bundle string
}

// matches reports whether a running app of that name and bundle
// identifier is a.
func (a App) matches(name, bundle string) bool {
	if a.Bundle != "" {
		return a.Bundle == bundle
	}
	return a.Name == name
}

// Apps is a list of apps.
type Apps []App

// Match reports whether a running app of that name and bundle identifier
// is one of the list.
func (as Apps) Match(name, bundle string) bool {
	for _, a := range as {
		if a.matches(name, bundle) {
			return true
		}
	}
	return false
}

// DefaultIdleApps are the apps whose use does not end a break without a
// timer.md: checking a message while away is not coming back.
var DefaultIdleApps = Apps{{Name: "微信"}, {Name: "WeChat"}, {Name: "Slack"}}

// Settings is the result of loading timer.md.
type Settings struct {
	Config Config
	// Enabled is whether the timer runs; ":enabled: no" turns it off.
	Enabled bool
	// IdleApps are the apps whose use does not end a break.
	IdleApps Apps
	// Errors lists the values that fell back to their default, the
	// sections that were skipped, and a file that could not be read.
	Errors []error
	// Defaulted is set when the file is missing or could not be read or
	// parsed as a whole, so every setting is its default.
	Defaulted bool
}

// defaultSettings are the settings without a usable timer.md, with the
// error that made it so, if any.
func defaultSettings(err error) Settings {
	s := Settings{Config: DefaultConfig, Enabled: true, IdleApps: DefaultIdleApps, Defaulted: true}
	if err != nil {
		s.Errors = []error{err}
	}
	return s
}

// Load reads timer.md at path. A missing file, or one that cannot be
// read, has the default settings.
func Load(path string) Settings {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return defaultSettings(nil)
	}
	if err != nil {
		return defaultSettings(err)
	}
	return Parse(string(data))
}

// Parse reads a timer.md file. The ":enabled:" line before the first
// "## " heading turns the timer off with "no" or "false"; a missing or
// invalid one leaves it on. The ":idle:" and ":work:" lines there set
// the thresholds, Go durations; a missing or invalid one keeps its
// default. Each section names an app, by its heading or a ":bundle:"
// line, and ":idle_app: yes" (or "true") makes its use count as a
// break. A section that fails to parse is skipped and reported. A
// file that fails to parse as a whole has the default settings.
func Parse(data string) Settings {
	sections, err := snippet.Parse(data)
	if err != nil {
		return defaultSettings(err)
	}
	s := Settings{Config: DefaultConfig, IdleApps: Apps{}}
	s.Enabled = s.enabled(data)
	s.Config.Idle = s.duration(data, keyIdle, DefaultIdle)
	s.Config.Work = s.duration(data, keyWork, DefaultWork)
	for _, sec := range sections {
		app, idle, err := parseApp(sec)
		if err != nil {
			s.Errors = append(s.Errors, fmt.Errorf("## %s: %w", sec.Keyword, err))
			continue
		}
		if idle {
			s.IdleApps = append(s.IdleApps, app)
		}
	}
	return s
}

// duration reads the threshold key before the first heading, or def when
// it is missing or invalid, which is reported.
func (s *Settings) duration(data, key string, def time.Duration) time.Duration {
	value, ok, err := field.Preamble(data, key)
	if err != nil {
		s.Errors = append(s.Errors, err)
		return def
	}
	if !ok {
		return def
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		s.Errors = append(s.Errors, fmt.Errorf(":%s: %q is not a duration such as 2m", key, value))
		return def
	}
	if d <= 0 {
		s.Errors = append(s.Errors, fmt.Errorf(":%s: %q is not positive", key, value))
		return def
	}
	return d
}

// enabled reads the ":enabled:" key before the first heading, true when
// it is missing or invalid, which is reported.
func (s *Settings) enabled(data string) bool {
	enabled, err := field.PreambleBool(data, keyEnabled, true)
	if err != nil {
		s.Errors = append(s.Errors, err)
	}
	return enabled
}

// parseApp reads a section: the app it names and whether its use counts
// as a break.
func parseApp(sec snippet.Snippet) (App, bool, error) {
	values, err := field.Section(sec.Content, sectionKeys)
	if err != nil {
		return App{}, false, err
	}
	app := App{Name: sec.Keyword}
	if bundle, ok := values[keyBundle]; ok {
		if bundle == "" {
			return App{}, false, errors.New(":bundle: needs a bundle identifier")
		}
		app.Bundle = bundle
	}
	v := values[keyIdleApp]
	if v == "" {
		return app, false, nil
	}
	idle, err := field.ParseBool(keyIdleApp, v)
	if err != nil {
		return App{}, false, err
	}
	return app, idle, nil
}
