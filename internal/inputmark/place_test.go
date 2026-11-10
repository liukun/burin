package inputmark

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestScreenPoint(t *testing.T) {
	if got, want := screenPoint(Point{10.4, -3.6}), (mygo.Point{X: 10, Y: -4}); got != want {
		t.Fatalf("screenPoint = %v, want %v", got, want)
	}
}

func TestWindowCenter(t *testing.T) {
	// A window on a display left of the primary one.
	if got, want := windowCenter(Point{-1200, 100}, 800, 600), (Point{-800, 400}); got != want {
		t.Fatalf("windowCenter = %v, want %v", got, want)
	}
}

func TestCenterOrigin(t *testing.T) {
	display := mygo.Rectangle{X: 1440, Y: -200, Width: 1920, Height: 1080}
	if got, want := centerOrigin(display, 480, 180), (mygo.Point{X: 1440 + 720, Y: -200 + 450}); got != want {
		t.Fatalf("centerOrigin = %v, want %v", got, want)
	}
}

func TestSmallOrigin(t *testing.T) {
	display := mygo.Rectangle{X: 0, Y: 0, Width: 1440, Height: 900}
	const w, h = 63, 27
	// For the pointer at y = 400 the window's middle is 4 above it: its
	// top at 400 - 27/2 - 4.
	const top = 383
	tests := []struct {
		name     string
		pointer  mygo.Point
		display  mygo.Rectangle
		want     mygo.Point
		wantLeft bool
	}{
		{"right of the pointer", mygo.Point{X: 500, Y: 400}, display, mygo.Point{X: 514, Y: top}, false},
		{"just fits on the right", mygo.Point{X: 1440 - 14 - w, Y: 400}, display, mygo.Point{X: 1440 - w, Y: top}, false},
		{"left past the right edge", mygo.Point{X: 1400, Y: 400}, display, mygo.Point{X: 1400 - 14 - w, Y: top}, true},
		{"down from the top", mygo.Point{X: 500, Y: 5}, display, mygo.Point{X: 514, Y: 0}, false},
		{"up from the bottom", mygo.Point{X: 500, Y: 895}, display, mygo.Point{X: 514, Y: 900 - h}, false},
		{"corner", mygo.Point{X: 1439, Y: 899}, display, mygo.Point{X: 1439 - 14 - w, Y: 900 - h}, true},
		{
			"second display",
			mygo.Point{X: 1500, Y: -190},
			mygo.Rectangle{X: 1440, Y: -200, Width: 1920, Height: 1080},
			mygo.Point{X: 1514, Y: -200},
			false,
		},
	}
	for _, tt := range tests {
		got, left := smallOrigin(tt.pointer, tt.display, w, h)
		if got != tt.want || left != tt.wantLeft {
			t.Errorf("%s: smallOrigin = %v, %v, want %v, %v", tt.name, got, left, tt.want, tt.wantLeft)
		}
	}
}
