package inputmark

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"burin/internal/field"
	"burin/internal/snippet"
)

// FileName is the input source indicator settings file in the config
// directory.
const FileName = "input_sources.md"

// Keys of input_sources.md: enabled before the first heading, the rest
// in a section named by an input source ID.
const (
	keyEnabled = "enabled"
	keyText    = "text"
	keyColor   = "color"
	keyShow    = "show"
)

// sectionKeys are the keys a section may have.
var sectionKeys = map[string]bool{keyText: true, keyColor: true, keyShow: true}

// fallbackText shows for an input source with neither a ":text:" nor a
// localized name.
const fallbackText = "?"

// Color is an RGBA color read from "#rrggbb" or "#rrggbbaa".
type Color struct{ R, G, B, A uint8 }

// White is the color of an input source without a ":color:".
var White = Color{255, 255, 255, 255}

// Style is how an input source shows: Text in Color, unless Show is off.
type Style struct {
	Text  string
	Color Color
	Show  bool
}

// Source is a keyboard input source: its ID, such as
// com.apple.keylayout.ABC, and its localized name.
type Source struct {
	ID   string
	Name string
}

// Settings is the result of loading input_sources.md.
type Settings struct {
	// Enabled is whether the indicator shows; ":enabled: no" turns it off.
	Enabled bool
	// Styles are the sections by input source ID. A Text left empty is
	// filled in from the source's name by StyleFor.
	Styles map[string]Style
	// Errors lists a value that fell back to its default, a section that
	// was skipped and a file that could not be read or parsed.
	Errors []error
	// Defaulted is set when the file is missing or could not be read or
	// parsed as a whole, so every setting is its default.
	Defaulted bool
}

// defaultSettings are the settings without a usable input_sources.md,
// with the error that made it so, if any.
func defaultSettings(err error) Settings {
	s := Settings{Enabled: true, Styles: map[string]Style{}, Defaulted: true}
	if err != nil {
		s.Errors = []error{err}
	}
	return s
}

// Load reads input_sources.md at path. A missing file, or one that cannot
// be read, has the default settings.
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

// Parse reads an input_sources.md file. The ":enabled:" line before the
// first "## " heading turns the indicator off with "no" or "false"; a
// missing or invalid one, which is reported, leaves it on. Each "## "
// heading is an input source ID, with ":text:", ":color:" and ":show:"
// lines; an invalid value falls back to its default and is reported, and
// a section with an unknown or repeated key, or an ID seen before, is
// skipped and reported.
func Parse(data string) Settings {
	sections, err := snippet.Parse(data)
	if err != nil {
		return defaultSettings(err)
	}
	enabled, err := field.PreambleBool(data, keyEnabled, true)
	s := Settings{Enabled: enabled, Styles: map[string]Style{}}
	if err != nil {
		s.Errors = append(s.Errors, err)
	}
	// seen holds every heading read, a skipped one included, so a later
	// section of the same ID is a duplicate either way.
	seen := map[string]bool{}
	for _, sec := range sections {
		if seen[sec.Keyword] {
			s.Errors = append(s.Errors, fmt.Errorf("## %s: duplicate section, skipped", sec.Keyword))
			continue
		}
		seen[sec.Keyword] = true
		style, errs := parseStyle(sec)
		for _, err := range errs {
			s.Errors = append(s.Errors, fmt.Errorf("## %s: %w", sec.Keyword, err))
		}
		if style == nil {
			continue
		}
		s.Styles[sec.Keyword] = *style
	}
	return s
}

// parseStyle reads a section, the style of the input source it names,
// and the errors of the values that fell back to their default. A nil
// style is a section that cannot be read at all.
func parseStyle(sec snippet.Snippet) (*Style, []error) {
	values, err := field.Section(sec.Content, sectionKeys)
	if err != nil {
		return nil, []error{err}
	}
	var errs []error
	style := defaultStyle()
	if text, ok := values[keyText]; ok {
		if text == "" {
			errs = append(errs, errors.New(":text: is empty, using the name's first letter"))
		}
		style.Text = text
	}
	if v, ok := values[keyColor]; ok {
		c, err := parseColor(v)
		if err != nil {
			errs = append(errs, err)
		} else {
			style.Color = c
		}
	}
	if v, ok := values[keyShow]; ok {
		show, err := field.ParseBool(keyShow, v)
		if err != nil {
			errs = append(errs, err)
		} else {
			style.Show = show
		}
	}
	return &style, errs
}

// parseColor reads "#rrggbb" or "#rrggbbaa".
func parseColor(v string) (Color, error) {
	bad := fmt.Errorf(":%s: %q is not #rrggbb or #rrggbbaa", keyColor, v)
	hex, hasHash := strings.CutPrefix(v, "#")
	validLength := len(hex) == 6 || len(hex) == 8
	if !hasHash || !validLength {
		return Color{}, bad
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return Color{}, bad
	}
	if len(hex) == 6 {
		n = n<<8 | 0xff
	}
	return Color{R: uint8(n >> 24), G: uint8(n >> 16), B: uint8(n >> 8), A: uint8(n)}, nil
}

// StyleFor is the style of src, and whether it has a section. A source
// without a section, or a section without a ":text:", shows the first
// letter of its name, and one without a section shows in white.
func (s Settings) StyleFor(src Source) (style Style, configured bool) {
	style, configured = s.Styles[src.ID]
	if !configured {
		style = defaultStyle()
	}
	if style.Text == "" {
		style.Text = firstRune(src.Name)
	}
	return style, configured
}

// defaultStyle is the style of an input source before its section, if
// any, says otherwise: no text yet, in white, shown.
func defaultStyle() Style {
	return Style{Color: White, Show: true}
}

// firstRune is the first character of name, or fallbackText for none.
func firstRune(name string) string {
	r, size := utf8.DecodeRuneInString(name)
	if size == 0 || r == utf8.RuneError {
		return fallbackText
	}
	return name[:size]
}
