package badge

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"burin/internal/bar"
	"burin/internal/windowrules"
	"burin/internal/workspace"
)

const (
	// PollInterval is how often the Dock is read, as often as the timer
	// polls.
	PollInterval = 2 * time.Second
	// iconPixels is the size the app icons are drawn at: the bar's 18
	// DIPs on a Retina display.
	iconPixels = 36
	// iconRetry is how long an app whose icon could not be drawn waits
	// before the next try.
	iconRetry = time.Minute
	// minWindows is how many standard windows a Windows app needs for its
	// window item to show: with one there is nothing to cycle to.
	minWindows = 2
	// windowsText is the text of a window item.
	windowsText = "⧉"
)

// Badge is the badge of an app on the bar: its title in the Dock and
// the badge's text, "•" for a badge without a count. Inside the watcher
// it is also an app's window item: Label is empty for an app with only
// that, which Badges leaves out.
type Badge struct {
	Name  string
	Label string
	// Path is the app's bundle as the Dock knows it, empty when unknown.
	Path string
	// Speak is whether the app's count is spoken during a break.
	Speak bool
	// Windows is how many standard windows a Windows app has, kept only
	// from minWindows up, so a change below it changes nothing; PID is
	// that app's process, which a click on its window item cycles.
	Windows int
	PID     int32
}

// hasBadge reports whether b is a badge from the Dock, not only a
// window item.
func (b Badge) hasBadge() bool { return b.Label != "" }

// Watcher reads the Dock's badges of its apps every PollInterval and is
// a section of the bar: one item per app with a badge, in the order of
// its apps. Its methods are safe from any goroutine.
type Watcher struct {
	// OnChange is called, without the watcher's lock, when the badges
	// changed.
	OnChange func()

	// The system the watcher reads and acts on; tests stand in for it.
	// read returns the Dock icons by title, runningApps lists the running
	// apps, false when the main thread did not answer, icon the icon of
	// an app from its bundle path or else its name, open activates an
	// app, and now is the time. windows counts the standard windows of an
	// app by pid, activate brings an app to the front by pid, and raise
	// brings its next window to the front.
	read        func() (map[string]DockIcon, error)
	runningApps func() ([]workspace.App, bool)
	icon        func(name, path string) (ui.ImageSource, error)
	open        func(name string) error
	now         func() time.Time
	windows     func(pid int32) (int, error)
	activate    func(pid int32) bool
	raise       func(pid int32) error

	// lastRunning is the last listing of the running apps that was
	// answered, lastCounts the window counts made from it, runningErrors
	// logs the listings not answered, and windowErrors the failures to
	// count the windows of each app, a lasting failure once. Only poll
	// touches them, on its one goroutine, so they need no lock.
	lastRunning   running
	lastCounts    map[string]windowCount
	runningErrors errorLog
	windowErrors  map[string]*errorLog

	// mu guards the rest.
	mu     sync.Mutex
	apps   []App
	badges []Badge
	// icons caches the icon of every app that had a badge, so it is
	// drawn once.
	icons map[string]ui.ImageSource
	// iconFailures holds the last failure of every app whose icon could
	// not be drawn, tried again iconRetry after it.
	iconFailures map[string]iconFailure
	// readErrors logs read failures, a lasting one once.
	readErrors errorLog
	stop       chan struct{}
	stopped    bool
}

// NewWatcher returns a watcher of the Dock with no apps.
func NewWatcher() *Watcher {
	return &Watcher{
		read:          Read,
		runningApps:   workspace.RunningApps,
		icon:          appIcon,
		open:          workspace.Open,
		now:           time.Now,
		windows:       windowrules.CountStandardWindows,
		activate:      workspace.Activate,
		raise:         windowrules.RaiseNextWindow,
		icons:         map[string]ui.ImageSource{},
		iconFailures:  map[string]iconFailure{},
		readErrors:    errorLog{prefix: "badges: "},
		runningErrors: errorLog{prefix: "badges: "},
		windowErrors:  map[string]*errorLog{},
	}
}

// iconFailure is when an app's icon last failed to draw and why.
type iconFailure struct {
	at  time.Time
	err string
}

// SetApps sets the apps whose badges show, as badges.md is reloaded. The
// next poll applies them.
func (w *Watcher) SetApps(apps []App) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.apps = apps
}

// Start polls the Dock every interval, the first time at once, until
// Stop.
func (w *Watcher) Start(interval time.Duration) {
	w.mu.Lock()
	if w.stop != nil {
		w.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	w.stop = stop
	w.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		w.poll()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				w.poll()
			}
		}
	}()
}

// Stop ends the polling.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stop == nil || w.stopped {
		return
	}
	w.stopped = true
	close(w.stop)
}

// poll reads the Dock and keeps the badges of the apps, with the window
// counts of the Windows apps. A failed read clears the badges, as they
// can no longer be told apart from stale ones; the windows do not come
// from the Dock and are counted regardless. It runs on the one polling
// goroutine, or in tests one call at a time.
func (w *Watcher) poll() {
	w.mu.Lock()
	apps := w.apps
	w.mu.Unlock()
	if len(apps) == 0 {
		w.setBadges(nil, false)
		return
	}
	icons, err := w.read()
	w.logError(err)
	if err != nil {
		icons = nil
	}
	countsWindows := slices.ContainsFunc(apps, func(a App) bool { return a.Windows })
	// Without a badge in the Dock no app has one: the running apps need
	// not be listed, unless their windows are counted. One listing serves
	// both, so they agree on which app is which.
	needBundles := anyBundle(apps) && anyLabel(icons)
	needRunning := needBundles || countsWindows
	var run running
	runningOK := true
	if needRunning {
		run, runningOK = w.listRunning()
	}
	badges := match(apps, icons, run.names)
	if countsWindows {
		// Unanswered, the apps' windows cannot be counted, which is no
		// reason to take their items away: the last counts stay.
		counts := w.lastCounts
		if runningOK {
			counts = w.windowCounts(apps, run)
			w.lastCounts = counts
		}
		badges = withWindows(apps, badges, counts, icons, run.names)
	}
	// An icon drawn after an earlier failure changes the items too.
	iconAdded := w.loadIcons(badges)
	w.setBadges(badges, iconAdded)
}

// windowCount is how many standard windows the process PID of an app
// has.
type windowCount struct {
	pid   int32
	count int
}

// errRunningUnanswered is why the running apps could not be listed.
var errRunningUnanswered = errors.New("running apps: the main thread did not answer")

// listRunning lists the running apps. When the main thread does not
// answer it returns the last listing that was answered and false, and
// logs the failure, a lasting one once.
func (w *Watcher) listRunning() (running, bool) {
	list, ok := w.runningApps()
	if !ok {
		w.runningErrors.log(errRunningUnanswered)
		return w.lastRunning, false
	}
	w.runningErrors.log(nil)
	w.lastRunning = runningOf(list)
	return w.lastRunning, true
}

// windowCounts counts the standard windows of the running Windows apps
// of apps, by the name the bar shows them under, as match names them. An
// app that is not running has none, and one whose windows cannot be read
// has none either, which is logged.
func (w *Watcher) windowCounts(apps []App, run running) map[string]windowCount {
	counts := map[string]windowCount{}
	for _, a := range apps {
		if !a.Windows {
			continue
		}
		name, pid, isRunning := run.lookup(a)
		_, counted := counts[name]
		skip := name == "" || !isRunning || counted
		if skip {
			continue
		}
		n, err := w.windows(pid)
		w.logWindowError(name, err)
		if err != nil {
			n = 0
		}
		counts[name] = windowCount{pid: pid, count: n}
	}
	return counts
}

// logWindowError logs err, the failure to count the windows of the app
// named name, unless it is that app's last one logged; nil forgets it.
func (w *Watcher) logWindowError(name string, err error) {
	l, ok := w.windowErrors[name]
	if !ok {
		l = &errorLog{prefix: "badges: windows of " + name + ": "}
		w.windowErrors[name] = l
	}
	l.log(err)
}

// withWindows adds to badges, the badges of apps in their order, the
// window counts of the apps with minWindows or more by name in counts:
// on the app's badge when it has one, else as an entry without a label
// in the app's place, its path from the Dock icon by title in icons. The
// apps are named as match names them, through bundles.
func withWindows(apps []App, badges []Badge, counts map[string]windowCount, icons map[string]DockIcon, bundles map[string]string) []Badge {
	byName := make(map[string]Badge, len(badges))
	for _, b := range badges {
		byName[b.Name] = b
	}
	var merged []Badge
	seen := map[string]bool{}
	for _, a := range apps {
		name := a.barName(bundles)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		b, hasBadge := byName[name]
		// An app without Windows shows no count, so a stale one in counts,
		// from before a reload took its :windows: away, does not show.
		c := counts[name]
		showsWindows := a.Windows && c.count >= minWindows
		if !hasBadge && !showsWindows {
			continue
		}
		if !hasBadge {
			b = Badge{Name: name, Path: icons[name].Path}
		}
		if showsWindows {
			b.Windows, b.PID = c.count, c.pid
		}
		merged = append(merged, b)
	}
	return merged
}

// logError logs err unless it is the last one logged; nil forgets it.
func (w *Watcher) logError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.readErrors.log(err)
}

// errorLog logs errors with prefix, a lasting one once: an error is
// logged unless it is the last one logged, and nil forgets it. Its
// owner guards it.
type errorLog struct {
	prefix string
	last   string
}

// log logs err unless it is the last one logged; nil forgets it.
func (l *errorLog) log(err error) {
	if err == nil {
		l.last = ""
		return
	}
	if err.Error() == l.last {
		return
	}
	l.last = err.Error()
	log.Printf("%s%v", l.prefix, err)
}

// setBadges keeps badges and calls OnChange when they changed, or
// regardless with force.
func (w *Watcher) setBadges(badges []Badge, force bool) {
	w.mu.Lock()
	changed := force || !slices.Equal(w.badges, badges)
	w.badges = badges
	w.mu.Unlock()
	if changed && w.OnChange != nil {
		w.OnChange()
	}
}

// loadIcons draws the icons of the apps of badges not drawn yet, those
// that failed only once iconRetry has passed, and reports whether any was
// added. A failure is logged when it differs from the app's last one.
func (w *Watcher) loadIcons(badges []Badge) bool {
	added := false
	for _, b := range badges {
		if !w.iconDue(b.Name) {
			continue
		}
		icon, err := w.icon(b.Name, b.Path)
		if err != nil {
			w.iconFailed(b.Name, err)
			continue
		}
		w.mu.Lock()
		w.icons[b.Name] = icon
		delete(w.iconFailures, b.Name)
		w.mu.Unlock()
		added = true
	}
	return added
}

// iconDue reports whether the icon of the app named name is to be drawn:
// it is not cached, and its last failure, if any, is iconRetry old.
func (w *Watcher) iconDue(name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, cached := w.icons[name]; cached {
		return false
	}
	failure, failed := w.iconFailures[name]
	if !failed {
		return true
	}
	return w.now().Sub(failure.at) >= iconRetry
}

// iconFailed records that the icon of the app named name failed with
// err, and logs err unless it is the app's last failure again.
func (w *Watcher) iconFailed(name string, err error) {
	w.mu.Lock()
	last := w.iconFailures[name]
	w.iconFailures[name] = iconFailure{at: w.now(), err: err.Error()}
	w.mu.Unlock()
	if err.Error() == last.err {
		return
	}
	log.Printf("badges: icon of %s: %v", name, err)
}

// Badges returns the badges of the last read, in the order of the apps,
// without the apps that have only a window item.
func (w *Watcher) Badges() []Badge {
	w.mu.Lock()
	defer w.mu.Unlock()
	var badges []Badge
	for _, b := range w.badges {
		if b.hasBadge() {
			badges = append(badges, b)
		}
	}
	return badges
}

// Items makes the watcher a section of the bar: the icon and badge of
// every app with one, which a click activates, each followed by the
// app's window item when it has minWindows or more standard windows,
// which a click cycles.
func (w *Watcher) Items() []bar.Item {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := make([]bar.Item, 0, len(w.badges))
	for _, b := range w.badges {
		name := b.Name
		if b.hasBadge() {
			items = append(items, bar.Item{
				Icon:    w.icons[name],
				Text:    b.Label,
				Tooltip: name,
				// The click runs on the main thread, which open waits on.
				OnClick: func() { go w.openApp(name) },
			})
		}
		if b.Windows == 0 {
			continue
		}
		pid := b.PID
		items = append(items, bar.Item{
			Icon:    w.icons[name],
			Text:    windowsText,
			Tooltip: fmt.Sprintf("%s: %d windows", name, b.Windows),
			// The click runs on the main thread, which activation waits
			// on, and Accessibility calls must not block it.
			OnClick: func() { go w.cycle(name, pid) },
		})
	}
	return items
}

// openApp brings app name to the front.
func (w *Watcher) openApp(name string) {
	if err := w.open(name); err != nil {
		log.Printf("badges: open %s: %v", name, err)
	}
}

// cycle raises the next standard window of app name, of process pid,
// and brings the app to the front.
func (w *Watcher) cycle(name string, pid int32) {
	// The window is raised before the app is activated: activation is
	// asynchronous, and finishing after the raise it would bring the old
	// main window back to the front.
	if err := w.raise(pid); err != nil {
		log.Printf("badges: next window of %s: %v", name, err)
	}
	if !w.activate(pid) {
		log.Printf("badges: activate %s: not activated", name)
	}
}

// match returns the badges of apps, in their order, from the Dock icons
// by title in icons. An app with a bundle identifier is found by the
// name of the running app with it, in bundles; an app without a badge,
// or named twice, is left out, as is a count-only app whose badge is
// not a count.
func match(apps []App, icons map[string]DockIcon, bundles map[string]string) []Badge {
	var badges []Badge
	seen := map[string]bool{}
	for _, a := range apps {
		name := a.barName(bundles)
		icon := icons[name]
		dotHidden := a.CountOnly && !isCount(icon.Label)
		skip := name == "" || icon.Label == "" || seen[name] || dotHidden
		if skip {
			continue
		}
		seen[name] = true
		badges = append(badges, Badge{Name: name, Label: icon.Label, Path: icon.Path, Speak: a.Speak})
	}
	return badges
}

// isCount reports whether a badge's label is a count rather than a dot:
// it has a digit, as a count such as "99+" may not be all digits.
func isCount(label string) bool {
	return strings.ContainsFunc(label, unicode.IsDigit)
}

// anyBundle reports whether an app is named by its bundle identifier.
func anyBundle(apps []App) bool {
	return slices.ContainsFunc(apps, func(a App) bool { return a.Bundle != "" })
}

// anyLabel reports whether a Dock icon has a badge.
func anyLabel(icons map[string]DockIcon) bool {
	for _, icon := range icons {
		if icon.Label != "" {
			return true
		}
	}
	return false
}

// running is one listing of the running apps: their names and pids by
// bundle identifier and their pids by name. Of several apps with the same
// name or identifier, the first listed is kept, for the names and the
// pids alike, so both name the same app.
type running struct {
	names                    map[string]string
	pidsByName, pidsByBundle map[string]int32
}

// barName returns the name the bar shows app a under: its own, or with
// a bundle identifier the name of the running app with it in bundles,
// "" when none runs.
func (a App) barName(bundles map[string]string) string {
	if a.Bundle == "" {
		return a.Name
	}
	return bundles[a.Bundle]
}

// lookup returns the name app a runs under, as barName names it, and its
// pid, and whether it runs.
func (r running) lookup(a App) (name string, pid int32, ok bool) {
	if a.Bundle == "" {
		pid, ok = r.pidsByName[a.Name]
		return a.Name, pid, ok
	}
	pid, ok = r.pidsByBundle[a.Bundle]
	return r.names[a.Bundle], pid, ok
}

// runningOf indexes the running apps of list.
func runningOf(list []workspace.App) running {
	r := running{
		names:        make(map[string]string, len(list)),
		pidsByName:   make(map[string]int32, len(list)),
		pidsByBundle: make(map[string]int32, len(list)),
	}
	for _, a := range list {
		if _, seen := r.pidsByName[a.Name]; a.Name != "" && !seen {
			r.pidsByName[a.Name] = a.PID
		}
		if _, seen := r.pidsByBundle[a.BundleID]; a.BundleID != "" && !seen {
			r.pidsByBundle[a.BundleID] = a.PID
			r.names[a.BundleID] = a.Name
		}
	}
	return r
}

// appIcon draws the icon of the app at path, or of the app named name
// when the path is unknown.
func appIcon(name, path string) (ui.ImageSource, error) {
	if path == "" {
		found, err := workspace.AppPath(name)
		if err != nil {
			return nil, err
		}
		path = found
	}
	png, err := mygo.Shell.FileIcon(path, iconPixels)
	if err != nil {
		return nil, err
	}
	return ui.DecodeImage(png)
}
