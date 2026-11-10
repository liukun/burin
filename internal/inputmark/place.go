package inputmark

import (
	"math"

	"github.com/egoist/mygo"
)

// The small mark's offset from the pointer, in DIPs: right of it, clear
// of the arrow, with the window's middle a little above the pointer's
// tip, so the text reads level with the arrow rather than under it.
const (
	pointerOffsetX = 14
	pointerRaise   = 4
)

// screenPoint rounds p to the screen coordinates of mygo, which, as the
// event tap's and Accessibility's, are DIPs from the top-left corner of
// the primary display.
func screenPoint(p Point) mygo.Point {
	return mygo.Point{X: int(math.Round(p.X)), Y: int(math.Round(p.Y))}
}

// windowCenter is the center of a window at pos of size, both as
// Accessibility reads them.
func windowCenter(pos Point, width, height float64) Point {
	return Point{X: pos.X + width/2, Y: pos.Y + height/2}
}

// centerOrigin is the top-left corner of a window of width by height
// centered on display.
func centerOrigin(display mygo.Rectangle, width, height int) mygo.Point {
	return mygo.Point{
		X: display.X + (display.Width-width)/2,
		Y: display.Y + (display.Height-height)/2,
	}
}

// smallOrigin is the top-left corner of the small mark's window of width
// by height for the pointer at pointer on display: right of the pointer,
// or left of it should the window cross the display's right edge, and
// moved up or down into the display should it cross the top or bottom.
// left reports that it went left, where the text lines up with the
// window's right edge to stay by the pointer.
func smallOrigin(pointer mygo.Point, display mygo.Rectangle, width, height int) (origin mygo.Point, left bool) {
	x := pointer.X + pointerOffsetX
	crossesRight := x+width > display.X+display.Width
	if crossesRight {
		x = pointer.X - pointerOffsetX - width
	}
	y := pointer.Y - height/2 - pointerRaise
	y = min(y, display.Y+display.Height-height)
	y = max(y, display.Y)
	return mygo.Point{X: x, Y: y}, crossesRight
}
