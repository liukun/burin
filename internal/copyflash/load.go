package copyflash

import (
	"errors"
	"io/fs"
	"os"

	"burin/internal/field"
)

// FileName is the clipboard flash settings file in the config directory.
const FileName = "clipboard.md"

// keyEnabled turns the flash on or off, before any heading.
const keyEnabled = "enabled"

// Settings is the result of loading clipboard.md.
type Settings struct {
	// Enabled is whether copies flash; ":enabled: no" turns it off.
	Enabled bool
	// Errors lists a value that fell back to its default and a file that
	// could not be read.
	Errors []error
	// Defaulted is set when the file is missing or could not be read, so
	// every setting is its default.
	Defaulted bool
}

// defaultSettings are the settings without a usable clipboard.md, with
// the error that made it so, if any.
func defaultSettings(err error) Settings {
	s := Settings{Enabled: true, Defaulted: true}
	if err != nil {
		s.Errors = []error{err}
	}
	return s
}

// Load reads clipboard.md at path. A missing file, or one that cannot be
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

// Parse reads a clipboard.md file. The ":enabled:" line before the first
// "## " heading turns the flash off with "no" or "false"; a missing or
// invalid one, which is reported, leaves it on.
func Parse(data string) Settings {
	enabled, err := field.PreambleBool(data, keyEnabled, true)
	s := Settings{Enabled: enabled}
	if err != nil {
		s.Errors = append(s.Errors, err)
	}
	return s
}
