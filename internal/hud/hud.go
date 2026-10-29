// Package hud shows the entries of the held leader key in a flyout at the
// top of the display under the pointer.
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

	// width and height are the content size last asked of the window;
	// main thread only.
	width, height int
	// revealing is set while the flyout is shown invisibly, until a frame
	// built it at the size of the held leader; main thread only.
	revealing bool
}

// New creates the hidden flyout. It is anchored to tray, which lets it
// float above the windows of other apps, but is placed by place: a tray
// icon hidden by a menu bar manager sits off screen. snapshot returns the
// state to show; it is called on the main thread for every frame. Like
// every window, it must be created once the app is ready.
func New(tray *mygo.Tray, snapshot func() leader.Snapshot) *HUD {
	h := &HUD{snapshot: snapshot, width: initialWidth, height: initialHeight}
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
	return h
}

// Show shows the flyout without taking the keyboard. It is shown
// invisibly first, so that a frame can size it to the held leader, and
// becomes visible from view once it fits: showing it at the size of the
// last leader and resizing it in sight flashed.
func (h *HUD) Show() {
	h.win.Update(func() {
		h.revealing = true
		h.win.SetOpacity(0)
		h.win.ShowInactive()
		h.place()
	})
}

// place moves the flyout to the top center of the display under the
// pointer. Main thread only; it runs after mygo placed the flyout by its
// tray icon, which happens on every resize.
func (h *HUD) place() {
	display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	p := topCenter(display.WorkArea, h.width, h.height)
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
	width, height := int(math.Ceil(float64(w))), int(math.Ceil(float64(ht)))
	sizeChanged := width != h.width || height != h.height
	// Not while the frame builds: Update runs it after, in order.
	if sizeChanged {
		h.width, h.height = width, height
		h.win.Update(func() {
			h.win.SetContentSize(width, height)
			h.place()
		})
		return
	}
	if !h.revealing {
		return
	}
	// This frame fits: it drew the content at the leader's size.
	h.revealing = false
	h.win.Update(func() { h.win.SetOpacity(opacity) })
}
