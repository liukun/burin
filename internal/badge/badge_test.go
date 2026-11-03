package badge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

const exampleBadges = `Badge apps: one "## " section per app whose Dock badge shows on the
bar. See docs/badges.md.

## Slack
## WhatsApp
## Telegram
## 微信
`

func TestParseExample(t *testing.T) {
	s := Parse(exampleBadges)
	if len(s.Errors) != 0 || s.Defaulted {
		t.Fatalf("errors = %v, defaulted = %v", s.Errors, s.Defaulted)
	}
	if !s.Enabled {
		t.Error("enabled = false, want true without :enabled:")
	}
	if !reflect.DeepEqual(s.Apps, DefaultApps) {
		t.Errorf("apps = %+v, want %+v", s.Apps, DefaultApps)
	}
}

func TestParseSections(t *testing.T) {
	data := `## Slack
## Chat
:bundle: com.example.chat
## Unknown
:after: 2m
## Empty bundle
:bundle:
## Twice
:bundle: a
:bundle: b
## Mail
a comment line
## Counted
:count_only: yes
## Dotted
:count_only: no
## Bad count
:count_only: maybe
## Counted twice
:count_only: yes
:count_only: yes
`
	s := Parse(data)
	want := []App{{Name: "Slack"}, {Name: "Chat", Bundle: "com.example.chat"}, {Name: "Mail"}, {Name: "Counted", CountOnly: true}, {Name: "Dotted"}}
	if !reflect.DeepEqual(s.Apps, want) {
		t.Errorf("apps = %+v, want %+v", s.Apps, want)
	}
	if len(s.Errors) != 5 {
		t.Errorf("errors = %v, want 5", s.Errors)
	}
}

func TestParseEnabled(t *testing.T) {
	tests := []struct {
		data    string
		enabled bool
		errors  int
	}{
		{data: "## Slack\n", enabled: true},
		{data: ":enabled: yes\n## Slack\n", enabled: true},
		{data: ":enabled: true\n## Slack\n", enabled: true},
		{data: ":enabled: no\n## Slack\n", enabled: false},
		{data: ":enabled: false\n## Slack\n", enabled: false},
		{data: ":enabled: maybe\n## Slack\n", enabled: true, errors: 1},
		{data: ":enabled:\n## Slack\n", enabled: true, errors: 1},
		{data: ":enabled: no\n:enabled: no\n", enabled: true, errors: 1},
	}
	for _, tt := range tests {
		s := Parse(tt.data)
		if s.Enabled != tt.enabled || len(s.Errors) != tt.errors {
			t.Errorf("Parse(%q): enabled = %v, errors %v; want %v, %d errors", tt.data, s.Enabled, s.Errors, tt.enabled, tt.errors)
		}
	}
	// The apps load either way; the caller decides what off means.
	if s := Parse(":enabled: no\n## Slack\n"); len(s.Apps) != 1 {
		t.Errorf("apps = %+v, want Slack", s.Apps)
	}
}

func TestParseSpeak(t *testing.T) {
	data := `## Yes
:speak: yes
## True
:speak: true
## No
:speak: no
## Unset
## Bad
:speak: maybe
## Twice
:speak: yes
:speak: yes
`
	s := Parse(data)
	want := []App{{Name: "Yes", Speak: true}, {Name: "True", Speak: true}, {Name: "No"}, {Name: "Unset"}}
	if !reflect.DeepEqual(s.Apps, want) {
		t.Errorf("apps = %+v, want %+v", s.Apps, want)
	}
	if len(s.Errors) != 2 {
		t.Errorf("errors = %v, want 2", s.Errors)
	}
}

func TestParseSpeakWindow(t *testing.T) {
	tests := []struct {
		data        string
		from, until int
		errors      int
	}{
		{data: "## Slack\n", from: DefaultSpeakFrom, until: DefaultSpeakUntil},
		{data: ":speak_from: 08:30\n:speak_until: 18:05\n## Slack\n", from: 8*60 + 30, until: 18*60 + 5},
		{data: ":speak_from: 07:00\n", from: 7 * 60, until: DefaultSpeakUntil},
		// A window past midnight is kept as given.
		{data: ":speak_from: 22:00\n:speak_until: 02:00\n", from: 22 * 60, until: 2 * 60},
		{data: ":speak_from: noon\n:speak_until: 25:00\n", from: DefaultSpeakFrom, until: DefaultSpeakUntil, errors: 2},
		{data: ":speak_until:\n", from: DefaultSpeakFrom, until: DefaultSpeakUntil, errors: 1},
		{data: ":speak_from: 08:00\n:speak_from: 09:00\n", from: DefaultSpeakFrom, until: DefaultSpeakUntil, errors: 1},
		// A key in a section is not the window's.
		{data: "## Slack\n:speak_from: 08:00\n", from: DefaultSpeakFrom, until: DefaultSpeakUntil, errors: 1},
	}
	for _, tt := range tests {
		s := Parse(tt.data)
		if s.SpeakFrom != tt.from || s.SpeakUntil != tt.until || len(s.Errors) != tt.errors {
			t.Errorf("Parse(%q): window %d-%d, errors %v; want %d-%d, %d errors", tt.data, s.SpeakFrom, s.SpeakUntil, s.Errors, tt.from, tt.until, tt.errors)
		}
	}
	if d := defaultSet(nil); d.SpeakFrom != DefaultSpeakFrom || d.SpeakUntil != DefaultSpeakUntil {
		t.Errorf("default window = %d-%d, want %d-%d", d.SpeakFrom, d.SpeakUntil, DefaultSpeakFrom, DefaultSpeakUntil)
	}
}

func TestParseNoSectionsHasNoApps(t *testing.T) {
	s := Parse("all gone\n")
	if len(s.Apps) != 0 || s.Defaulted {
		t.Errorf("Parse = %+v, want no apps", s)
	}
}

func TestLoadMissingOrUnparsableIsDefault(t *testing.T) {
	dir := t.TempDir()
	missing := Load(filepath.Join(dir, FileName))
	if !missing.Defaulted || len(missing.Errors) != 0 || !missing.Enabled || !reflect.DeepEqual(missing.Apps, DefaultApps) {
		t.Errorf("missing: Load = %+v, want the defaults without errors", missing)
	}
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte("## \xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := Load(path)
	if !bad.Defaulted || len(bad.Errors) != 1 || !bad.Enabled || !reflect.DeepEqual(bad.Apps, DefaultApps) {
		t.Errorf("unparsable: Load = %+v, want the defaults with one error", bad)
	}
}

func TestMatch(t *testing.T) {
	apps := []App{{Name: "Telegram"}, {Name: "Slack"}, {Name: "Chat", Bundle: "com.example.chat"}, {Name: "Mail"}, {Name: "Slack"}}
	icons := map[string]DockIcon{
		"Slack":    {Label: "•", Path: "/Applications/Slack.app"},
		"Telegram": {Label: "3"},
		"Chat Pro": {Label: "1"},
		"Mail":     {Path: "/System/Applications/Mail.app"},
		"Notes":    {Label: "2"},
	}
	bundles := map[string]string{"com.example.chat": "Chat Pro"}
	got := match(apps, icons, bundles)
	// In the order of apps; Mail has no badge, Notes is not an app, and
	// Slack shows once.
	want := []Badge{{Name: "Telegram", Label: "3"}, {Name: "Slack", Label: "•", Path: "/Applications/Slack.app"}, {Name: "Chat Pro", Label: "1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("match = %+v, want %+v", got, want)
	}
	// A count_only app shows a count, not a dot.
	counted := []App{{Name: "Slack", CountOnly: true}, {Name: "Telegram", CountOnly: true}, {Name: "Chat", CountOnly: true}}
	countIcons := map[string]DockIcon{"Slack": {Label: "•"}, "Telegram": {Label: "3"}, "Chat": {Label: "99+"}}
	wantCounted := []Badge{{Name: "Telegram", Label: "3"}, {Name: "Chat", Label: "99+"}}
	if got := match(counted, countIcons, nil); !reflect.DeepEqual(got, wantCounted) {
		t.Errorf("match of count_only apps = %+v, want %+v", got, wantCounted)
	}
	// A speak app's badge carries Speak.
	spoken := []App{{Name: "Slack", Speak: true}, {Name: "Telegram"}}
	wantSpoken := []Badge{{Name: "Slack", Label: "•", Path: "/Applications/Slack.app", Speak: true}, {Name: "Telegram", Label: "3"}}
	if got := match(spoken, icons, nil); !reflect.DeepEqual(got, wantSpoken) {
		t.Errorf("match of speak apps = %+v, want %+v", got, wantSpoken)
	}
	// A bundle with no running app matches nothing, not the app with
	// no name.
	if got := match([]App{{Name: "X", Bundle: "gone"}}, map[string]DockIcon{"": {Label: "1"}}, nil); len(got) != 0 {
		t.Errorf("match of a bundle not running = %+v, want none", got)
	}
}

// fakeDock stands in for the system.
type fakeDock struct {
	mu     sync.Mutex
	labels map[string]DockIcon
	err    error
	icons  int
	// iconFails makes drawing an icon fail.
	iconFails bool
	opened    []string
}

func newTestWatcher(d *fakeDock) *Watcher {
	w := NewWatcher()
	w.read = func() (map[string]DockIcon, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.labels, d.err
	}
	w.running = func() map[string]string { return nil }
	w.icon = func(string, string) (ui.ImageSource, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.icons++
		if d.iconFails {
			return nil, errors.New("no icon")
		}
		return &ui.Bitmap{}, nil
	}
	w.open = func(name string) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.opened = append(d.opened, name)
		return nil
	}
	return w
}

func TestWatcherReportsChanges(t *testing.T) {
	d := &fakeDock{labels: map[string]DockIcon{"Slack": {Label: "1"}, "Telegram": {}}}
	w := newTestWatcher(d)
	changes := 0
	w.OnChange = func() { changes++ }
	w.SetApps([]App{{Name: "Telegram"}, {Name: "Slack"}})

	w.poll()
	if changes != 1 {
		t.Fatalf("changes = %d after the first badge, want 1", changes)
	}
	items := w.Items()
	if len(items) != 1 || items[0].Text != "1" || items[0].Tooltip != "Slack" {
		t.Fatalf("items = %+v, want Slack's 1", items)
	}

	w.poll()
	if changes != 1 {
		t.Errorf("changes = %d with the same badges, want still 1", changes)
	}

	d.labels = map[string]DockIcon{"Slack": {Label: "2"}, "Telegram": {Label: "•"}}
	w.poll()
	if changes != 2 {
		t.Errorf("changes = %d after new badges, want 2", changes)
	}
	items = w.Items()
	if len(items) != 2 || items[0].Text != "•" || items[1].Text != "2" {
		t.Fatalf("items = %+v, want Telegram's • then Slack's 2", items)
	}
	// Each app's icon is drawn once.
	if d.icons != 2 {
		t.Errorf("icons drawn %d times, want 2", d.icons)
	}

	d.err = errors.New("no Dock")
	w.poll()
	if changes != 3 || len(w.Items()) != 0 {
		t.Errorf("changes = %d, items = %+v after a failed read, want cleared", changes, w.Items())
	}
}

func TestWatcherBadges(t *testing.T) {
	d := &fakeDock{labels: map[string]DockIcon{"Slack": {Label: "3"}, "Telegram": {Label: "•"}}}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack", Speak: true}, {Name: "Telegram"}})
	w.poll()
	got := w.Badges()
	want := []Badge{{Name: "Slack", Label: "3", Speak: true}, {Name: "Telegram", Label: "•"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Badges = %+v, want %+v", got, want)
	}
	// The badges are a copy, not the watcher's own.
	got[0].Label = "changed"
	if w.Badges()[0].Label != "3" {
		t.Error("changing the returned badges changed the watcher's")
	}
}

func TestWatcherWithoutAppsReadsNothing(t *testing.T) {
	d := &fakeDock{err: errors.New("must not be read")}
	w := newTestWatcher(d)
	w.poll()
	if len(w.Items()) != 0 {
		t.Errorf("items = %+v, want none", w.Items())
	}
}

func TestWatcherRetriesFailedIcons(t *testing.T) {
	d := &fakeDock{labels: map[string]DockIcon{"Slack": {Label: "1"}}, iconFails: true}
	w := newTestWatcher(d)
	now := time.Unix(0, 0)
	w.now = func() time.Time { return now }
	w.SetApps([]App{{Name: "Slack"}})

	w.poll()
	if items := w.Items(); len(items) != 1 || items[0].Icon != nil {
		t.Fatalf("items = %+v, want Slack without an icon", items)
	}
	changes := 0
	w.OnChange = func() { changes++ }
	d.iconFails = false

	// Before iconRetry has passed, the failed icon is not tried again.
	now = now.Add(iconRetry - time.Second)
	w.poll()
	if d.icons != 1 || changes != 0 {
		t.Fatalf("icons drawn %d times, changes = %d before the retry, want 1 and 0", d.icons, changes)
	}

	now = now.Add(time.Second)
	w.poll()
	if d.icons != 2 {
		t.Errorf("icons drawn %d times, want a retry after iconRetry", d.icons)
	}
	if changes != 1 {
		t.Errorf("changes = %d after the icon was drawn, want 1", changes)
	}
	if items := w.Items(); len(items) != 1 || items[0].Icon == nil {
		t.Errorf("items = %+v, want Slack with its icon", items)
	}

	// With the icon cached, the same badge changes nothing.
	w.poll()
	if changes != 1 || d.icons != 2 {
		t.Errorf("changes = %d, icons drawn %d times after a poll with the same badge, want 1 and 2", changes, d.icons)
	}
}
