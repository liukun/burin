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
