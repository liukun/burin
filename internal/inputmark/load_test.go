package inputmark

import (
	"os"
	"path/filepath"
	"testing"
)

const abc = "com.apple.keylayout.ABC"

func TestLoadMissingFileDefaults(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), FileName))
	if !s.Enabled || !s.Defaulted || len(s.Errors) != 0 || len(s.Styles) != 0 {
		t.Fatalf("settings = %+v, want enabled, defaulted, no errors, no styles", s)
	}
}

func TestLoadReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(":enabled: no\n\n## x\n:text: X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if s.Enabled || s.Defaulted || len(s.Errors) != 0 || s.Styles["x"].Text != "X" {
		t.Fatalf("settings = %+v, want disabled, not defaulted, no errors, x styled", s)
	}
}

func TestParseEnabled(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		enabled bool
		errors  int
	}{
		{"empty", "", true, 0},
		{"no", ":enabled: no\n", false, 0},
		{"yes", ":enabled: yes\n", true, 0},
		{"bad value", ":enabled: maybe\n", true, 1},
		{"after a heading", "## x\n:enabled: no\n", true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Parse(tt.data)
			if s.Enabled != tt.enabled || len(s.Errors) != tt.errors || s.Defaulted {
				t.Errorf("Parse(%q) = %+v, want enabled %v, %d errors", tt.data, s, tt.enabled, tt.errors)
			}
		})
	}
}

func TestParseSections(t *testing.T) {
	data := `:enabled: yes

## com.apple.keylayout.ABC
:text: A
:color: #ffffff

## com.tencent.inputmethod.wetype.pinyin
:text: 微
:color: #1ab31a80

## shown
:show: yes
`
	s := Parse(data)
	if len(s.Errors) != 0 {
		t.Fatalf("errors = %v", s.Errors)
	}
	want := map[string]Style{
		abc:                                     {Text: "A", Color: White, Show: true},
		"com.tencent.inputmethod.wetype.pinyin": {Text: "微", Color: Color{0x1a, 0xb3, 0x1a, 0x80}, Show: true},
		"shown":                                 {Color: White, Show: true},
	}
	if len(s.Styles) != len(want) {
		t.Fatalf("styles = %+v, want %+v", s.Styles, want)
	}
	for id, w := range want {
		if got := s.Styles[id]; got != w {
			t.Errorf("%s = %+v, want %+v", id, got, w)
		}
	}
}

func TestParseBadValuesFallBack(t *testing.T) {
	tests := []struct {
		name string
		data string
		want Style
	}{
		{"bad color", "## x\n:text: X\n:color: red\n", Style{Text: "X", Color: White, Show: true}},
		{"short color", "## x\n:color: #fff\n", Style{Color: White, Show: true}},
		{"color without hash", "## x\n:color: 1ab31a\n", Style{Color: White, Show: true}},
		{"color not hex", "## x\n:color: #1ab31z\n", Style{Color: White, Show: true}},
		{"bad show", "## x\n:show: maybe\n", Style{Color: White, Show: true}},
		{"empty text", "## x\n:text:\n", Style{Color: White, Show: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Parse(tt.data)
			if len(s.Errors) != 1 {
				t.Errorf("errors = %v, want 1", s.Errors)
			}
			for _, got := range s.Styles {
				if got != tt.want {
					t.Errorf("style = %+v, want %+v", got, tt.want)
				}
			}
			if len(s.Styles) != 1 {
				t.Errorf("styles = %+v, want one", s.Styles)
			}
		})
	}
}

func TestParseSkipsBrokenSections(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		errors int
	}{
		{"unknown key", "## x\n:size: 9\n", 1},
		{"duplicate heading", "## x\n:text: X\n## y\n:text: Y\n## x\n:text: Z\n", 1},
		{"repeated key", "## x\n:text: X\n:text: Y\n", 1},
		{"duplicate of a skipped section", "## x\n:size: 9\n## x\n:text: Y\n", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Parse(tt.data)
			if len(s.Errors) != tt.errors {
				t.Errorf("errors = %v, want %d", s.Errors, tt.errors)
			}
			if got, ok := s.Styles["x"]; ok && got.Text != "X" {
				t.Errorf("x = %+v, want skipped or the first", got)
			}
		})
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		in   string
		want Color
		ok   bool
	}{
		{"#ffffff", White, true},
		{"#1AB31A", Color{0x1a, 0xb3, 0x1a, 0xff}, true},
		{"#00000080", Color{0, 0, 0, 0x80}, true},
		{"", Color{}, false},
		{"#", Color{}, false},
		{"#fff", Color{}, false},
		{"#+fffff", Color{}, false},
		{"ffffff", Color{}, false},
	}
	for _, tt := range tests {
		got, err := parseColor(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("parseColor(%q) = %+v, %v; want %+v, ok %v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestStyleForFallsBackToName(t *testing.T) {
	s := Parse("## styled\n:color: #ff0000\n")
	tests := []struct {
		name       string
		src        Source
		want       Style
		configured bool
	}{
		{"no section", Source{ID: "x", Name: "Pinyin - Simplified"}, Style{Text: "P", Color: White, Show: true}, false},
		{"no section, CJK name", Source{ID: "x", Name: "微信输入法"}, Style{Text: "微", Color: White, Show: true}, false},
		{"no section, no name", Source{ID: "x"}, Style{Text: "?", Color: White, Show: true}, false},
		{"ABC without section shows", Source{ID: abc, Name: "ABC"}, Style{Text: "A", Color: White, Show: true}, false},
		{"section without text", Source{ID: "styled", Name: "Dvorak"}, Style{Text: "D", Color: Color{255, 0, 0, 255}, Show: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, configured := s.StyleFor(tt.src)
			if got != tt.want || configured != tt.configured {
				t.Errorf("StyleFor(%+v) = %+v, %v; want %+v, %v", tt.src, got, configured, tt.want, tt.configured)
			}
		})
	}
}

func TestFirstRuneInvalidUTF8(t *testing.T) {
	if got := firstRune("\xff"); got != "?" {
		t.Errorf("firstRune(invalid) = %q, want ?", got)
	}
}
