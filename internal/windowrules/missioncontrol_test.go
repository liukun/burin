package windowrules

import "testing"

func TestIsMissionControlWindow(t *testing.T) {
	// The layout the probe ran on: the BenQ, and the portrait display to
	// its left, above it.
	displays := []Display{benq, {Frame: Rect{X: -1080, Y: -640, W: 1080, H: 1920}}}
	tests := []struct {
		name    string
		process string
		layer   int64
		bounds  Rect
		want    bool
	}{
		{"Dock 18 层盖满主屏", "Dock", 18, Rect{0, 0, 1920, 1280}, true},
		{"Dock 18 层盖满副屏", "Dock", 18, Rect{-1080, -640, 1080, 1920}, true},
		{"Dock 常驻 20 层窗口", "Dock", 20, Rect{0, 0, 1920, 1280}, false},
		{"Dock 18 层不盖满", "Dock", 18, Rect{100, 100, 800, 600}, false},
		{"别的进程 18 层盖满", "ShowyEdge", 18, Rect{0, 0, 1920, 1280}, false},
		{"普通窗口", "Dock", 0, Rect{0, 0, 1920, 1280}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMissionControlWindow(tt.layer, tt.bounds, displays, func() string { return tt.process })
			if got != tt.want {
				t.Fatalf("isMissionControlWindow = %v, want %v", got, tt.want)
			}
		})
	}
}

// The process is looked up only for windows that pass the cheap checks.
func TestIsMissionControlWindowLooksUpLazily(t *testing.T) {
	asked := false
	process := func() string { asked = true; return "Dock" }
	isMissionControlWindow(0, Rect{0, 0, 1920, 1280}, []Display{benq}, process)
	isMissionControlWindow(18, Rect{0, 0, 10, 10}, []Display{benq}, process)
	if asked {
		t.Fatal("process looked up for a window that cannot be a backdrop")
	}
}
