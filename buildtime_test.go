package main

import (
	"runtime/debug"
	"testing"
	"time"
)

func TestBuildStamp(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	vcs := func(modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.time", Value: "2026-10-30T11:34:04Z"},
			{Key: "vcs.modified", Value: modified},
		}}
	}
	tests := []struct {
		name      string
		buildTime string
		info      *debug.BuildInfo
		want      string
	}{
		{"linked build time wins", "2026-10-31T02:46:13Z", vcs("true"), "Built 2026-10-31 10:46"},
		{"commit time, clean tree", "", vcs("false"), "Commit 2026-10-30 19:34"},
		{"commit time, dirty tree", "", vcs("true"), "Commit 2026-10-30 19:34 (modified)"},
		{"garbage build time falls back", "yesterday", vcs("false"), "Commit 2026-10-30 19:34"},
		{"no vcs settings", "", &debug.BuildInfo{}, ""},
		{"no build info", "", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildStamp(tc.buildTime, tc.info, shanghai); got != tc.want {
				t.Errorf("buildStamp(%q) = %q, want %q", tc.buildTime, got, tc.want)
			}
		})
	}
}
