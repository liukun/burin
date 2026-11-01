package hud

import (
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"burin/internal/gesture"
)

// Layout of the compass, in DIPs.
const (
	// cursorGap is the distance from the pointer to the near edge of a
	// label, leaving the pointer in sight in the middle.
	cursorGap = 36
	// cornerGap is the distance on both axes from the pointer to the
	// inner corner of a diagonal label: cursorGap scaled as the diagonal
	// gestures' threshold is, so they sit farther out.
	cornerGap  = cursorGap * gesture.DiagonalMinDistance / gesture.MinDistance
	pillPadX   = 8
	pillPadY   = 3
	pillRadius = 6
	pillBorder = 1
	// armedGrow is the padding an armed label gains, which makes it
	// look picked up.
	armedGrow  = 2
	glowBlur   = 10
	glowSpread = 1
	// glowRoom keeps the glow of an armed label at the window's edge
	// from being cut off.
	glowRoom = glowBlur + glowSpread + 2
	dotSize  = 6
	// compassOpacity is the compass's when shown: its labels are small,
	// so they need not let what they cover show through.
	compassOpacity = 1
	// pollInterval is how often the pointer is read while a button is
	// held, about once a frame: a held real mouse sends no drags.
	pollInterval = 16 * time.Millisecond
)

// Colors of the progress of a gesture.
var (
	progressColor = ui.Hex("#3fd2ff")
	// armedColor tells that releasing runs the gesture.
	armedColor = keyColor
)

// pillAlpha is the opacity of a label's background, so that what is
// under the compass shows through.
const pillAlpha = 0.8

// Directions are the titles of the gestures of the held button by
// direction; an empty title is a direction without a gesture.
type Directions struct {
	Up, Down, Left, Right                string
	UpLeft, UpRight, DownLeft, DownRight string
}

// Empty reports whether no direction has a gesture.
func (d Directions) Empty() bool { return d == Directions{} }

// slot is the title of dir in d, nil for a direction it has none of.
func (d *Directions) slot(dir gesture.Direction) *string {
	switch dir {
	case gesture.Up:
		return &d.Up
	case gesture.Down:
		return &d.Down
	case gesture.Left:
		return &d.Left
	case gesture.Right:
		return &d.Right
	case gesture.UpLeft:
		return &d.UpLeft
	case gesture.UpRight:
		return &d.UpRight
	case gesture.DownLeft:
		return &d.DownLeft
	case gesture.DownRight:
		return &d.DownRight
	}
	return nil
}

// title is the title of the gesture in dir.
func (d Directions) title(dir gesture.Direction) string {
	if s := d.slot(dir); s != nil {
		return *s
	}
	return ""
}

// Add sets the title of the gesture in dir, unless it has one already.
func (d *Directions) Add(dir gesture.Direction, title string) {
	if s := d.slot(dir); s != nil && *s == "" {
		*s = title
	}
}

// Progress is how far the pointer went towards each direction, from 0 at
// the press to 1 at gesture.MinDistance for the axes and at
// gesture.DiagonalMinDistance for the diagonals, and the direction a
// release would recognize, if any.
type Progress struct {
	Up, Down, Left, Right                float32
	UpLeft, UpRight, DownLeft, DownRight float32
	Armed                                gesture.Direction
}

// of is the progress towards dir.
func (p Progress) of(dir gesture.Direction) float32 {
	switch dir {
	case gesture.Up:
		return p.Up
	case gesture.Down:
		return p.Down
	case gesture.Left:
		return p.Left
	case gesture.Right:
		return p.Right
	case gesture.UpLeft:
		return p.UpLeft
	case gesture.UpRight:
		return p.UpRight
	case gesture.DownLeft:
		return p.DownLeft
	case gesture.DownRight:
		return p.DownRight
	}
	return 0
}

// total is the progress towards the direction the pointer went furthest.
func (p Progress) total() float32 {
	return max(p.Up, p.Down, p.Left, p.Right, p.UpLeft, p.UpRight, p.DownLeft, p.DownRight)
}

// progressOf is the progress of a pointer dx, dy from the press, y down
// as in CoreGraphics. Each direction lights up by the pointer's
// projection on it, so a drag lights several; the diagonals light up
// over gesture.DiagonalMinDistance, more slowly, as they need a longer
// drag. Only gesture.Classify arms one, so that the compass tells what a
// release would do.
func progressOf(dx, dy float64) Progress {
	// frac is v as a fraction of dist, kept between 0 and 1.
	frac := func(v, dist float64) float32 { return float32(min(max(v/dist, 0), 1)) }
	t := func(v float64) float32 { return frac(v, gesture.MinDistance) }
	// diag is the progress along the diagonal (sx, sy)/√2.
	diag := func(sx, sy float64) float32 {
		return frac((sx*dx+sy*dy)/math.Sqrt2, gesture.DiagonalMinDistance)
	}
	p := Progress{
		Up: t(-dy), Down: t(dy), Left: t(-dx), Right: t(dx),
		UpLeft: diag(-1, -1), UpRight: diag(1, -1), DownLeft: diag(-1, 1), DownRight: diag(1, 1),
	}
	if dir, ok := gesture.Classify(dx, dy); ok {
		p.Armed = dir
	}
	return p
}

// Compass is the flyout of the gestures of the held mouse button: their
// titles around the pointer, lighting up as it moves towards them. Its
// methods are safe from any goroutine and return without waiting for the
// main thread, so the event tap may call them.
type Compass struct {
	win    *mygo.Window
	reveal reveal
	// center is the press, where the compass centers; main thread only.
	center mygo.Point
	// shift is how far the press is from the window's middle, nonzero
	// once the window was moved back on screen; main thread only.
	shift mygo.Point

	// mu guards the gestures shown, the pointer's offset from the press
	// and the polling.
	mu     sync.Mutex
	dirs   Directions
	dx, dy float64
	// stop stops the poller of the last show, nil while hidden; a poller
	// whose stop is no longer it is stale and stops writing.
	stop chan struct{}
}

// NewCompass creates the hidden compass, anchored to tray as the HUD is.
// It must be created once the app is ready.
func NewCompass(tray *mygo.Tray) *Compass {
	c := &Compass{}
	c.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      tray,
		Placement: mygo.PlacementBottom,
		Width:     initialWidth,
		Height:    initialHeight,
		Focusable: false,
		Hidden:    true,
		Shadow:    false,
		Content:   ui.View(c.view),
	})
	// The replayed click of a press lands where the pointer is, which
	// the compass covers.
	c.win.SetIgnoreMouseEvents(true)
	c.reveal = reveal{win: c.win, width: initialWidth, height: initialHeight, opacity: compassOpacity, place: c.place}
	return c
}

// Show shows dirs in the compass centered on the press at x, y, in
// CoreGraphics coordinates, and follows the pointer until Hide.
func (c *Compass) Show(dirs Directions, x, y float64) {
	c.mu.Lock()
	c.stopPolling()
	stop := make(chan struct{})
	c.stop = stop
	c.dirs = dirs
	c.dx, c.dy = 0, 0
	c.mu.Unlock()
	go c.poll(stop, x, y)

	center := mygo.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
	c.win.Update(func() {
		c.center = center
		c.reveal.begin()
	})
}

// Hide hides the compass and stops following the pointer.
func (c *Compass) Hide() {
	c.mu.Lock()
	c.stopPolling()
	c.dirs = Directions{}
	c.mu.Unlock()
	c.win.Update(c.win.Hide)
}

// stopPolling stops the poller of the last show. c.mu is held.
func (c *Compass) stopPolling() {
	if c.stop == nil {
		return
	}
	close(c.stop)
	c.stop = nil
}

// poll reads the pointer about once a frame, as a held real mouse sends
// no drags, and redraws the compass with its offset from the press at
// x0, y0. A pointer that did not move is not redrawn.
func (c *Compass) poll(stop chan struct{}, x0, y0 float64) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last mygo.Point
	read := false
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		// Waits for the main thread, which is fine off it.
		p := mygo.Screen.CursorScreenPoint()
		if read && p == last {
			continue
		}
		last, read = p, true
		c.mu.Lock()
		stale := c.stop != stop
		if !stale {
			c.dx, c.dy = float64(p.X)-x0, float64(p.Y)-y0
		}
		c.mu.Unlock()
		if stale {
			return
		}
		c.win.Invalidate()
	}
}

// place centers the compass on the press, within its display. Main
// thread only.
func (c *Compass) place() {
	display := mygo.Screen.DisplayNearestPoint(c.center)
	w, h := c.reveal.width, c.reveal.height
	p := centeredAt(c.center, w, h, display.Bounds)
	c.shift = pressOffset(c.center, p, w, h)
	c.win.SetPosition(p.X, p.Y)
}

// pressOffset is how far center is from the middle of a window of the
// given size at p: zero unless centeredAt moved the window back.
func pressOffset(center, p mygo.Point, width, height int) mygo.Point {
	return mygo.Point{X: center.X - (p.X + width/2), Y: center.Y - (p.Y + height/2)}
}

// centeredAt is where a window of the given size goes to be centered on
// center, moved back into area where it would stick out; a window larger
// than area sticks out on the right and bottom.
func centeredAt(center mygo.Point, width, height int, area mygo.Rectangle) mygo.Point {
	clamp := func(v, lo, size, room int) int {
		return max(min(v, lo+room-size), lo)
	}
	return mygo.Point{
		X: clamp(center.X-width/2, area.X, width, area.Width),
		Y: clamp(center.Y-height/2, area.Y, height, area.Height),
	}
}

func (c *Compass) view(ctx *ui.Context) {
	c.mu.Lock()
	d, dx, dy := c.dirs, c.dx, c.dy
	c.mu.Unlock()
	// The labels stay around the window's middle; the dot is drawn at
	// the pointer, which is off the middle by the shift.
	dotX, dotY := float64(c.shift.X)+dx, float64(c.shift.Y)+dy
	w, h := buildCompass(ctx, d, progressOf(dx, dy), dotX, dotY)
	// The compass is hidden while no button is held: keep its size, so a
	// press does not resize it from empty.
	if d.Empty() {
		return
	}
	c.reveal.fit(w, h)
}

// compassDirs are the directions in the order the labels are built.
var compassDirs = []gesture.Direction{
	gesture.Up, gesture.Down, gesture.Left, gesture.Right,
	gesture.UpLeft, gesture.UpRight, gesture.DownLeft, gesture.DownRight,
}

// pillStyle is how a label looks at its progress.
type pillStyle struct {
	text, border ui.Color
	pad          float32
	glow         bool
}

// styleOf is the style of a label at progress t, armed or not: plain text at
// rest, turning to the progress color as the pointer nears, and the
// armed color, grown and glowing, once a release would run it.
func styleOf(th *ui.Theme, t float32, armed bool) pillStyle {
	if armed {
		return pillStyle{text: armedColor, border: armedColor, pad: armedGrow, glow: true}
	}
	return pillStyle{
		text:   th.Text.Mix(progressColor, t),
		border: th.Border.Mix(progressColor, t),
	}
}

// buildCompass builds the labels of d around the middle, lit by p, and
// the dot of the pointer dx, dy from the middle; it returns the content
// size they need.
func buildCompass(c *ui.Context, d Directions, p Progress, dx, dy float64) (width, height float32) {
	th := c.Theme()
	c.Root().Background(ui.Transparent)
	type label struct {
		dir   gesture.Direction
		title string
		// textW and textH are the title's size; w and h the label's at
		// rest, without the growth of an armed one.
		textW, textH, w, h float32
	}
	var labels []label
	var sideW, sideH, stackW, stackH, cornerW, cornerH float32
	for _, dir := range compassDirs {
		title := d.title(dir)
		if title == "" {
			continue
		}
		tw, tht := c.MeasureText(0, ui.Span{Text: title, Size: fontSize})
		tw += slack
		l := label{dir: dir, title: title, textW: tw, textH: tht,
			w: tw + 2*(pillPadX+pillBorder), h: tht + 2*(pillPadY+pillBorder)}
		labels = append(labels, l)
		switch dir {
		case gesture.Up, gesture.Down:
			stackW, stackH = max(stackW, l.w), max(stackH, l.h)
		case gesture.Left, gesture.Right:
			sideW, sideH = max(sideW, l.w), max(sideH, l.h)
		default:
			cornerW, cornerH = max(cornerW, l.w), max(cornerH, l.h)
		}
	}
	// Sized for armed labels, which grow, so that arming one does not
	// resize the window.
	grow := float32(2 * armedGrow)
	// Corner labels reach cornerGap plus their size from the middle on
	// both axes.
	var cornerX, cornerY float32
	if cornerW > 0 {
		cornerX, cornerY = 2*(cornerGap+cornerW+grow), 2*(cornerGap+cornerH+grow)
	}
	width = max(2*(cursorGap+sideW+grow), stackW+grow, cornerX) + 2*glowRoom
	height = max(2*(cursorGap+stackH+grow), sideH+grow, cornerY) + 2*glowRoom
	midX, midY := width/2, height/2

	for _, l := range labels {
		armed := p.Armed == l.dir
		st := styleOf(th, p.of(l.dir), armed)
		w, h := l.w+2*st.pad, l.h+2*st.pad
		// The near edge stays cursorGap from the middle as a label grows,
		// and the inner corner of a diagonal label cornerGap on both axes.
		var x, y float32
		switch l.dir {
		case gesture.Up:
			x, y = midX-w/2, midY-cursorGap-h
		case gesture.Down:
			x, y = midX-w/2, midY+cursorGap
		case gesture.Left:
			x, y = midX-cursorGap-w, midY-h/2
		case gesture.Right:
			x, y = midX+cursorGap, midY-h/2
		case gesture.UpLeft:
			x, y = midX-cornerGap-w, midY-cornerGap-h
		case gesture.UpRight:
			x, y = midX+cornerGap, midY-cornerGap-h
		case gesture.DownLeft:
			x, y = midX-cornerGap-w, midY+cornerGap
		case gesture.DownRight:
			x, y = midX+cornerGap, midY+cornerGap
		}
		pill := ui.Row(c.Key(string(l.dir))).Absolute().Left(x).Top(y).Width(w).Height(h).
			AlignItems(ui.Center).Justify(ui.Center).
			Background(th.Surface.Alpha(pillAlpha)).Radius(pillRadius).Border(pillBorder, st.border)
		if st.glow {
			pill.Shadow(0, 0, glowBlur, glowSpread, armedColor.Alpha(0.6))
		}
		pill.Children(func() {
			ui.Text(c, l.title).FontSize(fontSize).TextColor(st.text).Width(l.textW).NoWrap()
		})
	}

	// Green tells that a release runs a gesture: not so in a direction
	// without one.
	dotColor := th.TextMuted.Mix(progressColor, p.total())
	armsGesture := d.title(p.Armed) != ""
	if armsGesture {
		dotColor = armedColor
	}
	ui.Box(c.Key("dot")).Absolute().
		Left(midX + float32(dx) - dotSize/2).Top(midY + float32(dy) - dotSize/2).
		Width(dotSize).Height(dotSize).Radius(dotSize / 2).Background(dotColor)
	return width, height
}
