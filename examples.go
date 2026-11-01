package main

import (
	"embed"
	"io/fs"
)

// examplesTree is the examples directory, checked in at the repository
// root for users to read and embedded here, as an internal package cannot
// embed files above its own directory.
//
//go:embed examples
var examplesTree embed.FS

// examples is the example config, rooted like ~/.config/burin. The first
// launch writes it into an empty config dir; the tray's rules file items
// write single files of it.
var examples = mustSub(examplesTree, "examples")

// mustSub returns the subtree of fsys at dir, which is known to exist.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
