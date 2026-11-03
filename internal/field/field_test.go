package field

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		line       string
		key, value string
		ok         bool
	}{
		{line: ":hold: 200ms", key: "hold", value: "200ms", ok: true},
		{line: ":after:30m", key: "after", value: "30m", ok: true},
		{line: ":app:", key: "app", value: "", ok: true},
		{line: ":app:   \t", key: "app", value: "", ok: true},
		{line: ":path: ~/Apps/Foo.app  \n", key: "path", value: "~/Apps/Foo.app", ok: true},
		{line: ":title: ^a:b: c$", key: "title", value: "^a:b: c$", ok: true},
		{line: ":width_min2: 0.5", key: "width_min2", value: "0.5", ok: true},
		{line: ":_x: y", key: "_x", value: "y", ok: true},
		{line: ":mainTitle: Inbox", key: "mainTitle", value: "Inbox", ok: true},
		{line: "hold: 200ms"},
		{line: "注意: 不要这样写"},
		{line: ":)"},
		{line: "::"},
		{line: ":: value"},
		{line: " :hold: 200ms"},
		{line: ":2x: y"},
		{line: ":a b: c"},
		{line: ":hold 200ms"},
		{line: ""},
	}
	for _, tt := range tests {
		key, value, ok := Parse(tt.line)
		if key != tt.key || value != tt.value || ok != tt.ok {
			t.Errorf("Parse(%q) = %q, %q, %v, want %q, %q, %v", tt.line, key, value, ok, tt.key, tt.value, tt.ok)
		}
	}
}

func TestValue(t *testing.T) {
	tests := []struct {
		line, key, value string
		ok               bool
	}{
		{line: ":row: break", key: "row", value: "break", ok: true},
		{line: ":app:", key: "app", value: "", ok: true},
		{line: ":rows: break", key: "row"},
		{line: "row: break", key: "row"},
	}
	for _, tt := range tests {
		value, ok := Value(tt.line, tt.key)
		if value != tt.value || ok != tt.ok {
			t.Errorf("Value(%q, %q) = %q, %v, want %q, %v", tt.line, tt.key, value, ok, tt.value, tt.ok)
		}
	}
}

func TestPreamble(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		value   string
		ok      bool
		wantErr bool
	}{
		{name: "found", data: "my file\n  :hold: 150ms  \n## left\nx\n", value: "150ms", ok: true},
		{name: "empty value", data: ":hold:\n## left\nx\n", value: "", ok: true},
		{name: "missing", data: "hold: 1s\n:holds: 1s\n## left\nx\n"},
		{name: "no heading", data: ":hold: 1s\n", value: "1s", ok: true},
		{name: "duplicate", data: ":hold: 1s\n:hold: 2s\n## left\nx\n", wantErr: true},
		{name: "after heading", data: "## left\n:hold: 1s\n"},
		{name: "duplicate after heading", data: ":hold: 1s\n## left\n:hold: 2s\n", value: "1s", ok: true},
		{name: "CRLF", data: "comment\r\n:hold: 1s\r\n## left\r\n:hold: 2s\r\n", value: "1s", ok: true},
		{name: "other key", data: ":layout: rows\n:hold: 1s\n## left\nx\n", value: "1s", ok: true},
	}
	for _, tt := range tests {
		value, ok, err := Preamble(tt.data, "hold")
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, want error %v", tt.name, err, tt.wantErr)
			continue
		}
		if value != tt.value || ok != tt.ok {
			t.Errorf("%s: Preamble = %q, %v; want %q, %v", tt.name, value, ok, tt.value, tt.ok)
		}
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		value string
		b, ok bool
	}{
		{value: "yes", b: true, ok: true},
		{value: "true", b: true, ok: true},
		{value: "no", ok: true},
		{value: "false", ok: true},
		{value: ""},
		{value: "Yes"},
		{value: "1"},
		{value: "off"},
	}
	for _, tt := range tests {
		b, ok := Bool(tt.value)
		if b != tt.b || ok != tt.ok {
			t.Errorf("Bool(%q) = %v, %v, want %v, %v", tt.value, b, ok, tt.b, tt.ok)
		}
	}
}

func TestPreambleBool(t *testing.T) {
	tests := []struct {
		data    string
		def     bool
		want    bool
		wantErr bool
	}{
		{data: "", def: true, want: true},
		{data: ":on: no\n", def: true, want: false},
		{data: ":on: true\n", want: true},
		{data: ":on: maybe\n", def: true, want: true, wantErr: true},
		{data: ":on: no\n:on: no\n", def: true, want: true, wantErr: true},
		{data: "## x\n:on: no\n", def: true, want: true},
	}
	for _, tt := range tests {
		got, err := PreambleBool(tt.data, "on", tt.def)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("PreambleBool(%q, %v) = %v, %v; want %v, error %v", tt.data, tt.def, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestSection(t *testing.T) {
	known := map[string]bool{"after": true, "bundle": true}
	content := "a comment\n  :after: 30m\n:bundle:\n"
	values, err := Section(content, known)
	if err != nil || len(values) != 2 || values["after"] != "30m" || values["bundle"] != "" {
		t.Errorf("Section = %v, %v; want after 30m and an empty bundle", values, err)
	}
	if _, err := Section(":after: 1m\n:after: 2m\n", known); err == nil || err.Error() != "duplicate :after:" {
		t.Errorf("repeated key: err = %v, want duplicate :after:", err)
	}
	if _, err := Section(":path: x\n", known); err == nil || err.Error() != `unknown key "path"` {
		t.Errorf("unknown key: err = %v, want unknown key \"path\"", err)
	}
}

func TestSectionLists(t *testing.T) {
	known := map[string]bool{"margin": true, "except": true}
	repeatable := map[string]bool{"except": true}
	values, err := SectionLists(":margin: 8\n:except: A\n:except: B\n", known, repeatable)
	if err != nil || len(values) != 2 || len(values["except"]) != 2 || values["except"][1] != "B" || values["margin"][0] != "8" {
		t.Errorf("SectionLists = %v, %v; want margin [8] and except [A B]", values, err)
	}
	if _, err := SectionLists(":margin: 8\n:margin: 9\n", known, repeatable); err == nil || err.Error() != "duplicate :margin:" {
		t.Errorf("repeated key: err = %v, want duplicate :margin:", err)
	}
	if _, err := SectionLists(":title: x\n", known, repeatable); err == nil {
		t.Error("unknown key: err = nil, want an error")
	}
}
