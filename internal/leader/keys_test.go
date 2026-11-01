package leader

import "testing"

func TestParseStroke(t *testing.T) {
	tests := []struct {
		spec    string
		keycode int64
		flags   uint64
	}{
		{"cmd+[", 33, flagCmd},
		{"ctrl+up", 126, flagCtrl},
		{"cmd+shift+t", 17, flagCmd | flagShift},
		{"f3", 99, 0},
		{"CMD+Shift+T", 17, flagCmd | flagShift},
		{"opt+left", 123, flagAlt},
		{"alt+fn+delete", 51, flagAlt | flagSecondaryFn},
		{" cmd + , ", 43, flagCmd},
		{"escape", 53, 0},
		{"pagedown", 121, 0},
	}
	for _, tt := range tests {
		keycode, flags, err := ParseStroke(tt.spec)
		if err != nil {
			t.Errorf("%q: %v", tt.spec, err)
			continue
		}
		if keycode != tt.keycode || flags != tt.flags {
			t.Errorf("%q = %d, %#x; want %d, %#x", tt.spec, keycode, flags, tt.keycode, tt.flags)
		}
	}
}

func TestParseStrokeErrors(t *testing.T) {
	for _, spec := range []string{"", "cmd", "cmd+", "super+a", "cmd+nokey", "+a", "cmd++a"} {
		if _, _, err := ParseStroke(spec); err == nil {
			t.Errorf("%q: no error", spec)
		}
	}
}

func TestKeycodeOfKeepsEntryKeys(t *testing.T) {
	// The stroke-only keys must not become entry keys.
	if _, ok := entryKeys["left"]; ok {
		t.Fatal("left is an entry key")
	}
	if k, ok := keycodeOf("a"); !ok || k != 0 {
		t.Fatalf("keycodeOf(a) = %d, %v", k, ok)
	}
	if k, ok := keycodeOf("up"); !ok || k != 126 {
		t.Fatalf("keycodeOf(up) = %d, %v", k, ok)
	}
}
