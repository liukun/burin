package leader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSplitsKeyAndTitle(t *testing.T) {
	entries, err := Parse("comment\n## k Kitty\n{shell(open -a kitty)|null}\n## L  Activity Monitor \nx\n## d\n{date}\n")
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
		if _, err := Parse(data); err == nil {
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
	write(t, filepath.Join(dir, "notes.txt"), "## k Kitty\na\n")
	if err := os.Mkdir(filepath.Join(dir, "opt_l.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := Load(dir)
	if len(s.Configs) != 2 {
		t.Fatalf("configs %+v", s.Configs)
	}
	f18, cmdR := s.Configs[0], s.Configs[1]
	if f18.Key != "f18" || f18.Err == nil || !strings.Contains(f18.Err.Error(), "duplicate") {
		t.Errorf("f18 = %+v", f18)
	}
	if cmdR.Key != "cmd_r" || cmdR.Err != nil || len(cmdR.Entries) != 1 || cmdR.trigger.keycode != 54 {
		t.Errorf("cmd_r = %+v", cmdR)
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
