package hud

import (
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"burin/internal/leader"
)

func tester(t *testing.T, s *leader.Snapshot) (*ui.Tester, *[2]float32) {
	t.Helper()
	var size [2]float32
	tt := ui.NewTester(func(c *ui.Context) {
		w, h := Build(c, *s)
		size = [2]float32{w, h}
	}, 600, 400)
	return tt, &size
}

// savePNG writes the frame for a look by hand, when HUD_PNG names a dir.
func savePNG(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("HUD_PNG")
	if dir == "" {
		return
	}
	f, err := os.Create(dir + "/" + name + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestEntriesAndMarks(t *testing.T) {
	s := leader.Snapshot{Active: true, Key: "cmd_r", Entries: []leader.EntryState{
		{Key: "k", Title: "Kitty", Status: leader.Running},
		{Key: "l", Title: "Activity Monitor", Status: leader.Done},
		{Key: "b", Title: "Google Chrome", Status: leader.Failed},
		{Key: "d", Title: "复制今日日期"},
	}}
	tt, size := tester(t, &s)
	savePNG(t, tt, "entries")
	for _, want := range []string{"cmd_r", "4 entries", "k", "Kitty", "Activity Monitor", "复制今日日期", MarkRunning, MarkDone, MarkFailed} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if size[0] <= 0 || size[1] <= 0 {
		t.Fatalf("size %v", *size)
	}
	// At the size it asks for, everything fits.
	tt.SetSize(int(math.Ceil(float64(size[0]))), int(math.Ceil(float64(size[1]))))
	tt.Frame()
	savePNG(t, tt, "fit")
	for _, text := range []string{"Activity Monitor", "4 entries", "复制今日日期"} {
		r, ok := tt.Find(text)
		fits := ok && r.X+r.W <= size[0] && r.Y+r.H <= size[1]
		if !fits {
			t.Errorf("%q at %+v does not fit %v", text, r, *size)
		}
	}

	// Marks follow the snapshot on the next frame.
	for i := range s.Entries {
		s.Entries[i].Status = leader.Idle
	}
	tt.Frame()
	for _, m := range []string{MarkRunning, MarkDone, MarkFailed} {
		if slices.Contains(tt.Texts(), m) {
			t.Errorf("idle entries show %q", m)
		}
	}
}

func TestOrderAndColumns(t *testing.T) {
	keys := "abcdefghijklmnopqrstuvwxyz"
	var s leader.Snapshot
	s = leader.Snapshot{Active: true, Key: "cmd_r"}
	for i := range 12 {
		s.Entries = append(s.Entries, leader.EntryState{Key: keys[i : i+1], Title: fmt.Sprintf("Title %d", i)})
	}
	tt, size := tester(t, &s)
	oneColumn := *size
	first, _ := tt.Find("Title 0")
	last, _ := tt.Find("Title 11")
	if first.X != last.X || last.Y <= first.Y {
		t.Fatalf("12 entries not in one column: %+v %+v", first, last)
	}

	s.Entries = append(s.Entries, leader.EntryState{Key: "m", Title: "Title 12"})
	tt.Frame()
	savePNG(t, tt, "columns")
	a, _ := tt.Find("Title 0")
	b, _ := tt.Find("Title 7")
	if b.X <= a.X || b.Y != a.Y {
		t.Fatalf("13 entries not in two columns of 7 and 6: %+v %+v", a, b)
	}
	if size[0] <= oneColumn[0] || size[1] >= oneColumn[1] {
		t.Fatalf("two columns sized %v, one %v", *size, oneColumn)
	}
}

func TestSplit(t *testing.T) {
	for n, want := range map[int][]int{1: {1}, 12: {12}, 13: {7, 6}, 24: {12, 12}, 25: {9, 9, 7}} {
		var got []int
		for _, col := range split(make([]leader.EntryState, n)) {
			got = append(got, len(col))
		}
		if !slices.Equal(got, want) {
			t.Errorf("split(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestParseError(t *testing.T) {
	s := leader.Snapshot{Active: true, Key: "cmd_r", Err: errors.New(`cmd_r.md: ## k B: duplicate key "k"`)}
	tt, _ := tester(t, &s)
	savePNG(t, tt, "error")
	if !tt.HasText("duplicate key") || !tt.HasText("cmd_r") || !tt.HasText("0 entries") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestSplitRows(t *testing.T) {
	var entries []leader.EntryState
	for _, b := range []bool{true, false, true, true, false, false, true} {
		entries = append(entries, leader.EntryState{RowBreak: b})
	}
	var got []int
	for _, row := range splitRows(entries) {
		got = append(got, len(row))
	}
	if want := []int{2, 1, 3, 1}; !slices.Equal(got, want) {
		t.Errorf("splitRows = %v, want %v", got, want)
	}
	entries[0].RowBreak = false
	if got := splitRows(entries[:2]); len(got) != 1 || len(got[0]) != 2 {
		t.Errorf("splitRows without breaks = %v", got)
	}
	if got := splitRows(nil); len(got) != 0 {
		t.Errorf("splitRows(nil) = %v", got)
	}
}

func TestRowsLayout(t *testing.T) {
	s := leader.Snapshot{Active: true, Key: "f18", Layout: leader.LayoutRows, Entries: []leader.EntryState{
		{Key: "1", Title: "Chrome"},
		{Key: "2", Title: "Yab", Status: leader.Running},
		{Key: "q", Title: "Moshi", RowBreak: true},
		{Key: "w", Title: "Activity Monitor and more"},
		{Key: "e", Title: "Kitty"},
	}}
	tt, size := tester(t, &s)
	savePNG(t, tt, "rows")
	chrome, _ := tt.Find("Chrome")
	yab, _ := tt.Find("Yab")
	moshi, _ := tt.Find("Moshi")
	kitty, _ := tt.Find("Kitty")
	sameRow := yab.Y == chrome.Y && yab.X > chrome.X
	if !sameRow {
		t.Errorf("Chrome %+v and Yab %+v not in one row", chrome, yab)
	}
	nextRow := moshi.Y > chrome.Y && moshi.X == chrome.X && kitty.Y == moshi.Y
	if !nextRow {
		t.Errorf("Moshi %+v and Kitty %+v not starting the next row under %+v", moshi, kitty, chrome)
	}

	// Two rows, as wide as the second, the widest.
	var m metrics
	ui.NewTester(func(c *ui.Context) { m = measure(c, s) }, 600, 400)
	if len(m.rows) != 2 || len(m.cellW[0]) != 2 || len(m.cellW[1]) != 3 {
		t.Fatalf("rows %v", m.rows)
	}
	rowW := func(widths []float32) float32 {
		w := float32(len(widths)-1) * rowCellGap
		for _, cw := range widths {
			w += cw
		}
		return w
	}
	if rowW(m.cellW[1]) <= rowW(m.cellW[0]) {
		t.Fatalf("second row %v not wider than first %v", m.cellW[1], m.cellW[0])
	}
	if got, want := m.width, 2*padding+rowW(m.cellW[1]); got != want {
		t.Errorf("width %v, want %v", got, want)
	}
	if got, want := m.height, 2*padding+m.lineH+headerGap+2*m.rowH+rowGap; got != want {
		t.Errorf("height %v, want %v", got, want)
	}
	if *size != [2]float32{m.width, m.height} {
		t.Errorf("Build size %v, measured %v x %v", *size, m.width, m.height)
	}

	// At the size it asks for, everything fits.
	tt.SetSize(int(math.Ceil(float64(size[0]))), int(math.Ceil(float64(size[1]))))
	tt.Frame()
	for _, text := range []string{"Activity Monitor and more", "Kitty", "5 entries"} {
		r, ok := tt.Find(text)
		fits := ok && r.X+r.W <= size[0] && r.Y+r.H <= size[1]
		if !fits {
			t.Errorf("%q at %+v does not fit %v", text, r, *size)
		}
	}
}

func TestPlainLines(t *testing.T) {
	s := leader.Snapshot{Active: true, Key: "cmd_r", Plain: []string{"← Back", "↑ Mission Control"}}
	tt, size := tester(t, &s)
	savePNG(t, tt, "plain")
	texts := tt.Texts()
	if !slices.Equal(texts, s.Plain) {
		t.Fatalf("texts = %q, want only %q", texts, s.Plain)
	}
	tt.SetSize(int(math.Ceil(float64(size[0]))), int(math.Ceil(float64(size[1]))))
	tt.Frame()
	first, _ := tt.Find("← Back")
	for _, text := range s.Plain {
		r, ok := tt.Find(text)
		fits := ok && r.X+r.W <= size[0] && r.Y+r.H <= size[1]
		if !fits {
			t.Errorf("%q at %+v does not fit %v", text, r, *size)
		}
	}
	if second, _ := tt.Find("↑ Mission Control"); second.Y <= first.Y {
		t.Errorf("lines not stacked: %+v %+v", first, second)
	}
}
