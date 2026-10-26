package hud

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestTopCenter(t *testing.T) {
	// A secondary display left of the primary one has a negative origin.
	area := mygo.Rectangle{X: -1000, Y: 25, Width: 1000, Height: 700}
	got := topCenter(area, 300, 120)
	want := mygo.Point{X: -650, Y: 25 + topMargin}
	if got != want {
		t.Fatalf("topCenter = %+v, want %+v", got, want)
	}
}
