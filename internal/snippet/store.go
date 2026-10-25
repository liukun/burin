package snippet

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Table is the result of scanning a snippets directory.
type Table struct {
	// Snippets has one entry per keyword, in scan order.
	Snippets []Snippet
	// Errors lists the files that could not be read or parsed.
	Errors []error
}

// Load scans dir recursively for .md files in lexical order. A keyword
// defined again replaces the earlier definition. Files that fail to read
// or parse are skipped and reported in Errors. A missing dir is empty.
func Load(dir string) Table {
	var t Table
	dir = resolveRoot(dir)
	index := map[string]int{}
	fail := func(err error) {
		t.Errors = append(t.Errors, err)
		log.Printf("snippet: %v", err)
	}
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir && os.IsNotExist(err) {
				return filepath.SkipDir
			}
			fail(err)
			return nil
		}
		isMarkdown := !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".md")
		if !isMarkdown {
			return nil
		}
		snippets, err := loadFile(path)
		if err != nil {
			fail(err)
			return nil
		}
		for _, s := range snippets {
			i, seen := index[s.Keyword]
			if !seen {
				index[s.Keyword] = len(t.Snippets)
				t.Snippets = append(t.Snippets, s)
				continue
			}
			log.Printf("snippet: keyword %q in %s overrides %s", s.Keyword, s.File, t.Snippets[i].File)
			t.Snippets[i] = s
		}
		return nil
	})
	if walkErr != nil {
		fail(walkErr)
	}
	return t
}

// resolveRoot follows a symlinked root, because WalkDir does not descend
// into one. It returns dir unchanged when resolution fails, so a missing
// dir keeps its usual handling.
func resolveRoot(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

func loadFile(path string) ([]Snippet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	snippets, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range snippets {
		snippets[i].File = path
	}
	return snippets, nil
}
