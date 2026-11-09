package copyflash

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The flash, sizes in DIPs: the emoji grows from startSize to endSize
// as it fades from maxAlpha to none, linearly over duration.
const (
	emoji             = "📑"
	startSize float32 = 80
	endSize   float32 = 220
	duration          = 600 * time.Millisecond
	maxAlpha  float32 = 0.6
	// winSize is the window's width and height. The text is endSize all
	// along, scaled down at the start, and an emoji's line box is about
	// 1.2 times its font size, so a window of endSize would clip the
	// emoji, or push it off center.
	winSize = int(endSize * 5 / 4)
)

// Flash is the window of the flash. Show is safe from any goroutine and
// returns without waiting for the main thread.
type Flash struct {
	win *mygo.Window
	// run counts the flashes shown, each played over by the view's
	// timeline; main thread only.
	run uint64
}

// NewFlash creates the hidden flash window, anchored to tray as the HUD
// is, which lets it float above the windows of other apps. It must be
// created once the app is ready.
func NewFlash(tray *mygo.Tray) *Flash {
	f := &Flash{}
	f.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      tray,
		Placement: mygo.PlacementBottom,
		Width:     winSize,
		Height:    winSize,
		Focusable: false,
		Hidden:    true,
		Shadow:    false,
		Content:   ui.View(f.view),
	})
	// It shows wherever the pointer is, never in the way of a click.
	f.win.SetIgnoreMouseEvents(true)
	f.win.SetVisibleOnAllWorkspaces(true)
	return f
}

// Show starts the flash in the middle of the display under the pointer,
// over one that is still running.
func (f *Flash) Show() {
	f.win.Update(func() {
		f.run++
		f.win.SetOpacity(1)
		f.win.ShowInactive()
		// After showing, as reveal does: mygo places a flyout by its tray
		// icon.
		f.place()
	})
}

// place centers the window on the display under the pointer. Main thread
// only.
func (f *Flash) place() {
	area := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).Bounds
	f.win.SetPosition(area.X+(area.Width-winSize)/2, area.Y+(area.Height-winSize)/2)
}

func (f *Flash) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	col := ui.Column(c).Fill().Center().Background(ui.Transparent)
	// NonMotion: the flash plays even while the desktop asks for less
	// motion, as it is what tells the copy landed.
	tl := col.Timeline("flash", ui.TimelineOptions{Run: f.run, NonMotion: true})
	scale := tl.Tween(ui.Tween{From: startSize / endSize, To: 1, Duration: duration, Ease: ui.Linear})
	alpha := tl.Tween(ui.Tween{From: maxAlpha, To: 0, Duration: duration, Ease: ui.Linear}, ui.WithPrevious(0))
	col.Children(func() {
		ui.Text(c, emoji).FontSize(endSize).Scale(scale, scale).Opacity(alpha).NoWrap()
	})
	if !tl.Settled() {
		return
	}
	// Not while the frame builds: Update runs it after. A Show may be
	// queued before it, starting a new flash, which must not be hidden.
	run := f.run
	f.win.Update(func() {
		if f.run == run {
			f.win.Hide()
		}
	})
}
