package activity

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const exampleTimer = `Timer settings: thresholds at the top, then one "## " section per app
whose use does not end a break. See docs/timer.md.

:idle: 2m
:work: 25m

## Slack
:idle_app: yes

## 微信
:idle_app: yes
`

func TestParseExample(t *testing.T) {
	s := Parse(exampleTimer)
	if len(s.Errors) != 0 || s.Defaulted {
		t.Fatalf("errors = %v, defaulted = %v", s.Errors, s.Defaulted)
	}
	if !s.Enabled {
		t.Error("enabled = false, want true without :enabled:")
	}
	if s.Config != DefaultConfig {
		t.Errorf("config = %+v, want %+v", s.Config, DefaultConfig)
	}
	want := Apps{{Name: "Slack"}, {Name: "微信"}}
	if !reflect.DeepEqual(s.IdleApps, want) {
		t.Errorf("idle apps = %+v, want %+v", s.IdleApps, want)
	}
}

func TestParseThresholds(t *testing.T) {
	s := Parse(":idle: 90s\n:work: 50m\n")
	want := Config{Idle: 90 * time.Second, Work: 50 * time.Minute}
	if s.Config != want || len(s.Errors) != 0 {
		t.Fatalf("config = %+v, errors %v; want %+v", s.Config, s.Errors, want)
	}
	// Without sections no app counts as a break: the defaults are only
	// for a missing file.
	if len(s.IdleApps) != 0 {
		t.Errorf("idle apps = %+v, want none", s.IdleApps)
	}
}

func TestParseBadValuesFallBack(t *testing.T) {
	tests := []struct {
		name string
		data string
		want Config
	}{
		{"not a duration", ":idle: soon\n:work: 30m\n", Config{Idle: DefaultIdle, Work: 30 * time.Minute}},
		{"not positive", ":idle: 1m\n:work: 0s\n", Config{Idle: time.Minute, Work: DefaultWork}},
		{"duplicate", ":idle: 1m\n:idle: 2m\n", DefaultConfig},
	}
	for _, tt := range tests {
		s := Parse(tt.data)
		if s.Config != tt.want {
			t.Errorf("%s: config = %+v, want %+v", tt.name, s.Config, tt.want)
		}
		if len(s.Errors) != 1 {
			t.Errorf("%s: errors = %v, want one", tt.name, s.Errors)
		}
	}
}

func TestParseSections(t *testing.T) {
	data := `:work: 25m

## Slack
:idle_app: yes

## Notes
:idle_app: no

## Mail
no fields: a comment

## Chat
:bundle: com.example.chat
:idle_app: yes

## Bad
:idle_app: maybe

## Unknown
:after: 2m

## Twice
:idle_app: yes
:idle_app: yes

## Empty bundle
:bundle:
:idle_app: yes
`
	s := Parse(data)
	want := Apps{{Name: "Slack"}, {Name: "Chat", Bundle: "com.example.chat"}}
	if !reflect.DeepEqual(s.IdleApps, want) {
		t.Errorf("idle apps = %+v, want %+v", s.IdleApps, want)
	}
	if len(s.Errors) != 4 {
		t.Errorf("errors = %v, want 4", s.Errors)
	}
}

func TestParseEnabled(t *testing.T) {
	tests := []struct {
		data    string
		enabled bool
		errors  int
	}{
		{data: ":work: 25m\n", enabled: true},
		{data: ":enabled: yes\n", enabled: true},
		{data: ":enabled: true\n", enabled: true},
		{data: ":enabled: no\n", enabled: false},
		{data: ":enabled: false\n", enabled: false},
		{data: ":enabled: off\n", enabled: true, errors: 1},
		{data: ":enabled:\n", enabled: true, errors: 1},
		{data: ":enabled: no\n:enabled: no\n", enabled: true, errors: 1},
		// Only before the first heading.
		{data: "## Slack\n:enabled: no\n", enabled: true, errors: 1},
	}
	for _, tt := range tests {
		s := Parse(tt.data)
		if s.Enabled != tt.enabled || len(s.Errors) != tt.errors {
			t.Errorf("Parse(%q): enabled = %v, errors %v; want %v, %d errors", tt.data, s.Enabled, s.Errors, tt.enabled, tt.errors)
		}
	}
}

func TestParseIdleAppTrueFalse(t *testing.T) {
	s := Parse("## Slack\n:idle_app: true\n\n## Notes\n:idle_app: false\n")
	want := Apps{{Name: "Slack"}}
	if !reflect.DeepEqual(s.IdleApps, want) || len(s.Errors) != 0 {
		t.Errorf("idle apps = %+v, errors %v; want %+v without errors", s.IdleApps, s.Errors, want)
	}
}

func TestAppsMatch(t *testing.T) {
	apps := Apps{{Name: "Slack"}, {Name: "Chat", Bundle: "com.example.chat"}}
	tests := []struct {
		name, bundle string
		want         bool
	}{
		{"Slack", "com.tinyspeck.slackmacgap", true},
		{"Other", "com.example.chat", true},
		// With a bundle the heading is only a label.
		{"Chat", "com.other", false},
		{"Mail", "com.apple.mail", false},
	}
	for _, tt := range tests {
		if got := apps.Match(tt.name, tt.bundle); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.name, tt.bundle, got, tt.want)
		}
	}
}

func TestLoadMissingIsDefault(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), FileName))
	if !s.Defaulted || len(s.Errors) != 0 || s.Config != DefaultConfig || !s.Enabled {
		t.Fatalf("Load = %+v, want the defaults without errors", s)
	}
	if !reflect.DeepEqual(s.IdleApps, DefaultIdleApps) {
		t.Errorf("idle apps = %+v, want %+v", s.IdleApps, DefaultIdleApps)
	}
}

func TestLoadUnparsableIsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(":work: 5m\n## \xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if !s.Defaulted || len(s.Errors) != 1 || s.Config != DefaultConfig || !s.Enabled {
		t.Fatalf("Load = %+v, want the defaults with one error", s)
	}
}
