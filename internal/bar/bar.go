// Package bar is the floating bar at the top center of every display. It
// is a host: it knows windows, layout and clicks, and nothing of what it
// shows. Features implement Section and are laid out from left to right
// in the order they were added; they call Invalidate when what Items
// returns changes. While no section has an item the bar is hidden.
package bar

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Layout of the bar, in DIPs.
const (
	height   = 24
	fontSize = 12
	iconSize = 18
	// iconGap is between an item's icon and its text, itemGap between
	// items, padding on either side of them all.
	iconGap = 4
	itemGap = 10
	padding = 8
	// initialWidth is the width a window is created at, before its first
	// frame has measured the content.
	initialWidth = 220
	// minWidth keeps an empty bar a window AppKit can size.
	minWidth = 2 * padding
	// slack keeps rounding in the text measurements from leaving the
	// window a pixel narrower than the laid-out text.
	slack = 2
	// rebuildDelay lets a burst of display changes, as one is plugged in,
	// rebuild the bars once.
	rebuildDelay = 500 * time.Millisecond
	// resizeDelay is past the refresh period that fit must wait, 20ms in
	// tests; not visible on a bar.
	resizeDelay = 50 * time.Millisecond
)

// textColor is the bar's text: plain white, no background. mygo has no
// text stroke and its Shadow is a box shadow, which drew a dark frame
// around the text.
var textColor = ui.RGBA(255, 255, 255, 0.95)

// Item is one thing a section shows on the bar: an icon, a text or both.
type Item struct {
	Icon    ui.ImageSource
	Text    string
	Tooltip string
	// OnClick and Menu run on the main thread: what waits on it, a
	// dialog or a call that hops to it, belongs on a goroutine.
	OnClick func()
	Menu    func(m *ui.Menu)
}

// empty reports whether it has nothing to draw.
func (it Item) empty() bool { return it.Icon == nil && it.Text == "" }

// Section is a feature shown on the bar. Items is called from any
// goroutine, by Invalidate; a section must not call Invalidate while
// holding a lock its Items takes.
type Section interface {
	Items() []Item
}

// Bar shows the items of its sections at the top center of every display,
// in a floating panel on all Spaces. Its methods are safe from any
// goroutine.
type Bar struct {
	// invalidateMu orders the Invalidates, so that the items of an earlier
	// one never replace those of a later one.
	invalidateMu sync.Mutex
	// mu guards the sections and the items the windows draw, those of the
	// last Invalidate. The windows show while there are items.
	mu       sync.Mutex
	sections []Section
	items    []Item

	// winMu guards the windows, one per display. It is never held while
	// waiting on the main thread, which takes it in Invalidate from a
	// click or a menu.
	winMu sync.Mutex
	wins  []*window
	timer *time.Timer
	// rebuildMu keeps two rebuilds from interleaving; only off the main
	// thread.
	rebuildMu sync.Mutex
}

// window is the bar on one display.
type window struct {
	// area is the display's work area, the bar centered at its top.
	area mygo.Rectangle
	// win is set once the window exists; its view may run before.
	win atomic.Pointer[mygo.Window]
	// width is the window's content width. Main thread only.
	width int
}

// New makes a bar, hidden until it has items, on every display and rebuilds them as the
// displays change. It must be called once the app is ready.
func New() *Bar {
	b := &Bar{}
	b.rebuild()
	mygo.Screen.OnDisplaysChanged(b.displaysChanged)
	return b
}

// Add appends sections to the right of those added before and redraws.
func (b *Bar) Add(sections ...Section) {
	b.mu.Lock()
	b.sections = append(b.sections, sections...)
	b.mu.Unlock()
	b.Invalidate()
}

// Invalidate asks the sections for their items again, and redraws the
// bars when what they draw changed: a section may call it on every tick
// without the bars redrawing each time.
func (b *Bar) Invalidate() {
	b.invalidateMu.Lock()
	defer b.invalidateMu.Unlock()
	b.mu.Lock()
	sections := b.sections
	b.mu.Unlock()
	items := collect(sections)
	b.mu.Lock()
	changed := !sameItems(b.items, items)
	visibilityChanged := (len(b.items) > 0) != (len(items) > 0)
	// Stored even when they look the same: their callbacks may be newer.
	b.items = items
	b.mu.Unlock()
	if !changed {
		return
	}
	b.winMu.Lock()
	wins := b.wins
	b.winMu.Unlock()
	for _, w := range wins {
		win := w.win.Load()
		if win == nil {
			continue
		}
		win.Invalidate()
		if visibilityChanged {
			b.syncVisibility(win)
		}
	}
}

// shows reports whether the windows are to show: whether the last
// Invalidate found items.
func (b *Bar) shows() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items) > 0
}

// syncVisibility shows or hides win, on the main thread, as the bar is
// then: the last of several calls in flight leaves it right whatever
// order they were made in. It does not wait on the main thread, so it
// may be called with invalidateMu held.
func (b *Bar) syncVisibility(win *mygo.Window) {
	win.Update(func() {
		if b.shows() {
			win.ShowInactive()
			return
		}
		win.Hide()
	})
}

// collect returns the items of sections in order, without empty ones.
func collect(sections []Section) []Item {
	var items []Item
	for _, s := range sections {
		for _, it := range s.Items() {
			if it.empty() {
				continue
			}
			items = append(items, it)
		}
	}
	return items
}

// sameItems reports whether a and b draw the same: their icons, texts
// and tooltips. Callbacks cannot be compared and are left out.
func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		sameLook := a[i].Icon == b[i].Icon && a[i].Text == b[i].Text && a[i].Tooltip == b[i].Tooltip
		if !sameLook {
			return false
		}
	}
	return true
}

// contentWidth is the width the bar needs for items, whose texts measure
// as measure says: the items in a row, with gaps between them and padding
// on either side.
func contentWidth(items []Item, measure func(text string) float32) float32 {
	var w float32
	for i, it := range items {
		if i > 0 {
			w += itemGap
		}
		w += itemWidth(it, measure)
	}
	return max(w+2*padding, minWidth)
}

// itemWidth is the width of one item: its icon, its text and the gap
// between them when it has both.
func itemWidth(it Item, measure func(text string) float32) float32 {
	var w float32
	if it.Icon != nil {
		w += iconSize
	}
	if it.Text == "" {
		return w
	}
	if it.Icon != nil {
		w += iconGap
	}
	return w + measure(it.Text) + slack
}

// barOrigin is where a bar of the given width goes to sit centered at
// the top of area, the display's work area, under the menu bar.
func barOrigin(area mygo.Rectangle, width int) mygo.Point {
	return mygo.Point{
		X: area.X + (area.Width-width)/2,
		Y: area.Y,
	}
}

// displaysChanged rebuilds the bars once the displays settle.
func (b *Bar) displaysChanged() {
	b.winMu.Lock()
	defer b.winMu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(rebuildDelay, func() {
		b.rebuildMu.Lock()
		defer b.rebuildMu.Unlock()
		b.rebuild()
	})
}

// rebuild replaces the bars with one per connected display.
func (b *Bar) rebuild() {
	var wins []*window
	for _, d := range mygo.Screen.Displays() {
		wins = append(wins, b.newWindow(d.WorkArea, initialWidth))
	}
	b.winMu.Lock()
	old := b.wins
	b.wins = wins
	b.winMu.Unlock()
	// An Invalidate that changed the visibility while the windows were
	// made did not see them.
	for _, w := range wins {
		b.syncVisibility(w.win.Load())
	}
	for _, w := range old {
		w.win.Load().Close()
	}
}

// newWindow makes a bar of the width at the top of the work area, shown
// unless the bar has no items.
func (b *Bar) newWindow(area mygo.Rectangle, width int) *window {
	w := &window{area: area, width: width}
	p := barOrigin(area, width)
	// Created hidden and moved before showing: a window at 0, 0 would be
	// centered instead.
	win := mygo.NewWindow(mygo.WindowOptions{
		Width:          width,
		Height:         height,
		UseContentSize: true,
		Hidden:         true,
		Frameless:      true,
		Transparent:    true,
		AlwaysOnTop:    true,
		Panel:          true,
		DisableResize:  true,
		DisableMove:    true,
		DisableShadow:  true,
		Content:        ui.View(func(c *ui.Context) { b.view(c, w) }),
	})
	w.win.Store(win)
	win.SetPosition(p.X, p.Y)
	if b.shows() {
		win.ShowInactive()
	}
	return w
}

func (b *Bar) view(c *ui.Context, w *window) {
	b.mu.Lock()
	items := b.items
	b.mu.Unlock()
	c.Root().Background(ui.Transparent)
	row := ui.Row(c).Fill().Center().Gap(itemGap).Background(ui.Transparent)
	row.Children(func() {
		for _, it := range items {
			itemView(c, it)
		}
	})
	measure := func(text string) float32 {
		tw, _ := c.MeasureText(0, ui.Span{Text: text, Size: fontSize})
		return tw
	}
	b.fit(w, contentWidth(items, measure))
}

// fit resizes the window to the content's width, unless it has it, and
// keeps it centered. The resize waits resizeDelay: queued right after the
// frame that measured the content, it lands in the pass after a trimmed
// drawable's refit and mygo scales that frame into the new size, which
// squeezes the icons and text sideways. One refresh later it does not.
// A frame before the window is stored leaves the width for the next frame
// to fit.
func (b *Bar) fit(w *window, width float32) {
	px := int(math.Ceil(float64(width)))
	if px == w.width {
		return
	}
	win := w.win.Load()
	if win == nil {
		return
	}
	w.width = px
	time.AfterFunc(resizeDelay, func() {
		win.Update(func() {
			win.SetContentSize(px, height)
			p := barOrigin(w.area, px)
			win.SetPosition(p.X, p.Y)
		})
	})
}

// itemView builds one item: its icon, then its text, with its tooltip,
// click and menu on the whole of it. An item without a menu takes a
// right click as a click.
func itemView(c *ui.Context, it Item) {
	// Shrink(0) throughout: the window takes the content's width only
	// after the frame that changed it, and on that frame flex would
	// squeeze the icons sideways instead of letting them overflow.
	el := ui.Row(c).Center().Gap(iconGap).Shrink(0).Background(ui.Transparent)
	el.Children(func() {
		if it.Icon != nil {
			ui.Image(c, it.Icon).Size(iconSize, iconSize).Shrink(0)
		}
		if it.Text != "" {
			ui.Text(c, it.Text).FontSize(fontSize).TextColor(textColor).NoWrap().Shrink(0)
		}
	})
	if it.Tooltip != "" {
		el.Tooltip(it.Tooltip)
	}
	if it.OnClick != nil {
		el.OnClick(it.OnClick).Cursor(ui.CursorPointer)
	}
	if it.Menu != nil {
		el.ContextMenu(it.Menu)
		return
	}
	if it.OnClick != nil {
		el.HandleInput(secondaryClick(it.OnClick))
	}
}

// secondaryButton is the InputEvent.Button of a right click.
const secondaryButton = 1

// secondaryClick is an input handler that calls onClick as the secondary
// button is released. It takes the press too, so the release comes to it.
func secondaryClick(onClick func()) func(ui.InputEvent) bool {
	return func(ev ui.InputEvent) bool {
		if ev.Button != secondaryButton {
			return false
		}
		switch ev.Kind {
		case ui.InputPointerDown:
			return true
		case ui.InputPointerUp:
			onClick()
			return true
		}
		return false
	}
}
