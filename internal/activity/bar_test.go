package activity

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestBarOrigin(t *testing.T) {
	// A secondary display left of the primary one has a negative origin.
	area := mygo.Rectangle{X: -1000, Y: 25, Width: 1000, Height: 700}
	got := barOrigin(area)
	want := mygo.Point{X: -610, Y: 25}
	if got != want {
		t.Fatalf("barOrigin = %+v, want %+v", got, want)
	}
}

func TestTitle(t *testing.T) {
	tests := []struct {
		name string
		s    Snapshot
		want string
	}{
		{"idle", Snapshot{Status: Idle, Elapsed: 5*time.Minute + 30*time.Second}, "💤 5m"},
		{"resuming", Snapshot{Status: Idle, Resuming: true, Elapsed: 5*time.Minute + 30*time.Second}, "❓ 5m"},
		{"fresh", Snapshot{Status: Active, Elapsed: 42 * time.Second, IdleIn: IdleThreshold}, "▶42s -2m"},
		{"countdown rounds up to tens", Snapshot{Status: Active, Elapsed: 12 * time.Minute, IdleIn: 53 * time.Second}, "▶12m -60s"},
		{"countdown over a minute", Snapshot{Status: Active, Elapsed: 12 * time.Minute, IdleIn: 61 * time.Second}, "▶12m -1m"},
		{"paused", Snapshot{Status: Active, Paused: true, Elapsed: 12 * time.Minute, IdleIn: 20 * time.Second}, "⏸︎12m -20s"},
		{"overtime", Snapshot{Status: Active, Overtime: true, Elapsed: WorkLimit, IdleIn: IdleThreshold}, "⚠️▶25m -2m"},
		{"overtime two", Snapshot{Status: Active, Overtime: true, Elapsed: WorkLimit + 5*time.Minute, IdleIn: IdleThreshold}, "⚠️▶30m -2m⚠️"},
		{"overtime capped", Snapshot{Status: Active, Overtime: true, Elapsed: 2 * time.Hour, IdleIn: IdleThreshold}, "⚠️⚠️⚠️▶2h00m -2m⚠️⚠️⚠️"},
	}
	for _, tt := range tests {
		if got := Title(tt.s); got != tt.want {
			t.Errorf("%s: Title = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                     "0s",
		59*time.Second + 900*time.Millisecond: "59s",
		time.Minute:                           "1m",
		59*time.Minute + 59*time.Second:       "59m",
		time.Hour + 5*time.Minute:             "1h05m",
	}
	for d, want := range tests {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
