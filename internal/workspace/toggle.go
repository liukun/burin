package workspace

// Action is what toggling an app does to it.
type Action int

const (
	LaunchApp Action = iota
	ActivateApp
	HideApp
)

// Decide picks what toggling app name does given the running apps:
// a running app that is frontmost is hidden, a running one that is not
// is activated, and one not running is launched. Match by Name
// (localizedName) or BundleID, exact match. Of several matches the
// active one wins, else the first.
func Decide(apps []App, name string) (Action, App) {
	app, ok := find(apps, name)
	if !ok {
		return LaunchApp, App{}
	}
	if app.Active {
		return HideApp, app
	}
	return ActivateApp, app
}

// find returns the running app named name, preferring the active one of
// several, as that is the one the user sees and a toggle should hide.
func find(apps []App, name string) (App, bool) {
	// An empty name would match every process without a bundle ID.
	if name == "" {
		return App{}, false
	}
	var first App
	found := false
	for _, a := range apps {
		matches := a.Name == name || a.BundleID == name
		if !matches {
			continue
		}
		if a.Active {
			return a, true
		}
		if !found {
			first, found = a, true
		}
	}
	return first, found
}
