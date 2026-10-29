package leader

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSplitsKeyAndTitle(t *testing.T) {
	entries, _, err := Parse("comment\n## k Kitty\n{shell(open -a kitty)|null}\n## L  Activity Monitor \nx\n## d\n{date}\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Key: "k", Title: "Kitty", Content: "{shell(open -a kitty)|null}", keycode: 40},
		{Key: "l", Title: "Activity Monitor", Content: "x", keycode: 37},
		{Key: "d", Title: "", Content: "{date}", keycode: 2},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries: %+v", len(entries), entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"duplicate":   "## k Kitty\na\n## K Other\nb\n",
		"unknown key": "## kk Kitty\na\n",
		"empty":       "## \na\n",
	}
	for name, data := range tests {
		if _, _, err := Parse(data); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd_r.md"), "## k Kitty\na\n")
	write(t, filepath.Join(dir, "F18.md"), "## a A\nx\n## a B\ny\n")
	write(t, filepath.Join(dir, "caps.md"), "## k Kitty\na\n")
	write(t, filepath.Join(dir, "kp_minus.md"), "## d Date\n{date}\n")
	write(t, filepath.Join(dir, "notes.txt"), "## k Kitty\na\n")
	if err := os.Mkdir(filepath.Join(dir, "opt_l.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := Load(dir)
	if len(s.Configs) != 3 {
		t.Fatalf("configs %+v", s.Configs)
	}
	f18, cmdR, kpMinus := s.Configs[0], s.Configs[1], s.Configs[2]
	if f18.Key != "f18" || f18.Err == nil || !strings.Contains(f18.Err.Error(), "duplicate") {
		t.Errorf("f18 = %+v", f18)
	}
	if cmdR.Key != "cmd_r" || cmdR.Err != nil || len(cmdR.Entries) != 1 || cmdR.trigger.keycode != 54 {
		t.Errorf("cmd_r = %+v", cmdR)
	}
	if kpMinus.Key != "kp_minus" || kpMinus.Err != nil || kpMinus.trigger.keycode != 78 || kpMinus.trigger.mask != 0 {
		t.Errorf("kp_minus = %+v", kpMinus)
	}
	if len(s.Errors) != 1 || !strings.Contains(s.Errors[0].Error(), "caps") {
		t.Errorf("errors %v", s.Errors)
	}
}

func TestLoadMissingDir(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "nope"))
	if len(s.Configs) != 0 || len(s.Errors) != 0 {
		t.Fatalf("got %+v", s)
	}
}

func TestLoadSymlinks(t *testing.T) {
	real := t.TempDir()
	other := t.TempDir()
	write(t, filepath.Join(real, "cmd_r.md"), "## k Kitty\na\n")
	write(t, filepath.Join(other, "shared.md"), "## b Chrome\nb\n")
	if err := os.Symlink(filepath.Join(other, "shared.md"), filepath.Join(real, "opt_r.md")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "leaders")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	s := Load(link)
	if len(s.Configs) != 2 || len(s.Errors) != 0 {
		t.Fatalf("got %+v", s)
	}
	if s.Configs[0].Key != "cmd_r" || s.Configs[1].Key != "opt_r" || s.Configs[1].Entries[0].Title != "Chrome" {
		t.Fatalf("configs %+v", s.Configs)
	}
}

func TestParseHold(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    time.Duration
		wantErr bool
	}{
		{"set", "hold: 200ms\n## k Kitty\na\n", 200 * time.Millisecond, false},
		{"seconds", "  hold:0.5s  \n## k Kitty\na\n", 500 * time.Millisecond, false},
		{"missing", "## k Kitty\na\n", 0, false},
		{"comment", "my leader\nholds: 1s\nhold:\n## k Kitty\na\n", 0, false},
		{"after heading", "## k Kitty\nhold: 1s\n", 0, false},
		{"invalid", "hold: soon\n## k Kitty\na\n", DefaultHoldDelay, true},
		{"zero", "hold: 0s\n## k Kitty\na\n", DefaultHoldDelay, true},
		{"negative", "hold: -1s\n## k Kitty\na\n", DefaultHoldDelay, true},
		{"duplicate", "hold: 1s\nhold: 2s\n## k Kitty\na\n", DefaultHoldDelay, true},
	}
	for _, tt := range tests {
		entries, hold, err := Parse(tt.data)
		if hold != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("%s: hold %v err %v, want %v err %v", tt.name, hold, err, tt.want, tt.wantErr)
		}
		hasEntry := len(entries) == 1 && entries[0].Key == "k"
		if !hasEntry && tt.name != "after heading" {
			t.Errorf("%s: entries %+v", tt.name, entries)
		}
	}
}

func TestParseConfigHold(t *testing.T) {
	tab := parseConfig("tab", triggers["tab"], "hold: 200ms\n## k Kitty\na\n")
	if tab.Err != nil || tab.HoldDelay != 200*time.Millisecond || tab.trigger.keycode != 48 {
		t.Errorf("tab = %+v", tab)
	}
	invalid := parseConfig("tab", triggers["tab"], "hold: 0\n## k Kitty\na\n")
	if invalid.Err == nil || invalid.HoldDelay != DefaultHoldDelay {
		t.Errorf("invalid = %+v", invalid)
	}
	mod := parseConfig("cmd_r", triggers["cmd_r"], "hold: 200ms\n## k Kitty\na\n")
	if mod.Err == nil || mod.HoldDelay != 0 {
		t.Errorf("cmd_r = %+v", mod)
	}
	self := parseConfig("tab", triggers["tab"], "hold: 200ms\n## tab Self\na\n")
	if self.Err == nil || !strings.Contains(self.Err.Error(), "trigger") {
		t.Errorf("tab with ## tab = %+v", self)
	}
	other := parseConfig("f18", triggers["f18"], "## tab Tab\na\n")
	if other.Err != nil {
		t.Errorf("f18 with ## tab = %+v", other)
	}
}

func TestLoadHold(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "tab.md"), "hold: 150ms\n## k Kitty\na\n")
	s := Load(dir)
	if len(s.Configs) != 1 {
		t.Fatalf("configs %+v", s.Configs)
	}
	c := s.Configs[0]
	if c.Key != "tab" || c.Err != nil || c.HoldDelay != 150*time.Millisecond {
		t.Errorf("tab = %+v", c)
	}
	write(t, filepath.Join(dir, "tab.md"), "hold: 1s\nhold: 1s\n## k Kitty\na\n")
	c = Load(dir).Configs[0]
	if c.Err == nil || !strings.HasPrefix(c.Err.Error(), "tab.md: ") || errors.Unwrap(c.Err) == nil {
		t.Errorf("duplicate = %+v", c)
	}
}
