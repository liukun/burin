package inputmark

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Both marks show their text at their alpha for hold, then fade it to
// none, linearly over fade.
const (
	hold = 1500 * time.Millisecond
	fade = 500 * time.Millisecond
)

// followInterval is how often the small mark moves after the pointer
// while it shows: often enough to look attached, rarely enough not to
// move the window every frame.
const followInterval = time.Second / 24

// look is the size and alpha of a mark, sizes in DIPs.
type look struct {
	fontSize   float32
	alpha      float32
	shadowBlur float32
	// width and height are the window's: room for a few characters, and
	// a line box about 1.2 times the font size plus the shadow.
	width, height int
	// besidePointer keeps the text to the pointer's side of the window,
	// as close to the pointer as the shadow allows, rather than centered.
	besidePointer bool
}

// bigLook is the mark of an app or input source change, in the middle of
// a display: faint, so as not to hide what is under it. Its shadow keeps
// a light text readable over a light background.
var bigLook = look{
	fontSize:   120,
	alpha:      0.35,
	shadowBlur: 8,
	width:      120 * 4,
	height:     120 * 3 / 2,
}

// smallLook is the mark of a click, beside the pointer: smaller than the
// pointer, so as not to crowd what it points at, and nearly opaque to be
// read at a glance, with room for two or three characters.
var smallLook = look{
	fontSize:      18,
	alpha:         0.9,
	shadowBlur:    4,
	width:         18 * 7 / 2,
	height:        18 * 3 / 2,
	besidePointer: true,
}

// shadowColor is the shadow under the text.
var shadowColor = ui.RGBA(0, 0, 0, 0.8)

// Mark is the two windows of the input source mark, the big one and the
// small one, each showing and fading on its own. Its methods are safe
// from any goroutine and return without waiting for the main thread.
type Mark struct {
	big, small *markWindow
}

// markWindow is the window of one mark.
type markWindow struct {
	win  *mygo.Window
	look look
	// run counts the marks shown, each played over by the view's
	// timeline; text and color are what the last one shows, and alignEnd
	// whether its text lines up with the window's right edge rather than
	// its left. Main thread only.
	run      uint64
	text     string
	color    ui.Color
	alignEnd bool
	// follows is whether the mark showing follows the pointer: a small
	// one, until it is hidden, so that a follow still queued does not
	// move it. origin is where the window is, and followed when it last
	// followed the pointer. Main thread only.
	follows  bool
	origin   mygo.Point
	followed time.Time
}

// NewMark creates the hidden mark windows, anchored to tray as the
// clipboard flash is, which lets them float above the windows of other
// apps. It must be created once the app is ready.
func NewMark(tray *mygo.Tray) *Mark {
	return &Mark{big: newMarkWindow(tray, bigLook), small: newMarkWindow(tray, smallLook)}
}

func newMarkWindow(tray *mygo.Tray, l look) *markWindow {
	m := &markWindow{look: l}
	m.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      tray,
		Placement: mygo.PlacementBottom,
		Width:     l.width,
		Height:    l.height,
		Focusable: false,
		Hidden:    true,
		Shadow:    false,
		Content:   ui.View(m.view),
	})
	// It shows wherever the pointer or the focused window is, never in
	// the way of a click.
	m.win.SetIgnoreMouseEvents(true)
	m.win.SetVisibleOnAllWorkspaces(true)
	return m
}

// ShowBig shows style's text large, in the middle of the display nearest
// center, over a big mark still showing, whose hold starts over.
func (m *Mark) ShowBig(style Style, center mygo.Point) {
	m.big.show(style, func() (mygo.Point, bool) {
		area := mygo.Screen.DisplayNearestPoint(center).Bounds
		return centerOrigin(area, m.big.look.width, m.big.look.height), false
	})
}

// ShowSmall shows style's text small, beside a click at at, over a small
// mark still showing, whose hold starts over. It then follows the
// pointer until it is gone, onto another display too.
func (m *Mark) ShowSmall(style Style, at Point) {
	m.small.show(style, func() (mygo.Point, bool) {
		return m.small.besidePointer(screenPoint(at))
	})
}

// besidePointer is the origin of the window beside the pointer at p, on
// the display p is on, with whether the text lines up with the window's
// right edge.
func (m *markWindow) besidePointer(p mygo.Point) (mygo.Point, bool) {
	area := mygo.Screen.DisplayNearestPoint(p).Bounds
	return smallOrigin(p, area, m.look.width, m.look.height)
}

// show shows style's text at the origin place returns, on the main
// thread, with whether the text lines up with the window's right edge.
// A small mark then follows the pointer while it shows.
func (m *markWindow) show(style Style, place func() (origin mygo.Point, alignEnd bool)) {
	c := style.Color
	color := ui.RGBA(c.R, c.G, c.B, float32(c.A)/255)
	m.win.Update(func() {
		m.run++
		m.text, m.color = style.Text, color
		// No follow yet: the first frame follows at once, moving the mark
		// from the click to where the pointer is by then, a pixel or two.
		m.follows, m.followed = m.look.besidePointer, time.Time{}
		origin, alignEnd := place()
		m.alignEnd = alignEnd
		m.win.SetOpacity(1)
		m.win.ShowInactive()
		// After showing: mygo places a flyout by its tray icon.
		m.win.SetPosition(origin.X, origin.Y)
		m.origin = origin
	})
}

// followPointer places the window of the mark run anew, unless that mark
// was hidden or another shown since. A pointer that stays put leaves the
// window where it is.
func (m *markWindow) followPointer(run uint64) {
	if !m.following(run) {
		return
	}
	origin, alignEnd := m.besidePointer(mygo.Screen.CursorScreenPoint())
	m.alignEnd = alignEnd
	if origin == m.origin {
		return
	}
	m.win.SetPosition(origin.X, origin.Y)
	m.origin = origin
}

func (m *markWindow) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	l := m.look
	col := ui.Column(c).Fill().Center().Background(ui.Transparent)
	if l.besidePointer {
		align := ui.Start
		if m.alignEnd {
			align = ui.End
		}
		col.AlignItems(align).Padding(l.shadowBlur)
	}
	// NonMotion: the mark only fades, and tells which input source is on
	// even while the desktop asks for less motion. Delay holds it at its
	// alpha before it fades.
	tl := col.Timeline("mark", ui.TimelineOptions{Run: m.run, NonMotion: true, Delay: hold})
	alpha := tl.Tween(ui.Tween{From: l.alpha, To: 0, Duration: fade, Ease: ui.Linear})
	col.Children(func() {
		ui.Text(c, m.text).FontSize(l.fontSize).TextColor(m.color).
			TextShadow(0, 2, l.shadowBlur, shadowColor).Opacity(alpha).NoWrap()
	})
	run := m.run
	if !tl.Settled() {
		m.scheduleFollow(c.Now(), run)
		return
	}
	// Not while the frame builds: Update runs it after. A show may be
	// queued before it, starting a new mark, which must not be hidden.
	m.win.Update(func() {
		if m.run == run {
			m.follows = false
			m.win.Hide()
		}
	})
}

// scheduleFollow has the window of the mark run follow the pointer once
// followInterval has gone by since it last did. The timeline builds the
// view every frame while it holds and fades, which is what drives this,
// so following stops with it. Not while the frame builds: Update moves
// the window after, and redraws it should the text change sides.
func (m *markWindow) scheduleFollow(now time.Time, run uint64) {
	due := m.follows && followDue(now, m.followed)
	if !due {
		return
	}
	m.followed = now
	m.win.Update(func() { m.followPointer(run) })
}

// following reports whether the mark run is the one showing and follows
// the pointer: a small mark, not hidden since nor followed by another.
func (m *markWindow) following(run uint64) bool {
	return m.run == run && m.follows
}

// followDue reports whether followInterval has gone by at now since the
// mark last followed the pointer at last, the zero time for never.
func followDue(now, last time.Time) bool {
	return now.Sub(last) >= followInterval
}
