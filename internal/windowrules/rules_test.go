package windowrules

import (
	"regexp"
	"slices"
	"testing"
)

// benqAvail is the usable area of a BenQ RD280U with the Dock: the
// 1920x1280 full frame without the 30pt menu bar and the 55pt Dock.
var benqAvail = Rect{X: 0, Y: 30, W: 1920, H: 1195}

var benq = Display{Frame: Rect{X: 0, Y: 0, W: 1920, H: 1280}, Visible: benqAvail}

// Translated from init_test.lua. With the usable area 1920x1195 the width
// range is 960 to 1440 and the height range 597.5 to 896.25.
func TestExpectedFrame(t *testing.T) {
	r := Range{Min: 0.5, Max: 0.75}
	topLeft := Rule{Anchor: TopLeft, Width: r, Height: r}
	bottomLeft := Rule{Anchor: BottomLeft, Width: r, Height: r}
	tests := []struct {
		name string
		rule Rule
		win  Rect
		want Rect
	}{
		{"topLeft 范围内", topLeft, Rect{300, 200, 1200, 700}, Rect{0, 30, 1200, 700}},
		{"bottomLeft 范围内", bottomLeft, Rect{300, 200, 1200, 700}, Rect{0, 30 + 1195 - 700, 1200, 700}},
		{"topLeft 铺满夹到 max", topLeft, Rect{0, 30, 1920, 1250}, Rect{0, 30, 1440, 896.25}},
		{"bottomLeft 铺满夹到 max", bottomLeft, Rect{0, 30, 1920, 1250}, Rect{0, 30 + 1195 - 896.25, 1440, 896.25}},
		{"topLeft 太小补到 min", topLeft, Rect{0, 30, 400, 300}, Rect{0, 30, 960, 597.5}},
		{"宽超高在范围内", topLeft, Rect{0, 30, 1920, 700}, Rect{0, 30, 1440, 700}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpectedFrame(tt.rule, benqAvail, tt.win); got != tt.want {
				t.Fatalf("ExpectedFrame = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLeftMarginFrame(t *testing.T) {
	rule := MarginRule{Margin: 66}
	tests := []struct {
		name   string
		win    Rect
		want   Rect
		wantOK bool
	}{
		{"x 已够不动", Rect{100, 30, 800, 600}, Rect{}, false},
		{"x 刚好 66 不动", Rect{66, 30, 800, 600}, Rect{}, false},
		// No tolerance: a point short still moves.
		{"x=65 差一点也右移", Rect{65, 30, 800, 600}, Rect{66, 30, 800, 600}, true},
		{"x=0 整体右移", Rect{0, 100, 800, 600}, Rect{66, 100, 800, 600}, true},
		{"铺满窗口右移并收窄", Rect{0, 30, 1920, 1195}, Rect{66, 30, 1854, 1195}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LeftMarginFrame(rule, benqAvail, tt.win)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("LeftMarginFrame = %+v, %v, want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func fakeWin(id uint32, title, app string, fullScreenButton bool) Window {
	return Window{
		CG:   CGWindow{ID: id, Owner: app},
		Info: Info{Title: title, FullScreenButton: fullScreenButton},
	}
}

func ids(wins []Window) []uint32 {
	out := []uint32{}
	for _, w := range wins {
		out = append(out, w.CG.ID)
	}
	return out
}

func TestSelectWindows(t *testing.T) {
	mainRe := regexp.MustCompile(`\[Main\]`)
	slackMain := fakeWin(102, "dev-private (Channel) - f2pool - Slack [Main] 🏠🔊", "Slack", false)
	slackHuddle := fakeWin(3483, "dev-private - f2pool - 1 new item - Slack", "Slack", false)
	slackAlone := fakeWin(200, "dev-private (Channel) - f2pool - Slack", "Slack", false)
	yabMain := fakeWin(301, "Yab", "Yab", true)
	yabSettings := fakeWin(302, "General", "Yab", false)
	// A window whose inspection failed has the zero Info: no title and no
	// full screen button.
	yabNoElement := Window{CG: CGWindow{ID: 301, Owner: "Yab"}}
	tests := []struct {
		name string
		rule Rule
		wins []Window
		want []uint32
	}{
		{"mainTitle：有 [Main] 只留主窗口", Rule{App: "Slack", MainTitle: mainRe},
			[]Window{slackHuddle, slackMain}, []uint32{102}},
		{"mainTitle：没有 [Main] 全部保留", Rule{App: "Slack", MainTitle: mainRe},
			[]Window{slackAlone}, []uint32{200}},
		{"无 mainTitle：全部保留", Rule{App: "Arc"},
			[]Window{slackHuddle, slackMain}, []uint32{3483, 102}},
		{"title 与 mainTitle 叠加：title 先过滤",
			Rule{App: "Slack", Title: regexp.MustCompile("Channel"), MainTitle: mainRe},
			[]Window{slackHuddle, slackMain}, []uint32{102}},
		{"fullScreenable：只留有全屏按钮的窗口", Rule{App: "Yab", FullScreenable: true},
			[]Window{yabSettings, yabMain}, []uint32{301}},
		{"无 fullScreenable：全部保留", Rule{App: "Yab"},
			[]Window{yabSettings, yabMain}, []uint32{302, 301}},
		{"fullScreenable 与 mainTitle 叠加：无全屏按钮的窗口标题匹配也不返回",
			Rule{App: "Yab", FullScreenable: true, MainTitle: regexp.MustCompile("General")},
			[]Window{yabSettings, yabMain}, []uint32{301}},
		{"fullScreenable：拿不到 AX 元素的窗口被过滤，不报错", Rule{App: "Yab", FullScreenable: true},
			[]Window{yabSettings, yabNoElement}, []uint32{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(SelectWindows(tt.rule, tt.wins)); !slices.Equal(got, tt.want) {
				t.Fatalf("SelectWindows = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsExcepted(t *testing.T) {
	exceptRule := MarginRule{Except: []string{"Finder"}}
	handled := map[uint32]bool{102: true}
	slackMain := CGWindow{ID: 102, Owner: "Slack"}
	slackHuddle := CGWindow{ID: 3483, Owner: "Slack"}
	finder := CGWindow{ID: 7, Owner: "Finder"}
	tests := []struct {
		name    string
		rule    MarginRule
		win     CGWindow
		handled map[uint32]bool
		want    bool
	}{
		{"规则接管的窗口放过", exceptRule, slackMain, handled, true},
		{"huddle 未接管不放过", exceptRule, slackHuddle, handled, false},
		{"列表里的 app 放过", exceptRule, finder, handled, true},
		{"无 except 字段不放过", MarginRule{}, slackHuddle, map[uint32]bool{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExcepted(tt.rule, tt.win, tt.handled); got != tt.want {
				t.Fatalf("IsExcepted = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The Lua findApp guarded against hs.application.find's fuzzy match; the
// CoreGraphics owner name is matched exactly instead.
func TestWindowsOfApp(t *testing.T) {
	trello := CGWindow{ID: 1, Owner: "Trello"}
	autofill := CGWindow{ID: 2, Owner: "自动填充 (Trello)"}
	tests := []struct {
		name string
		wins []CGWindow
		want []CGWindow
	}{
		{"跳过名字不完全相同的", []CGWindow{autofill, trello}, []CGWindow{trello}},
		{"只有模糊匹配返回空", []CGWindow{autofill}, nil},
		{"没找到返回空", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WindowsOfApp("Trello", tt.wins); !slices.Equal(got, tt.want) {
				t.Fatalf("WindowsOfApp = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSameRectTolerance(t *testing.T) {
	base := Rect{0, 30, 1200, 700}
	tests := []struct {
		name string
		r    Rect
		want bool
	}{
		{"相同", base, true},
		{"差 5 算相同", Rect{5, 25, 1205, 695}, true},
		{"差 6 不同", Rect{6, 30, 1200, 700}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameRect(base, tt.r); got != tt.want {
				t.Fatalf("SameRect = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFindScreenBySizeAndScreenOf(t *testing.T) {
	portrait := Display{Frame: Rect{X: 1920, Y: -300, W: 1080, H: 1920}}
	displays := []Display{benq, portrait}
	if got := FindScreenBySize(Size{1080, 1920}, displays); got != &displays[1] {
		t.Fatalf("FindScreenBySize = %+v, want %+v", got, &displays[1])
	}
	if got := FindScreenBySize(Size{1083, 1917}, displays); got != &displays[1] {
		t.Fatalf("FindScreenBySize within tolerance = %+v, want %+v", got, &displays[1])
	}
	if got := FindScreenBySize(Size{1512, 982}, displays); got != nil {
		t.Fatalf("FindScreenBySize missing = %+v, want nil", got)
	}
	// Mostly on the portrait display.
	if got := ScreenOf(Rect{1800, 0, 800, 600}, displays); got != &displays[1] {
		t.Fatalf("ScreenOf = %+v, want %+v", got, &displays[1])
	}
	if got := ScreenOf(Rect{-5000, 0, 100, 100}, displays); got != nil {
		t.Fatalf("ScreenOf off screen = %+v, want nil", got)
	}
}

func TestActivePins(t *testing.T) {
	kitty := Rule{App: "kitty"}
	onBenq := Rule{App: "Slack", Screen: &benqSize}
	onPortrait := Rule{App: "Slack", Screen: &portraitSize}
	anywhere := Rule{App: "Slack"}
	pins := []Rule{onBenq, onPortrait, kitty, anywhere}
	tests := []struct {
		name     string
		displays []Display
		want     []Rule
	}{
		{"desk", desk, []Rule{onBenq, kitty}},
		{"portrait only", []Display{portrait}, []Rule{onPortrait, kitty}},
		{"laptop alone", []Display{laptop}, []Rule{kitty, anywhere}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ActivePins(pins, tt.displays); !slices.EqualFunc(got, tt.want, sameRule) {
				t.Fatalf("ActivePins = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func sameRule(a, b Rule) bool { return a.App == b.App && a.Screen == b.Screen }
