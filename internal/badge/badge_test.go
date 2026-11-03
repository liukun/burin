package badge

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"burin/internal/workspace"
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

func TestParseWindows(t *testing.T) {
	data := `## Yes
:windows: yes
## True
:windows: true
## No
:windows: no
## False
:windows: false
## Unset
## Bad
:windows: maybe
## Empty
:windows:
`
	s := Parse(data)
	want := []App{{Name: "Yes", Windows: true}, {Name: "True", Windows: true}, {Name: "No"}, {Name: "False"}, {Name: "Unset"}}
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
	// running are the running apps, unanswered makes listing them fail,
	// and listed counts the listings. windows are the standard window
	// counts by pid, and windowErr makes counting fail.
	running    []workspace.App
	unanswered bool
	listed     int
	windows    map[int32]int
	windowErr  error
	counted    int
	// cycled records every raise and activation in order, as "raise 20"
	// and "activate 20", and activated receives the pid of every
	// activation, the last step of a click.
	cycled    []string
	activated chan int32
}

func newTestWatcher(d *fakeDock) *Watcher {
	w := NewWatcher()
	w.read = func() (map[string]DockIcon, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.labels, d.err
	}
	w.runningApps = func() ([]workspace.App, bool) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.listed++
		if d.unanswered {
			return nil, false
		}
		return d.running, true
	}
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
	w.windows = func(pid int32) (int, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.counted++
		return d.windows[pid], d.windowErr
	}
	w.activate = func(pid int32) bool {
		d.mu.Lock()
		d.cycled = append(d.cycled, fmt.Sprintf("activate %d", pid))
		d.mu.Unlock()
		if d.activated != nil {
			d.activated <- pid
		}
		return true
	}
	w.raise = func(pid int32) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.cycled = append(d.cycled, fmt.Sprintf("raise %d", pid))
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

func TestWatcherWindowItems(t *testing.T) {
	d := &fakeDock{
		labels:    map[string]DockIcon{"Slack": {Label: "3"}},
		running:   []workspace.App{{Name: "Slack", PID: 10}, {Name: "Notes", PID: 20}, {Name: "Mail", PID: 30}},
		windows:   map[int32]int{10: 2, 20: 3, 30: 1},
		activated: make(chan int32, 1),
	}
	w := newTestWatcher(d)
	changes := 0
	w.OnChange = func() { changes++ }
	w.SetApps([]App{{Name: "Notes", Windows: true}, {Name: "Slack", Windows: true}, {Name: "Mail", Windows: true}, {Name: "Gone", Windows: true}})
	w.poll()
	if changes != 1 {
		t.Fatalf("changes = %d after the first poll, want 1", changes)
	}
	// Notes has only its window item, in its place; Slack's follows its
	// badge; Mail with one window and Gone, not running, have none.
	items := w.Items()
	var got []string
	for _, it := range items {
		got = append(got, it.Text+" "+it.Tooltip)
	}
	want := []string{"⧉ Notes: 3 windows", "3 Slack", "⧉ Slack: 2 windows"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %q, want %q", got, want)
	}
	if items[0].Icon == nil || items[2].Icon == nil {
		t.Error("window items have no icon")
	}
	// Badges leaves out Notes, which has no badge.
	if badges := w.Badges(); len(badges) != 1 || badges[0].Name != "Slack" {
		t.Errorf("Badges = %+v, want only Slack", badges)
	}

	// A click raises the app's next window by pid, then activates the
	// app: were it activated first, the activation could undo the raise.
	items[0].OnClick()
	select {
	case <-d.activated:
	case <-time.After(time.Second):
		t.Fatal("the click activated nothing")
	}
	d.mu.Lock()
	cycled := slices.Clone(d.cycled)
	d.mu.Unlock()
	if want := []string{"raise 20", "activate 20"}; !reflect.DeepEqual(cycled, want) {
		t.Errorf("the click did %q, want %q", cycled, want)
	}

	w.poll()
	if changes != 1 {
		t.Errorf("changes = %d with the same counts, want still 1", changes)
	}
	d.mu.Lock()
	d.windows = map[int32]int{10: 2, 20: 4, 30: 1}
	d.mu.Unlock()
	w.poll()
	if changes != 2 {
		t.Errorf("changes = %d after a count changed, want 2", changes)
	}
	// Below minWindows the item goes, and so does a change of count there.
	d.mu.Lock()
	d.windows = map[int32]int{10: 2, 20: 1, 30: 0}
	d.mu.Unlock()
	w.poll()
	if changes != 3 || len(w.Items()) != 2 {
		t.Errorf("changes = %d, items = %+v after Notes dropped to one window, want 3 and two items", changes, w.Items())
	}
	// Counts changing only below minWindows change no item.
	d.mu.Lock()
	d.windows = map[int32]int{10: 2, 20: 0, 30: 1}
	d.mu.Unlock()
	w.poll()
	if changes != 3 {
		t.Errorf("changes = %d after counts changed only below %d, want still 3", changes, minWindows)
	}
}

func TestWatcherWindowErrorLoggedOnce(t *testing.T) {
	logged := captureLog(t)
	d := &fakeDock{
		running:   []workspace.App{{Name: "Slack", PID: 10}},
		windows:   map[int32]int{10: 2},
		windowErr: errors.New("no Accessibility"),
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack", Windows: true}})
	w.poll()
	w.poll()
	if got := strings.Count(logged.String(), "no Accessibility"); got != 1 {
		t.Fatalf("logged %q after two failed counts, want the failure once", logged.String())
	}
	// A success forgets the failure, so its return is logged again.
	d.mu.Lock()
	d.windowErr = nil
	d.mu.Unlock()
	w.poll()
	d.mu.Lock()
	d.windowErr = errors.New("no Accessibility")
	d.mu.Unlock()
	w.poll()
	if got := strings.Count(logged.String(), "no Accessibility"); got != 2 {
		t.Errorf("logged %q after a success and a failure again, want the failure twice", logged.String())
	}
}

func TestWatcherListsRunningAppsOncePerPoll(t *testing.T) {
	d := &fakeDock{
		labels:  map[string]DockIcon{"Chat Pro": {Label: "1"}},
		running: []workspace.App{{Name: "Chat Pro", BundleID: "com.example.chat", PID: 40}, {Name: "Notes", PID: 20}},
		windows: map[int32]int{20: 2, 40: 2},
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Chat", Bundle: "com.example.chat"}, {Name: "Notes", Windows: true}})
	w.poll()
	if d.listed != 1 {
		t.Errorf("running apps listed %d times in one poll, want 1", d.listed)
	}
}

func TestRunningOfKeepsFirstDuplicate(t *testing.T) {
	r := runningOf([]workspace.App{
		{Name: "Chat", BundleID: "com.example.chat", PID: 1},
		{Name: "Chat Helper", BundleID: "com.example.chat", PID: 2},
	})
	// The name and the pid by bundle come from the same app.
	if r.names["com.example.chat"] != "Chat" || r.pidsByBundle["com.example.chat"] != 1 {
		t.Errorf("runningOf = %+v, want Chat, pid 1 for the bundle", r)
	}
}

func TestWatcherKeepsWindowsWhenRunningUnanswered(t *testing.T) {
	logged := captureLog(t)
	d := &fakeDock{
		running: []workspace.App{{Name: "Slack", PID: 10}},
		windows: map[int32]int{10: 2},
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack", Windows: true}})
	w.poll()
	d.mu.Lock()
	d.unanswered = true
	d.mu.Unlock()
	w.poll()
	w.poll()
	if items := w.Items(); len(items) != 1 || items[0].Text != windowsText {
		t.Errorf("items = %+v with the running apps unanswered, want Slack's window item kept", items)
	}
	if got := strings.Count(logged.String(), "did not answer"); got != 1 {
		t.Fatalf("logged %q after two unanswered polls, want the failure once", logged.String())
	}
	// An answer forgets the failure, so its return is logged again.
	d.mu.Lock()
	d.unanswered = false
	d.mu.Unlock()
	w.poll()
	d.mu.Lock()
	d.unanswered = true
	d.mu.Unlock()
	w.poll()
	if got := strings.Count(logged.String(), "did not answer"); got != 2 {
		t.Errorf("logged %q after an answer and a failure again, want the failure twice", logged.String())
	}
}

// captureLog sends the log to the returned buffer until the test ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	log.SetOutput(&logged)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	return &logged
}

func TestWatcherWindowsOnlyForWindowsApps(t *testing.T) {
	d := &fakeDock{
		running: []workspace.App{{Name: "Slack", PID: 10}, {Name: "Chat Pro", BundleID: "com.example.chat", PID: 40}},
		windows: map[int32]int{10: 2, 40: 2},
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack"}, {Name: "Chat", Bundle: "com.example.chat", Windows: true}})
	w.poll()
	items := w.Items()
	// Slack's windows are not counted, and Chat is found by its bundle
	// and named as it runs.
	if len(items) != 1 || items[0].Tooltip != "Chat Pro: 2 windows" {
		t.Errorf("items = %+v, want Chat Pro's window item", items)
	}
	if d.counted != 1 {
		t.Errorf("windows counted %d times, want 1", d.counted)
	}
}

func TestWatcherWindowCountFailureIsNone(t *testing.T) {
	d := &fakeDock{
		running:   []workspace.App{{Name: "Slack", PID: 10}},
		windows:   map[int32]int{10: 2},
		windowErr: errors.New("no Accessibility"),
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack", Windows: true}})
	w.poll()
	if items := w.Items(); len(items) != 0 {
		t.Errorf("items = %+v after a failed count, want none", items)
	}
}

func TestWatcherWindowsSurviveFailedDockRead(t *testing.T) {
	d := &fakeDock{
		err:     errors.New("no Dock"),
		running: []workspace.App{{Name: "Slack", PID: 10}},
		windows: map[int32]int{10: 2},
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack", Windows: true}})
	w.poll()
	if items := w.Items(); len(items) != 1 || items[0].Text != windowsText {
		t.Errorf("items = %+v after a failed Dock read, want Slack's window item", items)
	}
}

func TestWatcherStaleCountsSkipAppsWithoutWindows(t *testing.T) {
	d := &fakeDock{
		running: []workspace.App{{Name: "Slack", PID: 10}},
		windows: map[int32]int{10: 2},
	}
	w := newTestWatcher(d)
	w.SetApps([]App{{Name: "Slack", Windows: true}, {Name: "Notes", Windows: true}})
	w.poll()
	// A reload takes Slack's :windows: away while the running apps go
	// unanswered, so the counts kept from before still hold Slack's.
	d.mu.Lock()
	d.unanswered = true
	d.mu.Unlock()
	w.SetApps([]App{{Name: "Slack"}, {Name: "Notes", Windows: true}})
	w.poll()
	if items := w.Items(); len(items) != 0 {
		t.Errorf("items = %+v after Slack's :windows: was removed, want none", items)
	}
}
