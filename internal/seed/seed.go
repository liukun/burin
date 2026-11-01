// Package seed writes the example config into the config directory: the
// whole tree on first launch, when the directory is missing or empty, and
// single files on request, so a tray item can open a rules file that is
// not there yet. It never overwrites a file.
package seed

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// Modes of what Seed and SeedFile write. Files under scriptsDir are
// executable, as a pipe only runs an executable script.
const (
	dirMode    = 0o755
	fileMode   = 0o644
	scriptMode = 0o755
	scriptsDir = "scripts"
)

// Seed writes every file of src under dir when dir is missing or empty,
// and returns their paths. A dir with any entry is left alone: nothing is
// written and written is nil.
func Seed(src fs.FS, dir string) (written []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, nil
	}
	err = fs.WalkDir(src, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if err := writeFile(src, dir, rel); err != nil {
			return err
		}
		written = append(written, filepath.Join(dir, filepath.FromSlash(rel)))
		return nil
	})
	return written, err
}

// SeedFile writes the file rel, a slash-separated path in src, to the
// same path under dir, creating its parent directories, unless it exists
// already. It reports whether it wrote the file.
func SeedFile(src fs.FS, dir, rel string) (written bool, err error) {
	err = writeFile(src, dir, rel)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	return err == nil, err
}

// writeFile copies the file rel of src to the same path under dir. It
// fails with fs.ErrExist rather than overwrite a file.
func writeFile(src fs.FS, dir, rel string) error {
	data, err := fs.ReadFile(src, rel)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, modeOf(rel))
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

// modeOf is the mode of the file rel: executable under scriptsDir.
func modeOf(rel string) os.FileMode {
	isScript := path.Dir(rel) == scriptsDir
	if isScript {
		return scriptMode
	}
	return fileMode
}
