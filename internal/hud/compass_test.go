package hud

import (
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"burin/internal/gesture"
)

func TestCenteredAt(t *testing.T) {
	area := mygo.Rectangle{X: 0, Y: 0, Width: 1440, Height: 900}
	// A secondary display left of the primary one has a negative origin.
	left := mygo.Rectangle{X: -1920, Y: -200, Width: 1920, Height: 1080}
	for _, tc := range []struct {
		name   string
		center mygo.Point
		area   mygo.Rectangle
		want   mygo.Point
		// shift is the press's offset from the window's middle.
		shift mygo.Point
	}{
		{"centered", mygo.Point{X: 700, Y: 400}, area, mygo.Point{X: 600, Y: 340}, mygo.Point{}},
		{"top edge", mygo.Point{X: 700, Y: 10}, area, mygo.Point{X: 600, Y: 0}, mygo.Point{X: 0, Y: -50}},
		{"top left", mygo.Point{X: 30, Y: 10}, area, mygo.Point{X: 0, Y: 0}, mygo.Point{X: -70, Y: -50}},
		{"bottom right", mygo.Point{X: 1430, Y: 895}, area, mygo.Point{X: 1240, Y: 780}, mygo.Point{X: 90, Y: 55}},
		{"secondary", mygo.Point{X: -1000, Y: 300}, left, mygo.Point{X: -1100, Y: 240}, mygo.Point{}},
		{"secondary top left", mygo.Point{X: -1900, Y: -190}, left, mygo.Point{X: -1920, Y: -200}, mygo.Point{X: -80, Y: -50}},
	} {
		got := centeredAt(tc.center, 200, 120, tc.area)
		if got != tc.want {
			t.Errorf("%s: centeredAt = %+v, want %+v", tc.name, got, tc.want)
		}
		if shift := pressOffset(tc.center, got, 200, 120); shift != tc.shift {
			t.Errorf("%s: pressOffset = %+v, want %+v", tc.name, shift, tc.shift)
		}
	}
}

func TestProgressOf(t *testing.T) {
	const d = gesture.MinDistance
	diag := d / math.Sqrt2
	// corner is the progress of a diagonal at a projection of v on it.
	corner := func(v float64) float32 { return float32(v / gesture.DiagonalMinDistance) }
	for _, tc := range []struct {
		name   string
		dx, dy float64
		want   Progress
	}{
		{"still", 0, 0, Progress{}},
		{"straight up", 0, -d, Progress{Up: 1, UpLeft: corner(diag), UpRight: corner(diag), Armed: gesture.Up}},
		{"past the threshold", 3 * d, 0, Progress{Right: 1, UpRight: 1, DownRight: 1, Armed: gesture.Right}},
		{"short of the threshold", -d / 2, 0, Progress{Left: 0.5, UpLeft: corner(diag / 2), DownLeft: corner(diag / 2)}},
		{"diagonal", diag, diag, Progress{Right: float32(diag / d), Down: float32(diag / d), DownRight: corner(d)}},
	} {
		if got := progressOf(tc.dx, tc.dy); got != tc.want {
			t.Errorf("%s: progressOf(%v, %v) = %+v, want %+v", tc.name, tc.dx, tc.dy, got, tc.want)
		}
	}
}

func TestProgressOfDiagonal(t *testing.T) {
	p := progressOf(40, 40)
	want := float32(40 * math.Sqrt2 / gesture.DiagonalMinDistance)
	if math.Abs(float64(p.DownRight-want)) > 1e-6 {
		t.Errorf("DownRight = %v, want %v", p.DownRight, want)
	}
	if p.Down != 1 || p.Right != 1 {
		t.Errorf("Down %v, Right %v, want both 1", p.Down, p.Right)
	}
	if p.UpLeft != 0 || p.UpRight != 0 || p.DownLeft != 0 {
		t.Errorf("other diagonals lit: %+v", p)
	}
	if p.Armed != "" {
		t.Errorf("Armed = %q, want none", p.Armed)
	}
	if p := progressOf(50, 50); p.Armed != gesture.DownRight {
		t.Errorf("progressOf(50, 50).Armed = %q, want %q", p.Armed, gesture.DownRight)
	}
}

// compassTester builds the compass of *d at *p, sized to what it needs.
func compassTester(t *testing.T, d *Directions, p *Progress) (*ui.Tester, *[2]float32) {
	t.Helper()
	return compassAppTester(t, d, "", p)
}

// compassAppTester is compassTester over the app named app.
func compassAppTester(t *testing.T, d *Directions, app string, p *Progress) (*ui.Tester, *[2]float32) {
	t.Helper()
	var size [2]float32
	tt := ui.NewTester(func(c *ui.Context) {
		w, h := buildCompass(c, *d, app, *p, 0, 0)
		size = [2]float32{w, h}
	}, 600, 400)
	tt.SetSize(int(math.Ceil(float64(size[0]))), int(math.Ceil(float64(size[1]))))
	tt.Frame()
	return tt, &size
}

func TestCompassLabels(t *testing.T) {
	d := Directions{Up: "Mission Control", Left: "Back"}
	p := Progress{}
	tt, size := compassTester(t, &d, &p)
	savePNG(t, tt, "compass")
	for _, want := range []string{"Mission Control", "Back"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if n := len(tt.Texts()); n != 2 {
		t.Errorf("texts %q, want only the two gestures", tt.Texts())
	}
	up, _ := tt.Find("Mission Control")
	back, _ := tt.Find("Back")
	midX, midY := size[0]/2, size[1]/2
	upAbove := up.Y+up.H <= midY-cursorGap && up.X < midX && up.X+up.W > midX
	if !upAbove {
		t.Errorf("up label %+v not above the middle %v, %v", up, midX, midY)
	}
	backLeft := back.X+back.W <= midX-cursorGap && back.Y < midY && back.Y+back.H > midY
	if !backLeft {
		t.Errorf("left label %+v not left of the middle %v, %v", back, midX, midY)
	}
	for _, r := range []ui.Rect{up, back} {
		fits := r.X >= 0 && r.Y >= 0 && r.X+r.W <= size[0] && r.Y+r.H <= size[1]
		if !fits {
			t.Errorf("%+v does not fit %v", r, *size)
		}
	}
}

func TestCompassCorners(t *testing.T) {
	d := Directions{Up: "Mission Control", Left: "Back"}
	p := Progress{}
	tt, size := compassTester(t, &d, &p)
	cardinal := *size

	d.DownRight = "Cancel"
	d.UpLeft = "Close"
	tt.Frame()
	tt.SetSize(int(math.Ceil(float64(size[0]))), int(math.Ceil(float64(size[1]))))
	tt.Frame()
	savePNG(t, tt, "compass-corners")
	// The room kept for the arcs may already hold the corner labels'
	// height, but not their width.
	larger := size[0] > cardinal[0] && size[1] >= cardinal[1]
	if !larger {
		t.Errorf("size with corners %v, not larger than %v", *size, cardinal)
	}
	midX, midY := size[0]/2, size[1]/2
	cancel, ok := tt.Find("Cancel")
	if !ok {
		t.Fatalf("missing Cancel in %q", tt.Texts())
	}
	closeLabel, ok := tt.Find("Close")
	if !ok {
		t.Fatalf("missing Close in %q", tt.Texts())
	}
	downRight := cancel.X >= midX+cornerGap && cancel.Y >= midY+cornerGap
	if !downRight {
		t.Errorf("down-right label %+v not past %v, %v", cancel, midX+cornerGap, midY+cornerGap)
	}
	upLeft := closeLabel.X+closeLabel.W <= midX-cornerGap && closeLabel.Y+closeLabel.H <= midY-cornerGap
	if !upLeft {
		t.Errorf("up-left label %+v not before %v, %v", closeLabel, midX-cornerGap, midY-cornerGap)
	}
	for _, r := range []ui.Rect{cancel, closeLabel} {
		fits := r.X >= 0 && r.Y >= 0 && r.X+r.W <= size[0] && r.Y+r.H <= size[1]
		if !fits {
			t.Errorf("%+v does not fit %v", r, *size)
		}
	}
}

// hasColorIn reports whether a pixel of r in tt's frame is about c.
func hasColorIn(tt *ui.Tester, r ui.Rect, c ui.Color) bool {
	// The tester draws at scale 1: a DIP is a pixel.
	img := tt.Image()
	near := func(a, b uint8) bool { return math.Abs(float64(a)-float64(b)) < 24 }
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			px := img.At(x, y).(color.RGBA)
			if near(px.R, c.R) && near(px.G, c.G) && near(px.B, c.B) {
				return true
			}
		}
	}
	return false
}

func TestCompassArmed(t *testing.T) {
	d := Directions{Up: "Mission Control", Left: "Back"}
	p := Progress{Left: 0.4}
	tt, _ := compassTester(t, &d, &p)
	back, _ := tt.Find("Back")
	if hasColorIn(tt, back, keyColor) {
		t.Fatal("unarmed label drawn in the armed color")
	}

	p = progressOf(-2*gesture.MinDistance, 0)
	tt.Frame()
	savePNG(t, tt, "compass-armed")
	back, _ = tt.Find("Back")
	if !hasColorIn(tt, back, keyColor) {
		t.Fatal("armed label not drawn in the armed color")
	}
	up, _ := tt.Find("Mission Control")
	if hasColorIn(tt, up, keyColor) {
		t.Fatal("unarmed up label drawn in the armed color")
	}
}

func TestStyleOf(t *testing.T) {
	th := &ui.Theme{Text: ui.Hex("#f0f0f0"), Border: ui.Hex("#404040")}
	rest := styleOf(th, 0, false)
	if rest.text != th.Text || rest.border != th.Border || rest.glow || rest.pad != 0 {
		t.Errorf("at rest %+v", rest)
	}
	if near := styleOf(th, 1, false); near.text != progressColor || near.border != progressColor {
		t.Errorf("at the threshold %+v", near)
	}
	armed := styleOf(th, 1, true)
	if armed.text != keyColor || !armed.glow || armed.pad != armedGrow {
		t.Errorf("armed %+v", armed)
	}
}

func TestCompassEmptyKeepsNothing(t *testing.T) {
	d := Directions{}
	p := progressOf(0, -2*gesture.MinDistance)
	tt, _ := compassTester(t, &d, &p)
	if texts := tt.Texts(); len(texts) != 0 {
		t.Fatalf("texts %q", texts)
	}
}

func TestCompassApp(t *testing.T) {
	d := Directions{Up: "Mission Control"}
	p := Progress{}
	tt, size := compassAppTester(t, &d, "Safari", &p)
	savePNG(t, tt, "compass-app")
	app, ok := tt.Find("Safari")
	if !ok {
		t.Fatalf("missing app in %q", tt.Texts())
	}
	up, _ := tt.Find("Mission Control")
	midX := size[0] / 2
	above := app.Y+app.H <= up.Y && app.X < midX && app.X+app.W > midX
	if !above {
		t.Errorf("app %+v not above the up label %+v", app, up)
	}
	fits := app.Y >= 0 && app.X >= 0 && app.X+app.W <= size[0]
	if !fits {
		t.Errorf("app %+v does not fit %v", app, *size)
	}
	// Without an up gesture, the name sits where it would with one.
	d2 := Directions{Left: "Back"}
	tt2, size2 := compassAppTester(t, &d2, "Safari", &p)
	app2, _ := tt2.Find("Safari")
	if below := app2.Y+app2.H > size2[1]/2-cursorGap; below {
		t.Errorf("app %+v reaches under the up label's place", app2)
	}
}

func TestArcPath(t *testing.T) {
	// The up arc peaks cursorGap above the middle, where the up label's
	// bottom edge is, and spans both sides of it. Bounds takes in the
	// control points, a little past the arc.
	b := arcPath(100, 100, cursorGap, 270, arcHalfDeg).Bounds()
	top := float32(100 - cursorGap)
	if b.Y > top || b.Y < top-2 {
		t.Errorf("up arc top %v, want just above %v", b.Y, top)
	}
	if !(b.X < 100 && b.X+b.W > 100 && b.Y+b.H < 100) {
		t.Errorf("up arc %+v not over the middle", b)
	}
	// A right arc stays right of the middle, around its level.
	if b := arcPath(100, 100, cursorGap, 0, arcHalfDeg).Bounds(); b.X <= 100 || b.Y > 100 || b.Y+b.H < 100 {
		t.Errorf("right arc %+v not at the right", b)
	}
}

func TestCompassFitsArcs(t *testing.T) {
	// With only an up gesture, the window still holds the arcs of the
	// other directions, the corner ones farthest out.
	d := Directions{Up: "Up"}
	p := Progress{}
	tt, size := compassTester(t, &d, &p)
	savePNG(t, tt, "compass-arcs")
	if size[0] < 2*arcReach || size[1] < 2*arcReach {
		t.Errorf("size %v does not hold the arcs, %v across", *size, 2*arcReach)
	}
}
