package seed

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

var testFS = fstest.MapFS{
	"quitter.md":        {Data: []byte("## Preview\n")},
	"snippets/basic.md": {Data: []byte("## ;date\n{date}\n")},
	"scripts/b64.py":    {Data: []byte("#!/bin/sh\n")},
}

func TestSeedEmptyDir(t *testing.T) {
	for _, tt := range []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{name: "missing", dir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "a", "burin") }},
		{name: "empty", dir: func(t *testing.T) string { return t.TempDir() }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.dir(t)
			written, err := Seed(testFS, dir)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				filepath.Join(dir, "quitter.md"),
				filepath.Join(dir, "scripts", "b64.py"),
				filepath.Join(dir, "snippets", "basic.md"),
			}
			slices.Sort(written)
			if !slices.Equal(written, want) {
				t.Fatalf("written = %v, want %v", written, want)
			}
			for rel, f := range testFS {
				data, err := os.ReadFile(filepath.Join(dir, rel))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != string(f.Data) {
					t.Errorf("%s = %q, want %q", rel, data, f.Data)
				}
			}
			wantExec := map[string]bool{"quitter.md": false, "snippets/basic.md": false, "scripts/b64.py": true}
			for rel, want := range wantExec {
				info, err := os.Stat(filepath.Join(dir, rel))
				if err != nil {
					t.Fatal(err)
				}
				isExec := info.Mode().Perm()&0o100 != 0
				if isExec != want {
					t.Errorf("%s mode = %v, want executable %v", rel, info.Mode().Perm(), want)
				}
			}
		})
	}
}

func TestSeedNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := Seed(testFS, dir)
	if err != nil {
		t.Fatal(err)
	}
	if written != nil {
		t.Fatalf("written = %v, want nothing", written)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want only notes.txt", len(entries))
	}
}

func TestSeedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "burin")
	written, err := SeedFile(testFS, dir, "quitter.md")
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("missing file not written")
	}
	path := filepath.Join(dir, "quitter.md")
	if data, _ := os.ReadFile(path); string(data) != "## Preview\n" {
		t.Fatalf("quitter.md = %q", data)
	}

	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err = SeedFile(testFS, dir, "quitter.md")
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Fatal("existing file reported written")
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Fatalf("existing file changed to %q", data)
	}
}

func TestSeedFileMissingInSource(t *testing.T) {
	if _, err := SeedFile(testFS, t.TempDir(), "nope.md"); err == nil {
		t.Fatal("want an error for a file src does not have")
	}
}
