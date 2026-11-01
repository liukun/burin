// Package hud has the app's two flyouts: the HUD, which shows the entries
// of the held leader key at the top of the display under the pointer, and
// the Compass, which shows the gestures of the held mouse button around
// the pointer. They are separate windows, so either shows while the other
// does.
package hud

import (
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"burin/internal/leader"
)

const (
	initialWidth  = 240
	initialHeight = 120
	cornerRadius  = 10
	// topMargin is the distance between the work area's top and the HUD.
	topMargin = 8
	// opacity is the HUD's when shown, letting what it covers show through.
	opacity = 0.8
)

// HUD is the leader flyout. Its methods are safe from any goroutine and
// return without waiting for the main thread, so the event tap may call
// them.
type HUD struct {
	win      *mygo.Window
	snapshot func() leader.Snapshot
	reveal   reveal
}

// New creates the hidden flyout. It is anchored to tray, which lets it
// float above the windows of other apps, but is placed by place: a tray
// icon hidden by a menu bar manager sits off screen. snapshot returns the
// state to show; it is called on the main thread for every frame. Like
// every window, it must be created once the app is ready.
func New(tray *mygo.Tray, snapshot func() leader.Snapshot) *HUD {
	h := &HUD{snapshot: snapshot}
	h.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:         tray,
		Placement:    mygo.PlacementBottom,
		Gap:          4,
		Width:        initialWidth,
		Height:       initialHeight,
		Focusable:    false,
		Hidden:       true,
		Shadow:       true,
		Vibrancy:     mygo.VibrancyHUD,
		CornerRadius: cornerRadius,
		Content:      ui.View(h.view),
	})
	h.reveal = reveal{win: h.win, width: initialWidth, height: initialHeight, opacity: opacity, place: h.place}
	return h
}

// Show shows the flyout without taking the keyboard, sized to the held
// leader; see reveal.
func (h *HUD) Show() { h.win.Update(h.reveal.begin) }

// place moves the flyout to the top center of the display under the
// pointer. Main thread only; it runs after mygo placed the flyout by its
// tray icon, which happens on every resize.
func (h *HUD) place() {
	display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	p := topCenter(display.WorkArea, h.reveal.width, h.reveal.height)
	h.win.SetPosition(p.X, p.Y)
}

// topCenter is where a window of the given size goes to sit centered
// under the top edge of area.
func topCenter(area mygo.Rectangle, width, height int) mygo.Point {
	return mygo.Point{
		X: area.X + (area.Width-width)/2,
		Y: area.Y + topMargin,
	}
}

// Hide hides the flyout.
func (h *HUD) Hide() { h.win.Update(h.win.Hide) }

// Invalidate redraws the flyout after the snapshot changed.
func (h *HUD) Invalidate() { h.win.Invalidate() }

func (h *HUD) view(c *ui.Context) {
	s := h.snapshot()
	w, ht := Build(c, s)
	// The flyout is hidden while no leader is held: keep its size, so a
	// hold does not resize it to empty and back.
	if !s.Active {
		return
	}
	h.reveal.fit(w, ht)
}

// reveal shows a flyout sized to its content without a flash: the
// flyout is shown invisibly first, and becomes visible once a frame
// built its content at the size it needs. Showing it at the size of the
// last content and resizing it in sight flashed. All of it is main
// thread only.
type reveal struct {
	win *mygo.Window
	// width and height are the content size last asked of the window.
	width, height int
	// revealing is set while the flyout is shown invisibly, until a frame
	// built it at the size of its content.
	revealing bool
	// opacity is the flyout's once visible.
	opacity float64
	// place moves the flyout where it goes, at width x height; it runs
	// after mygo placed the flyout by its tray icon, which happens on
	// every resize.
	place func()
}

// begin shows the flyout invisibly, waiting for fit to reveal it.
func (r *reveal) begin() {
	r.revealing = true
	r.win.SetOpacity(0)
	r.win.ShowInactive()
	r.place()
}

// fit is called by every frame that built content of the given size: it
// resizes the flyout to it, or reveals the flyout once a frame fits.
func (r *reveal) fit(w, h float32) {
	width, height := int(math.Ceil(float64(w))), int(math.Ceil(float64(h)))
	sizeChanged := width != r.width || height != r.height
	// Not while the frame builds: Update runs it after, in order.
	if sizeChanged {
		r.width, r.height = width, height
		r.win.Update(func() {
			r.win.SetContentSize(width, height)
			r.place()
		})
		return
	}
	if !r.revealing {
		return
	}
	// This frame fits: it drew the content at its size.
	r.revealing = false
	r.win.Update(func() { r.win.SetOpacity(r.opacity) })
}
