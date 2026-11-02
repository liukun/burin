package activity

import (
	"testing"
	"time"
)

func TestItemsIsTitle(t *testing.T) {
	tr := NewTracker(time.Now().Add(-3*time.Minute), DefaultConfig)
	items := tr.Items()
	if len(items) != 1 {
		t.Fatalf("Items = %d items, want 1", len(items))
	}
	want := Title(tr.Snapshot(time.Now()))
	if items[0].Text != want || items[0].Menu == nil {
		t.Errorf("Items = %+v, want text %q with a menu", items[0], want)
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
		{"fresh", Snapshot{Status: Active, Elapsed: 42 * time.Second, IdleIn: DefaultIdle, Work: DefaultWork}, "▶42s -2m"},
		{"countdown rounds up to tens", Snapshot{Status: Active, Elapsed: 12 * time.Minute, IdleIn: 53 * time.Second, Work: DefaultWork}, "▶12m -60s"},
		{"countdown over a minute", Snapshot{Status: Active, Elapsed: 12 * time.Minute, IdleIn: 61 * time.Second}, "▶12m -1m"},
		{"paused", Snapshot{Status: Active, Paused: true, Elapsed: 12 * time.Minute, IdleIn: 20 * time.Second}, "⏸︎12m -20s"},
		{"overtime", Snapshot{Status: Active, Overtime: true, Elapsed: DefaultWork, IdleIn: DefaultIdle, Work: DefaultWork}, "⚠️▶25m -2m"},
		{"overtime two", Snapshot{Status: Active, Overtime: true, Elapsed: DefaultWork + 5*time.Minute, IdleIn: DefaultIdle, Work: DefaultWork}, "⚠️▶30m -2m⚠️"},
		{"overtime of a shorter work time", Snapshot{Status: Active, Overtime: true, Elapsed: 20 * time.Minute, IdleIn: DefaultIdle, Work: 10 * time.Minute}, "⚠️⚠️▶20m -2m⚠️"},
		{"overtime capped", Snapshot{Status: Active, Overtime: true, Elapsed: 2 * time.Hour, IdleIn: DefaultIdle, Work: DefaultWork}, "⚠️⚠️⚠️▶2h00m -2m⚠️⚠️⚠️"},
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
