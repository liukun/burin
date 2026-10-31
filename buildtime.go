package main

import (
	"runtime/debug"
	"time"

	"github.com/egoist/mygo"
)

// buildTime is the build's UTC time in RFC 3339, linked in by
// scripts/restart.sh through MYGO_LDFLAGS; a plain mygo build leaves it
// empty and the commit time stands in.
var buildTime string

// buildStamp describes when the binary was made, for the tray menu: the
// build time when linked in, else the commit time, marked "(modified)"
// when the tree had uncommitted changes, else "" when nothing is known.
// Times show in loc.
func buildStamp(buildTime string, info *debug.BuildInfo, loc *time.Location) string {
	const layout = "2006-01-02 15:04"
	if t, err := time.Parse(time.RFC3339, buildTime); err == nil {
		return "Built " + t.In(loc).Format(layout)
	}
	if info == nil {
		return ""
	}
	var vcsTime, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.time":
			vcsTime = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	t, err := time.Parse(time.RFC3339, vcsTime)
	if err != nil {
		return ""
	}
	stamp := "Commit " + t.In(loc).Format(layout)
	if modified == "true" {
		stamp += " (modified)"
	}
	return stamp
}

// buildStampItem is the greyed menu line with the build stamp, or nil
// when there is none.
func buildStampItem() *mygo.MenuItem {
	info, _ := debug.ReadBuildInfo()
	stamp := buildStamp(buildTime, info, time.Local)
	if stamp == "" {
		return nil
	}
	return &mygo.MenuItem{Label: stamp, Disabled: true}
}
