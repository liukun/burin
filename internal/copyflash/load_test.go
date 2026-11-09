package copyflash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileDefaults(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), FileName))
	if !s.Enabled || !s.Defaulted || len(s.Errors) != 0 {
		t.Fatalf("settings = %+v, want enabled, defaulted, no errors", s)
	}
}

func TestLoadReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(":enabled: no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if s.Enabled || s.Defaulted || len(s.Errors) != 0 {
		t.Fatalf("settings = %+v, want disabled, not defaulted, no errors", s)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		enabled bool
		errors  int
	}{
		{"empty", "", true, 0},
		{"comment only", "Flash on copy.\n", true, 0},
		{"no", ":enabled: no\n", false, 0},
		{"false", ":enabled: false\n", false, 0},
		{"yes", ":enabled: yes\n", true, 0},
		{"true", ":enabled: true\n", true, 0},
		{"bad value", ":enabled: maybe\n", true, 1},
		{"duplicate", ":enabled: no\n:enabled: no\n", true, 1},
		{"after a heading", "## Later\n:enabled: no\n", true, 0},
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
