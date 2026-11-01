package gesture

import "math"

// Thresholds of a gesture. A drag whose main axis makes up at least
// AxisRatio of the horizontal plus vertical distance, within about 23
// degrees of an axis, is left, right, up or down when it is at least
// MinDistance pixels long. Any other drag is in one of the diagonal
// sectors between them and is a diagonal gesture when it is at least
// DiagonalMinDistance pixels long, so that a sloppy short drag is no
// gesture.
const (
	MinDistance         = 30
	DiagonalMinDistance = 60
	AxisRatio           = 0.7
)

// Classify returns the direction of a drag by dx, dy, where y grows
// downwards as in CoreGraphics, and whether the drag is a gesture at all.
func Classify(dx, dy float64) (Direction, bool) {
	ax, ay := math.Abs(dx), math.Abs(dy)
	length := math.Hypot(ax, ay)
	if length < MinDistance {
		return "", false
	}
	major := max(ax, ay)
	onAxis := major/(ax+ay) >= AxisRatio
	if onAxis {
		return cardinal(dx, dy, ax, ay), true
	}
	if length < DiagonalMinDistance {
		return "", false
	}
	return diagonal(dx, dy), true
}

// cardinal is the axis direction of a drag along its main axis, given
// its absolute distances ax and ay on each axis.
func cardinal(dx, dy, ax, ay float64) Direction {
	switch {
	case ax >= ay && dx < 0:
		return Left
	case ax >= ay:
		return Right
	case dy < 0:
		return Up
	default:
		return Down
	}
}

// diagonal is the diagonal direction of a drag off both axes.
func diagonal(dx, dy float64) Direction {
	up, left := dy < 0, dx < 0
	switch {
	case up && left:
		return UpLeft
	case up:
		return UpRight
	case left:
		return DownLeft
	default:
		return DownRight
	}
}

// Kind is what a mouse event does to its button.
type Kind int

// Kinds.
const (
	Press Kind = iota
	Drag
	Release
)

// KindOf returns the kind of a mouse event type, and false for other
// event types.
func KindOf(eventType uint32) (Kind, bool) {
	switch eventType {
	case EventRightMouseDown, EventOtherMouseDown:
		return Press, true
	case EventRightMouseDragged, EventOtherMouseDragged:
		return Drag, true
	case EventRightMouseUp, EventOtherMouseUp:
		return Release, true
	default:
		return 0, false
	}
}

// Result is what the Recognizer makes of an event.
type Result struct {
	// Consume is set for the events of a tracked press, which the app
	// must not see: the press, its drags and its release.
	Consume bool
	// Ended is set for the release of a tracked press. Gesture then says
	// whether the drag was one, in Direction.
	Ended     bool
	Gesture   bool
	Direction Direction
}

// Recognizer follows one press of a mouse button at a time and compares
// where it was released with where it was pressed. Drags are not added
// up: with some mice the system sends none while the button is held. It
// is not safe for concurrent use.
//
// Every press starts over: with two buttons down, the one pressed last is
// tracked, and the release of the other is passed on untouched.
type Recognizer struct {
	active bool
	button Button
	// x0, y0 is where the press was.
	x0, y0 float64
}

// Handle feeds one event of button b to the recognizer; x and y are the
// pointer's location at the event, read for presses and releases only.
// Presses are always tracked: the caller passes only presses of buttons
// that have gestures.
func (r *Recognizer) Handle(kind Kind, b Button, x, y float64) Result {
	if kind == Press {
		*r = Recognizer{active: true, button: b, x0: x, y0: y}
		return Result{Consume: true}
	}
	tracked := r.active && r.button == b
	if !tracked {
		return Result{}
	}
	if kind == Drag {
		return Result{Consume: true}
	}
	dir, ok := Classify(x-r.x0, y-r.y0)
	*r = Recognizer{}
	return Result{Consume: true, Ended: true, Gesture: ok, Direction: dir}
}

// Reset forgets the press being tracked.
func (r *Recognizer) Reset() { *r = Recognizer{} }
