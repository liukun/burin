// Package workspace asks NSWorkspace for the running apps and hides,
// quits, activates and launches them in process, without forking open.
package workspace

// App is one running app as NSWorkspace lists it.
type App struct {
	PID      int32
	Name     string
	BundleID string
	// Path is the path of the app's bundle, empty for a process without
	// one.
	Path string
	// Active is set for the frontmost app.
	Active bool
	Hidden bool
}
