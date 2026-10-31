package windowrules

import (
	"testing"
	"time"
)

func TestHoldZeroValueHoldsNothing(t *testing.T) {
	var h hold
	if h.holding(time.Now()) {
		t.Fatal("zero hold is holding")
	}
}

func TestHoldExtendsUntilDeadline(t *testing.T) {
	var h hold
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.extend(t0, 10*time.Second)
	if !h.holding(t0.Add(9 * time.Second)) {
		t.Fatal("not holding before the deadline")
	}
	if h.holding(t0.Add(10 * time.Second)) {
		t.Fatal("holding at the deadline")
	}
}

func TestHoldKeepsTheLaterDeadline(t *testing.T) {
	var h hold
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.extend(t0, 10*time.Second)
	h.extend(t0.Add(5*time.Second), 10*time.Second)
	if !h.holding(t0.Add(14 * time.Second)) {
		t.Fatal("second change did not extend the hold")
	}
	h.extend(t0.Add(6*time.Second), time.Second)
	if !h.holding(t0.Add(14 * time.Second)) {
		t.Fatal("a shorter extend cut the hold")
	}
}
