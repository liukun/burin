package windowrules

import "testing"

func TestNextWindowID(t *testing.T) {
	ids := []uint32{10, 20, 30}
	tests := []struct {
		name   string
		mainID uint32
		ids    []uint32
		want   uint32
	}{
		{name: "none main", mainID: 0, ids: ids, want: 10},
		{name: "main first", mainID: 10, ids: ids, want: 20},
		{name: "main middle", mainID: 20, ids: ids, want: 30},
		{name: "main last wraps", mainID: 30, ids: ids, want: 10},
		{name: "main not listed", mainID: 25, ids: ids, want: 30},
		{name: "main not listed past the last wraps", mainID: 35, ids: ids, want: 10},
		{name: "two windows wrap", mainID: 20, ids: []uint32{10, 20}, want: 10},
		{name: "one window", mainID: 10, ids: []uint32{10}, want: 0},
		{name: "no windows", mainID: 0, ids: nil, want: 0},
	}
	for _, tt := range tests {
		if got := nextWindowID(tt.mainID, tt.ids); got != tt.want {
			t.Errorf("%s: nextWindowID(%d, %v) = %d, want %d", tt.name, tt.mainID, tt.ids, got, tt.want)
		}
	}
}

// TestNextWindowIDVisitsEvery checks that clicks reach every window: the
// order is stable whichever window becomes main, so following the pick
// from each main visits all of them before coming back.
func TestNextWindowIDVisitsEvery(t *testing.T) {
	ids := []uint32{7, 3, 9}
	sorted := []uint32{3, 7, 9}
	main := uint32(3)
	seen := map[uint32]bool{main: true}
	for range len(ids) - 1 {
		main = nextWindowID(main, sorted)
		seen[main] = true
	}
	if len(seen) != len(ids) {
		t.Errorf("visited %v, want all of %v", seen, ids)
	}
	if next := nextWindowID(main, sorted); next != 3 {
		t.Errorf("after the last window the cycle goes to %d, want 3", next)
	}
}
