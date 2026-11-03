package badge

import (
	"log"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"burin/internal/bar"
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
)

// Badge is the badge of an app on the bar: its title in the Dock and
// the badge's text, "•" for a badge without a count.
type Badge struct {
	Name  string
	Label string
	// Path is the app's bundle as the Dock knows it, empty when unknown.
	Path string
}

// Watcher reads the Dock's badges of its apps every PollInterval and is
// a section of the bar: one item per app with a badge, in the order of
// its apps. Its methods are safe from any goroutine.
type Watcher struct {
	// OnChange is called, without the watcher's lock, when the badges
	// changed.
	OnChange func()

	// The system the watcher reads and acts on; tests stand in for it.
	// read returns the Dock icons by title, running the names of the
	// running apps by bundle identifier, icon the icon of an app from its
	// bundle path or else its name, open activates an app, and now is the
	// time.
	read    func() (map[string]DockIcon, error)
	running func() map[string]string
	icon    func(name, path string) (ui.ImageSource, error)
	open    func(name string) error
	now     func() time.Time

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
	// lastErr is the last read error logged, so a lasting one is logged
	// once.
	lastErr string
	stop    chan struct{}
	stopped bool
}

// NewWatcher returns a watcher of the Dock with no apps.
func NewWatcher() *Watcher {
	return &Watcher{
		read:         Read,
		running:      runningNames,
		icon:         appIcon,
		open:         workspace.Open,
		now:          time.Now,
		icons:        map[string]ui.ImageSource{},
		iconFailures: map[string]iconFailure{},
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

// poll reads the Dock and keeps the badges of the apps. A failed read
// clears them, as they can no longer be told apart from stale ones. It
// runs on the one polling goroutine, or in tests one call at a time.
func (w *Watcher) poll() {
	w.mu.Lock()
	apps := w.apps
	w.mu.Unlock()
	if len(apps) == 0 {
		w.setBadges(nil, false)
		return
	}
	icons, err := w.read()
	if err != nil {
		w.logError(err)
		w.setBadges(nil, false)
		return
	}
	w.logError(nil)
	var bundles map[string]string
	// Without a badge in the Dock no app has one: the running apps need
	// not be listed.
	needBundles := anyBundle(apps) && anyLabel(icons)
	if needBundles {
		bundles = w.running()
	}
	badges := match(apps, icons, bundles)
	// An icon drawn after an earlier failure changes the items too.
	iconAdded := w.loadIcons(badges)
	w.setBadges(badges, iconAdded)
}

// logError logs err unless it is the last one logged; nil forgets it.
func (w *Watcher) logError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil {
		w.lastErr = ""
		return
	}
	if err.Error() == w.lastErr {
		return
	}
	w.lastErr = err.Error()
	log.Printf("badges: %v", err)
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

// Items makes the watcher a section of the bar: the icon and badge of
// every app with one, which a click activates.
func (w *Watcher) Items() []bar.Item {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := make([]bar.Item, 0, len(w.badges))
	for _, b := range w.badges {
		name := b.Name
		items = append(items, bar.Item{
			Icon:    w.icons[name],
			Text:    b.Label,
			Tooltip: name,
			// The click runs on the main thread, which open waits on.
			OnClick: func() { go w.activate(name) },
		})
	}
	return items
}

// activate brings app name to the front.
func (w *Watcher) activate(name string) {
	if err := w.open(name); err != nil {
		log.Printf("badges: open %s: %v", name, err)
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
		name := a.Name
		if a.Bundle != "" {
			name = bundles[a.Bundle]
		}
		icon := icons[name]
		dotHidden := a.CountOnly && !isCount(icon.Label)
		skip := name == "" || icon.Label == "" || seen[name] || dotHidden
		if skip {
			continue
		}
		seen[name] = true
		badges = append(badges, Badge{Name: name, Label: icon.Label, Path: icon.Path})
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

// runningNames returns the names of the running apps by bundle
// identifier.
func runningNames() map[string]string {
	apps, _ := workspace.RunningApps()
	names := make(map[string]string, len(apps))
	for _, a := range apps {
		if a.BundleID != "" {
			names[a.BundleID] = a.Name
		}
	}
	return names
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
