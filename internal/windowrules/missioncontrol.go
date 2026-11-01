package windowrules

// While Mission Control is up, CoreGraphics reports the thumbnails'
// frames as the windows' bounds, so a round would "fix" windows that are
// in place and resize them to thumbnail size. Probed on macOS 26 with two
// displays (CGWindowListCopyWindowInfo, on screen only, every 0.1s): from
// the first frame of the opening animation to the last of the closing
// one, the Dock owns one extra window per display at layer 18, unnamed,
// alpha 1, whose bounds are exactly that display's frame. They are gone
// as soon as Mission Control is. The Dock's own permanent window sits at
// layer 20 and is not one of them. kCGWindowOwnerName is localized ("程序坞"
// on a Chinese system), so the owner is told by its process name instead.
const (
	// missionControlLayer is the layer of the Dock's Mission Control
	// backdrop windows.
	missionControlLayer = 18
	// dockProcess is the Dock's process name, which, unlike its owner
	// name in the window list, is not localized.
	dockProcess = "Dock"
)

// isMissionControlWindow reports whether a listed window is one of the
// Dock's Mission Control backdrops: at missionControlLayer, covering a
// whole display within Tolerance, owned by the Dock. process returns the
// owner's process name; it is only called once the cheap checks pass, so
// an ordinary round does not look up any process.
func isMissionControlWindow(layer int64, bounds Rect, displays []Display, process func() string) bool {
	if layer != missionControlLayer {
		return false
	}
	if !isCGFullScreen(bounds, displays) {
		return false
	}
	return process() == dockProcess
}
