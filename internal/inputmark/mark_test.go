package inputmark

import (
	"testing"
	"time"
)

func TestFollowDue(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last time.Time
		want bool
	}{
		{"never followed", time.Time{}, true},
		{"1ns short of the interval", now.Add(-followInterval + time.Nanosecond), false},
		{"exactly the interval", now.Add(-followInterval), true},
		{"same frame", now, false},
	}
	for _, tt := range tests {
		if got := followDue(now, tt.last); got != tt.want {
			t.Errorf("%s: followDue = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFollowing(t *testing.T) {
	tests := []struct {
		name string
		m    markWindow
		run  uint64
		want bool
	}{
		{"current mark", markWindow{run: 2, follows: true}, 2, true},
		{"an older mark", markWindow{run: 3, follows: true}, 2, false},
		{"hidden, or the big mark", markWindow{run: 2, follows: false}, 2, false},
	}
	for _, tt := range tests {
		if got := tt.m.following(tt.run); got != tt.want {
			t.Errorf("%s: following = %v, want %v", tt.name, got, tt.want)
		}
	}
}
