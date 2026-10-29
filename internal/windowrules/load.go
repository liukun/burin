package windowrules

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"burin/internal/snippet"
)

// FileName is the rules file in the config directory.
const FileName = "window_rules.md"

// Set is the result of loading the rules file.
type Set struct {
	Config Config
	// Errors lists the sections that failed to parse, which are skipped,
	// and a file that could not be read.
	Errors []error
}

// keyLine is a "key: value" line of a section; other lines are comments.
var keyLine = regexp.MustCompile(`^([A-Za-z_]+):\s*(.*)$`)

// Section keys. A section with a margin: line is a margin rule.
const (
	keyScreenW        = "screen_w"
	keyScreenH        = "screen_h"
	keyAnchor         = "anchor"
	keyWidthMin       = "width_min"
	keyWidthMax       = "width_max"
	keyHeightMin      = "height_min"
	keyHeightMax      = "height_max"
	keyTitle          = "title"
	keyMainTitle      = "mainTitle"
	keyFullScreenable = "fullScreenable"
	keyMargin         = "margin"
	keyExcept         = "except"
)

// pinKeys and marginKeys are the keys each kind of section may have.
var (
	pinKeys = map[string]bool{
		keyScreenW: true, keyScreenH: true, keyAnchor: true,
		keyWidthMin: true, keyWidthMax: true, keyHeightMin: true, keyHeightMax: true,
		keyTitle: true, keyMainTitle: true, keyFullScreenable: true,
	}
	marginKeys = map[string]bool{
		keyScreenW: true, keyScreenH: true, keyMargin: true, keyExcept: true,
	}
)

var anchors = map[string]Anchor{"topLeft": TopLeft, "bottomLeft": BottomLeft}

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

// Parse reads the rules of a window_rules.md file. Each "## " heading
// starts a section of "key: value" lines; every other line, and the text
// before the first heading, is a comment. A section with a margin: line
// is a MarginRule, any other a Rule for the app its heading names. A
// section that fails to parse is skipped and reported, the others still
// load.
func Parse(data string) Set {
	var s Set
	sections, err := snippet.Parse(data)
	if err != nil {
		s.Errors = append(s.Errors, err)
		return s
	}
	for _, sec := range sections {
		if err := s.Config.addSection(sec); err != nil {
			s.Errors = append(s.Errors, fmt.Errorf("## %s: %w", sec.Keyword, err))
		}
	}
	return s
}

// addSection parses one section into a margin rule, when it has a
// margin: key, or else a pin rule, and appends it to c.
func (c *Config) addSection(sec snippet.Snippet) error {
	values, err := sectionValues(sec.Content)
	if err != nil {
		return err
	}
	if _, isMargin := values[keyMargin]; isMargin {
		rule, err := parseMargin(values)
		if err != nil {
			return err
		}
		c.Margins = append(c.Margins, rule)
		return nil
	}
	rule, err := parsePin(sec.Keyword, values)
	if err != nil {
		return err
	}
	c.Pins = append(c.Pins, rule)
	return nil
}

// sectionValues collects the key lines of a section. Only except: may
// repeat.
func sectionValues(content string) (map[string][]string, error) {
	values := map[string][]string{}
	for line := range strings.Lines(content) {
		m := keyLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		key, value := m[1], strings.TrimSpace(m[2])
		known := pinKeys[key] || marginKeys[key]
		if !known {
			return nil, fmt.Errorf("unknown key %q", key)
		}
		_, seen := values[key]
		if seen && key != keyExcept {
			return nil, fmt.Errorf("duplicate %s:", key)
		}
		values[key] = append(values[key], value)
	}
	return values, nil
}

// checkKeys rejects keys the kind of section does not take.
func checkKeys(values map[string][]string, allowed map[string]bool, kind string) error {
	for key := range values {
		if !allowed[key] {
			return fmt.Errorf("%s: is not a key of a %s rule", key, kind)
		}
	}
	return nil
}

func parsePin(app string, values map[string][]string) (Rule, error) {
	if err := checkKeys(values, pinKeys, "pin"); err != nil {
		return Rule{}, err
	}
	rule := Rule{App: app}
	var err error
	if rule.Screen, err = optionalScreen(values); err != nil {
		return Rule{}, err
	}
	anchor, err := required(values, keyAnchor)
	if err != nil {
		return Rule{}, err
	}
	var ok bool
	if rule.Anchor, ok = anchors[anchor]; !ok {
		return Rule{}, fmt.Errorf("anchor: %q is not topLeft or bottomLeft", anchor)
	}
	if rule.Width, err = fractionRange(values, keyWidthMin, keyWidthMax); err != nil {
		return Rule{}, err
	}
	if rule.Height, err = fractionRange(values, keyHeightMin, keyHeightMax); err != nil {
		return Rule{}, err
	}
	if rule.Title, err = optionalRegexp(values, keyTitle); err != nil {
		return Rule{}, err
	}
	if rule.MainTitle, err = optionalRegexp(values, keyMainTitle); err != nil {
		return Rule{}, err
	}
	if v, ok := values[keyFullScreenable]; ok {
		if rule.FullScreenable, err = strconv.ParseBool(v[0]); err != nil {
			return Rule{}, fmt.Errorf("fullScreenable: %q is not true or false", v[0])
		}
	}
	return rule, nil
}

func parseMargin(values map[string][]string) (MarginRule, error) {
	if err := checkKeys(values, marginKeys, "margin"); err != nil {
		return MarginRule{}, err
	}
	screen, err := optionalScreen(values)
	if err != nil {
		return MarginRule{}, err
	}
	if screen == nil {
		return MarginRule{}, errors.New("screen_w: and screen_h: are required")
	}
	margin, err := number(values, keyMargin)
	if err != nil {
		return MarginRule{}, err
	}
	if margin < 0 {
		return MarginRule{}, fmt.Errorf("margin: %g is negative", margin)
	}
	rule := MarginRule{Screen: *screen, Margin: margin}
	for _, name := range values[keyExcept] {
		if name == "" {
			return MarginRule{}, errors.New("except: needs an app name")
		}
		rule.Except = append(rule.Except, name)
	}
	return rule, nil
}

// required returns the value of key, which must be there.
func required(values map[string][]string, key string) (string, error) {
	v, ok := values[key]
	if !ok {
		return "", fmt.Errorf("%s: is required", key)
	}
	return v[0], nil
}

// number returns the value of the required key as a number.
func number(values map[string][]string, key string) (float64, error) {
	s, err := required(values, key)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", key, s)
	}
	return f, nil
}

// optionalScreen reads screen_w: and screen_h:, which come together or
// not at all.
func optionalScreen(values map[string][]string) (*Size, error) {
	_, hasW := values[keyScreenW]
	_, hasH := values[keyScreenH]
	if !hasW && !hasH {
		return nil, nil
	}
	if hasW != hasH {
		return nil, errors.New("screen_w: and screen_h: go together")
	}
	w, err := number(values, keyScreenW)
	if err != nil {
		return nil, err
	}
	h, err := number(values, keyScreenH)
	if err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("screen %gx%g is not a size", w, h)
	}
	return &Size{W: w, H: h}, nil
}

// fractionRange reads a required min and max pair of fractions with
// 0 <= min <= max <= 1.
func fractionRange(values map[string][]string, minKey, maxKey string) (Range, error) {
	lo, err := number(values, minKey)
	if err != nil {
		return Range{}, err
	}
	hi, err := number(values, maxKey)
	if err != nil {
		return Range{}, err
	}
	valid := 0 <= lo && lo <= hi && hi <= 1
	if !valid {
		return Range{}, fmt.Errorf("%s: %g and %s: %g need 0 <= min <= max <= 1", minKey, lo, maxKey, hi)
	}
	return Range{Min: lo, Max: hi}, nil
}

// optionalRegexp compiles the value of key when it is there.
func optionalRegexp(values map[string][]string, key string) (*regexp.Regexp, error) {
	v, ok := values[key]
	if !ok {
		return nil, nil
	}
	re, err := regexp.Compile(v[0])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return re, nil
}
