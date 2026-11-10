package main

import (
	"os"
	"path/filepath"
	"testing"

	"burin/internal/activity"
	"burin/internal/badge"
	"burin/internal/copyflash"
	"burin/internal/gesture"
	"burin/internal/inputmark"
	"burin/internal/leader"
	"burin/internal/quitter"
	"burin/internal/seed"
	"burin/internal/snippet"
	"burin/internal/windowrules"
)

// seededExamples writes the embedded examples into a fresh config dir.
func seededExamples(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "burin")
	written, err := seed.Seed(examples, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) == 0 {
		t.Fatal("no examples written")
	}
	return dir
}

func TestExamplesLoad(t *testing.T) {
	dir := seededExamples(t)

	tbl := snippet.Load(filepath.Join(dir, "snippets"))
	if len(tbl.Errors) != 0 {
		t.Errorf("snippets: %v", tbl.Errors)
	}
	keywords := map[string]bool{}
	for _, s := range tbl.Snippets {
		keywords[s.Keyword] = true
	}
	for _, k := range []string{";date", ";time", "=b64"} {
		if !keywords[k] {
			t.Errorf("snippet %q missing, have %v", k, keywords)
		}
	}

	leaders := leader.Load(filepath.Join(dir, "leaders"))
	if len(leaders.Errors) != 0 || len(leaders.Configs) != 1 {
		t.Fatalf("leaders: %d configs, errors %v", len(leaders.Configs), leaders.Errors)
	}
	if c := leaders.Configs[0]; c.Err != nil || len(c.Entries) != 6 {
		t.Errorf("leader %s: %d entries, error %v", c.Key, len(c.Entries), c.Err)
	}

	mouse := gesture.Load(filepath.Join(dir, "mouse"))
	if len(mouse.Errors) != 0 || len(mouse.Configs) != 1 {
		t.Fatalf("mouse: %d configs, errors %v", len(mouse.Configs), mouse.Errors)
	}
	if c := mouse.Configs[0]; !c.Usable() || c.Err != nil || len(c.Rules) != 3 {
		t.Errorf("mouse %s: %d rules, error %v", c.Button, len(c.Rules), c.Err)
	}

	q := quitter.Load(filepath.Join(dir, quitter.FileName))
	if len(q.Errors) != 0 || q.Config.Len() != 1 {
		t.Errorf("quitter: %d rules, errors %v", q.Config.Len(), q.Errors)
	}

	rules := windowrules.Load(filepath.Join(dir, windowrules.FileName))
	if len(rules.Errors) != 0 || rules.Config.Len() != 0 {
		t.Errorf("window rules: %d rules, errors %v", rules.Config.Len(), rules.Errors)
	}

	// The examples are the built-in defaults, written out; only the
	// idle apps differ, as WeChat's English name is a default too.
	timer := activity.Load(filepath.Join(dir, activity.FileName))
	if len(timer.Errors) != 0 || timer.Defaulted || timer.Config != activity.DefaultConfig || len(timer.IdleApps) != 2 || !timer.Enabled {
		t.Errorf("timer: %+v", timer)
	}

	badges := badge.Load(filepath.Join(dir, badge.FileName))
	if len(badges.Errors) != 0 || badges.Defaulted || len(badges.Apps) != len(badge.DefaultApps) || !badges.Enabled {
		t.Errorf("badges: %+v", badges)
	}

	clipboard := copyflash.Load(filepath.Join(dir, copyflash.FileName))
	if len(clipboard.Errors) != 0 || clipboard.Defaulted || !clipboard.Enabled {
		t.Errorf("clipboard: %+v", clipboard)
	}

	inputSources := inputmark.Load(filepath.Join(dir, inputmark.FileName))
	if len(inputSources.Errors) != 0 || inputSources.Defaulted || !inputSources.Enabled || len(inputSources.Styles) != 2 {
		t.Errorf("input sources: %+v", inputSources)
	}
}

func TestExamplesScriptExecutable(t *testing.T) {
	dir := seededExamples(t)
	info, err := os.Stat(filepath.Join(dir, "scripts", "b64.py"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("b64.py mode = %v, want executable", info.Mode().Perm())
	}
}
