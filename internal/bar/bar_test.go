package bar

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestBarOrigin(t *testing.T) {
	// A secondary display left of the primary one has a negative origin.
	area := mygo.Rectangle{X: -1000, Y: 25, Width: 1000, Height: 700}
	got := barOrigin(area, 220)
	want := mygo.Point{X: -610, Y: 25}
	if got != want {
		t.Fatalf("barOrigin = %+v, want %+v", got, want)
	}
	// A wider bar moves left to stay centered.
	if got := barOrigin(area, 300); got.X != -650 {
		t.Fatalf("barOrigin at 300 = %+v, want X -650", got)
	}
}

// fixedSection returns its items as they are.
type fixedSection []Item

func (s fixedSection) Items() []Item { return s }

func TestCollectKeepsSectionOrder(t *testing.T) {
	icon := testIcon(t)
	got := collect([]Section{
		fixedSection{{Text: "a"}, {}},
		fixedSection{},
		fixedSection{{Icon: icon}, {Text: "b"}},
	})
	if len(got) != 3 {
		t.Fatalf("collect = %d items, want 3 without the empty one", len(got))
	}
	if got[0].Text != "a" || got[1].Icon != icon || got[2].Text != "b" {
		t.Errorf("collect = %+v, want a, icon, b", got)
	}
}

func TestSameItems(t *testing.T) {
	icon := testIcon(t)
	base := []Item{{Text: "▶1m"}, {Icon: icon, Text: "1", Tooltip: "Slack"}}
	tests := []struct {
		name  string
		other []Item
		want  bool
	}{
		{"same", []Item{{Text: "▶1m", OnClick: func() {}}, {Icon: icon, Text: "1", Tooltip: "Slack"}}, true},
		{"text", []Item{{Text: "▶2m"}, {Icon: icon, Text: "1", Tooltip: "Slack"}}, false},
		{"icon", []Item{{Text: "▶1m"}, {Text: "1", Tooltip: "Slack"}}, false},
		{"tooltip", []Item{{Text: "▶1m"}, {Icon: icon, Text: "1"}}, false},
		{"length", base[:1], false},
	}
	for _, tt := range tests {
		if got := sameItems(base, tt.other); got != tt.want {
			t.Errorf("%s: sameItems = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestContentWidth(t *testing.T) {
	icon := testIcon(t)
	// Every rune measures 10.
	measure := func(s string) float32 { return float32(10 * len([]rune(s))) }
	tests := []struct {
		name  string
		items []Item
		want  float32
	}{
		{"empty", nil, minWidth},
		{"text", []Item{{Text: "abc"}}, 30 + slack + 2*padding},
		{"icon", []Item{{Icon: icon}}, iconSize + 2*padding},
		{"icon and text", []Item{{Icon: icon, Text: "•"}}, iconSize + iconGap + 10 + slack + 2*padding},
		{"three", []Item{{Text: "abc"}, {Icon: icon, Text: "1"}, {Icon: icon}},
			30 + slack + itemGap + iconSize + iconGap + 10 + slack + itemGap + iconSize + 2*padding},
	}
	for _, tt := range tests {
		if got := contentWidth(tt.items, measure); got != tt.want {
			t.Errorf("%s: contentWidth = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// testIcon is a 1x1 image.
func testIcon(t *testing.T) ui.ImageSource {
	t.Helper()
	svg, err := ui.ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	return svg
}

// sliceSection returns the items it points to, which a test changes.
type sliceSection struct{ items *[]Item }

func (s sliceSection) Items() []Item { return *s.items }

func TestInvalidateTracksVisibility(t *testing.T) {
	// No windows: a Bar made without New has no display to show on.
	var items []Item
	b := &Bar{}
	if b.shows() {
		t.Fatal("shows before any section, want hidden")
	}
	b.Add(sliceSection{&items})
	if b.shows() {
		t.Fatal("shows without items, want hidden")
	}
	items = []Item{{Text: "▶1m"}}
	b.Invalidate()
	if !b.shows() {
		t.Fatal("hidden with an item, want shown")
	}
	// Empty items do not count.
	items = []Item{{}}
	b.Invalidate()
	if b.shows() {
		t.Fatal("shows with only an empty item, want hidden")
	}
}

func TestRightClickWithoutMenuClicks(t *testing.T) {
	var clicks int
	it := Item{Text: "badge", OnClick: func() { clicks++ }}
	tester := ui.NewTester(func(c *ui.Context) { itemView(c, it) }, 200, 40)
	if err := tester.Click("badge"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Fatalf("left click: %d clicks, want 1", clicks)
	}
	if err := tester.RightClick("badge"); err != nil {
		t.Fatal(err)
	}
	if clicks != 2 {
		t.Fatalf("right click: %d clicks, want 2", clicks)
	}
}

func TestRightClickWithMenuOpensIt(t *testing.T) {
	var clicks int
	it := Item{
		Text:    "timer",
		OnClick: func() { clicks++ },
		Menu:    func(m *ui.Menu) { m.Item("Rest") },
	}
	tester := ui.NewTester(func(c *ui.Context) { itemView(c, it) }, 200, 40)
	if err := tester.RightClick("timer"); err != nil {
		t.Fatal(err)
	}
	if clicks != 0 {
		t.Errorf("right click: %d clicks, want none", clicks)
	}
	if menu := tester.Menu(); len(menu) != 1 || menu[0] != "Rest" {
		t.Errorf("menu = %q, want Rest", menu)
	}
}
