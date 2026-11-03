package windowrules

import (
	"errors"
	"maps"
	"slices"
	"testing"
)

// fakeInspector answers from tables and records which windows and apps
// it was asked about. apps defaults to testApps, so the many cases that
// pin Slack need not repeat its Accessibility windows.
type fakeInspector struct {
	infos      map[uint32]Info
	failed     map[uint32]bool
	asked      []uint32
	apps       map[int32][]AppWindow
	appsFailed map[int32]bool
	askedApps  []int32
}

func (f *fakeInspector) Inspect(w CGWindow) (Info, error) {
	f.asked = append(f.asked, w.ID)
	if f.failed[w.ID] {
		return Info{}, errors.New("AX timeout")
	}
	return f.infos[w.ID], nil
}

func (f *fakeInspector) AppWindows(pid int32) ([]AppWindow, error) {
	f.askedApps = append(f.askedApps, pid)
	if f.appsFailed[pid] {
		return nil, errors.New("AX timeout")
	}
	apps := f.apps
	if apps == nil {
		apps = testApps
	}
	return apps[pid], nil
}

// queries counts every call made to f.
func (f *fakeInspector) queries() int { return len(f.asked) + len(f.askedApps) }

var (
	portrait = Display{
		Frame:   Rect{X: 1920, Y: 0, W: 1080, H: 1920},
		Visible: Rect{X: 1920, Y: 25, W: 1080, H: 1895},
	}
	laptop = Display{
		Frame:   Rect{X: 0, Y: 0, W: 1512, H: 982},
		Visible: Rect{X: 0, Y: 33, W: 1512, H: 949},
	}
	desk = []Display{benq, portrait}

	benqSize     = Size{1920, 1280}
	portraitSize = Size{1080, 1920}

	// testConfig is config.lua: Slack and Yab pinned on the BenQ, Trello
	// on the portrait display, a 66pt left margin on the BenQ.
	testConfig = Config{
		Pins: []Rule{
			{App: "Slack", Screen: &benqSize, Anchor: TopLeft, Oldest: true,
				Width: Range{0.7, 0.9}, Height: Range{0.5, 0.8}},
			{App: "Yab", Screen: &benqSize, Anchor: BottomLeft, FullScreenable: true,
				Width: Range{0.7, 0.9}, Height: Range{0.5, 0.8}},
			{App: "Trello", Screen: &portraitSize, Anchor: BottomLeft,
				Width: Range{0.95, 1}, Height: Range{0.3, 0.4}},
		},
		Margins: []MarginRule{{Screen: benqSize, Margin: 66, Except: []string{"Finder"}}},
	}
)

func standard(title string) Info { return Info{Subrole: standardWindow, Title: title} }

// Slack in place on the BenQ: width 1500 is within 1344-1728, height 800
// within 597.5-956.
var (
	slackMainAt = Rect{0, 30, 1500, 800}
	slackMain   = CGWindow{ID: 102, PID: 10, Owner: "Slack", Bounds: slackMainAt}
	slackHuddle = CGWindow{ID: 3483, PID: 10, Owner: "Slack", Bounds: Rect{0, 500, 400, 300}}
	yabMain     = CGWindow{ID: 301, PID: 20, Owner: "Yab", Bounds: Rect{0, 1225 - 700, 1500, 700}}
	yabGeneral  = CGWindow{ID: 302, PID: 20, Owner: "Yab", Bounds: Rect{10, 400, 600, 500}}
	kitty       = CGWindow{ID: 401, PID: 30, Owner: "kitty", Bounds: Rect{100, 30, 800, 600}}
	finder      = CGWindow{ID: 501, PID: 40, Owner: "Finder", Bounds: Rect{0, 100, 800, 600}}

	testInfos = map[uint32]Info{
		102:  standard("general - f2pool - Slack [Main]"),
		3483: standard("general - f2pool - Slack"),
		301:  {Subrole: standardWindow, Title: "Yab", FullScreenButton: true},
		302:  standard("General"),
		401:  standard("zsh"),
		501:  standard("Downloads"),
		601:  standard("Board"),
		701:  {Subrole: "AXDialog", Title: "Save"},
		801:  {Subrole: standardWindow, Minimized: true},
	}

	// testApps is what AppWindows says: Slack's main window, minimized
	// when CoreGraphics does not list it, next to the huddle popup.
	testApps = map[int32][]AppWindow{
		10: {
			{ID: 102, Info: Info{Subrole: standardWindow, Title: "general - f2pool - Slack [Main]", Minimized: true}},
			{ID: 3483, Info: testInfos[3483]},
		},
	}
)

func at(w CGWindow, r Rect) CGWindow {
	w.Bounds = r
	return w
}

// moveTargets maps each moved window to its target.
func moveTargets(moves []Move) map[uint32]Rect {
	out := map[uint32]Rect{}
	for _, m := range moves {
		out[m.Window.ID] = m.To
	}
	return out
}

func TestPlan(t *testing.T) {
	tests := []struct {
		name      string
		windows   []CGWindow
		displays  []Display
		failed    map[uint32]bool
		apps      map[int32][]AppWindow
		appsFail  map[int32]bool
		wantMoves map[uint32]Rect
		wantAsked []uint32
		// wantApps is the pids AppWindows was asked about.
		wantApps []int32
	}{
		{
			name:      "Slack 已在位不动",
			windows:   []CGWindow{slackMain},
			displays:  desk,
			wantMoves: map[uint32]Rect{},
			wantAsked: []uint32{102},
			wantApps:  []int32{10},
		},
		{
			name:      "Slack 拖离左上角归位",
			windows:   []CGWindow{at(slackMain, Rect{300, 200, 1500, 800})},
			displays:  desk,
			wantMoves: map[uint32]Rect{102: slackMainAt},
			wantAsked: []uint32{102},
			wantApps:  []int32{10},
		},
		{
			name:      "Slack 铺满可用区夹到上限",
			windows:   []CGWindow{at(slackMain, Rect{0, 30, 1920, 1195})},
			displays:  desk,
			wantMoves: map[uint32]Rect{102: {0, 30, 1728, 956}},
			wantAsked: []uint32{102},
			wantApps:  []int32{10},
		},
		{
			name:      "Slack 在副屏搬到规则屏",
			windows:   []CGWindow{at(slackMain, Rect{2000, 100, 1000, 800})},
			displays:  desk,
			wantMoves: map[uint32]Rect{102: {0, 30, 1344, 800}},
			wantAsked: []uint32{102},
			wantApps:  []int32{10},
		},
		{
			name:      "huddle 弹窗不钉，只右移 66",
			windows:   []CGWindow{slackMain, slackHuddle},
			displays:  desk,
			wantMoves: map[uint32]Rect{3483: {66, 500, 400, 300}},
			wantAsked: []uint32{102, 3483},
			wantApps:  []int32{10},
		},
		{
			name:      "主窗口全屏被 CG 预过滤：app 整轮不钉，huddle 只右移 66",
			windows:   []CGWindow{at(slackMain, benq.Frame), slackHuddle},
			displays:  desk,
			wantMoves: map[uint32]Rect{3483: {66, 500, 400, 300}},
			wantAsked: []uint32{3483},
			wantApps:  []int32{10},
		},
		{
			name:      "主窗口最小化，屏上只剩 huddle：不钉，只右移 66",
			windows:   []CGWindow{slackHuddle},
			displays:  desk,
			wantMoves: map[uint32]Rect{3483: {66, 500, 400, 300}},
			wantAsked: []uint32{3483},
			wantApps:  []int32{10},
		},
		{
			name:      "AX 查 app 窗口失败：不钉，只右移 66",
			windows:   []CGWindow{slackHuddle},
			displays:  desk,
			appsFail:  map[int32]bool{10: true},
			wantMoves: map[uint32]Rect{3483: {66, 500, 400, 300}},
			wantAsked: []uint32{3483},
			wantApps:  []int32{10},
		},
		{
			name:      "AX 里也只有这一个窗口：它就是最老的，钉住",
			windows:   []CGWindow{slackHuddle},
			displays:  desk,
			apps:      map[int32][]AppWindow{10: {{ID: 3483, Info: testInfos[3483]}}},
			wantMoves: map[uint32]Rect{3483: {0, 30, 1344, 597.5}},
			wantAsked: []uint32{3483},
			wantApps:  []int32{10},
		},
		{
			name:      "Yab General 无全屏按钮不钉，只走左边距",
			windows:   []CGWindow{yabMain, yabGeneral},
			displays:  desk,
			wantMoves: map[uint32]Rect{302: {66, 400, 600, 500}},
			wantAsked: []uint32{301, 302},
		},
		{
			name:      "Trello 钉在竖屏左下角",
			windows:   []CGWindow{{ID: 601, PID: 50, Owner: "Trello", Bounds: Rect{1920, 25, 1080, 1895}}},
			displays:  desk,
			wantMoves: map[uint32]Rect{601: {1920, 25 + 1895 - 1895*0.4, 1080, 1895 * 0.4}},
			wantAsked: []uint32{601},
		},
		{
			name:      "拔掉 BenQ 规则静默跳过",
			windows:   []CGWindow{at(slackMain, Rect{0, 33, 1512, 949}), at(finder, Rect{0, 33, 800, 600})},
			displays:  []Display{laptop},
			wantMoves: map[uint32]Rect{},
			wantAsked: nil,
		},
		{
			name:      "守边距的窗口不开 AX",
			windows:   []CGWindow{kitty},
			displays:  desk,
			wantMoves: map[uint32]Rect{},
			wantAsked: nil,
		},
		{
			name:      "except 里的 app 不开 AX",
			windows:   []CGWindow{finder},
			displays:  desk,
			wantMoves: map[uint32]Rect{},
			wantAsked: nil,
		},
		{
			name:      "全屏窗口不开 AX",
			windows:   []CGWindow{at(slackMain, benq.Frame), at(kitty, benq.Frame)},
			displays:  desk,
			wantMoves: map[uint32]Rect{},
			wantAsked: nil,
		},
		{
			name:      "副屏上的窗口不受左边距约束",
			windows:   []CGWindow{at(kitty, Rect{1920, 100, 800, 600})},
			displays:  desk,
			wantMoves: map[uint32]Rect{},
			wantAsked: nil,
		},
		{
			name: "越界的对话框和最小化窗口不动",
			windows: []CGWindow{
				{ID: 701, PID: 60, Owner: "TextEdit", Bounds: Rect{0, 100, 400, 300}},
				{ID: 801, PID: 60, Owner: "TextEdit", Bounds: Rect{0, 500, 400, 300}},
			},
			displays:  desk,
			wantMoves: map[uint32]Rect{},
			wantAsked: []uint32{701, 801},
		},
		{
			name:      "AX 失败的窗口不动",
			windows:   []CGWindow{at(kitty, Rect{0, 100, 800, 600})},
			displays:  desk,
			failed:    map[uint32]bool{401: true},
			wantMoves: map[uint32]Rect{},
			wantAsked: []uint32{401},
		},
		{
			name:      "越界普通窗口右移",
			windows:   []CGWindow{at(kitty, Rect{0, 100, 800, 600})},
			displays:  desk,
			wantMoves: map[uint32]Rect{401: {66, 100, 800, 600}},
			wantAsked: []uint32{401},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insp := &fakeInspector{infos: testInfos, failed: tt.failed, apps: tt.apps, appsFailed: tt.appsFail}
			moves, _, stats := Plan(testConfig, tt.windows, tt.displays, nil, insp)
			if got := moveTargets(moves); !maps.Equal(got, tt.wantMoves) {
				t.Fatalf("moves = %+v, want %+v", got, tt.wantMoves)
			}
			if !slices.Equal(insp.asked, tt.wantAsked) {
				t.Fatalf("asked = %+v, want %+v", insp.asked, tt.wantAsked)
			}
			if !slices.Equal(insp.askedApps, tt.wantApps) {
				t.Fatalf("askedApps = %+v, want %+v", insp.askedApps, tt.wantApps)
			}
			if stats.Inspects != insp.queries() {
				t.Fatalf("stats.Inspects = %+v, want %+v", stats.Inspects, insp.queries())
			}
		})
	}
}

// An oldest rule with fullScreenable takes the oldest among the windows
// with a full screen button: an older window without one, such as a
// welcome or settings window, must not keep the main window from being
// pinned.
func TestPlanOldestAfterFilters(t *testing.T) {
	yabWelcome := CGWindow{ID: 300, PID: 20, Owner: "Yab", Bounds: Rect{10, 400, 600, 500}}
	welcomeInfo := standard("Welcome")
	infos := maps.Clone(testInfos)
	infos[yabWelcome.ID] = welcomeInfo
	insp := &fakeInspector{
		infos: infos,
		apps: map[int32][]AppWindow{20: {
			{ID: yabWelcome.ID, Info: welcomeInfo},
			{ID: yabMain.ID, Info: testInfos[yabMain.ID]},
		}},
	}
	rule := Rule{App: "Yab", Screen: &benqSize, Anchor: BottomLeft, FullScreenable: true, Oldest: true,
		Width: Range{0.7, 0.9}, Height: Range{0.5, 0.8}}
	windows := []CGWindow{yabWelcome, at(yabMain, Rect{200, 200, 1500, 700})}
	moves, _, _ := Plan(Config{Pins: []Rule{rule}}, windows, desk, nil, insp)
	want := map[uint32]Rect{yabMain.ID: yabMain.Bounds}
	if got := moveTargets(moves); !maps.Equal(got, want) {
		t.Fatalf("moves = %+v, want %+v", got, want)
	}
	if !slices.Equal(insp.askedApps, []int32{20}) {
		t.Fatalf("askedApps = %+v, want %+v", insp.askedApps, []int32{20})
	}
}

// A pin rule without a screen keeps the window on its own display.
func TestPlanRuleWithoutScreen(t *testing.T) {
	cfg := Config{Pins: []Rule{{App: "kitty", Anchor: TopLeft, Width: Range{0.5, 0.9}, Height: Range{0.3, 0.9}}}}
	w := at(kitty, Rect{2100, 300, 800, 600})
	moves, _, _ := Plan(cfg, []CGWindow{w}, desk, nil, &fakeInspector{infos: testInfos})
	want := map[uint32]Rect{401: {1920, 25, 800, 600}}
	if got := moveTargets(moves); !maps.Equal(got, want) {
		t.Fatalf("moves = %+v, want %+v", got, want)
	}
}

// A pin rule for the BenQ and a fallback without a display: only one
// runs, so the window moves once, to the BenQ when it is connected and
// into place on the laptop otherwise.
func TestPlanFallbackPinMovesOnce(t *testing.T) {
	base := Rule{App: "kitty", Width: Range{0.5, 0.9}, Height: Range{0.3, 0.9}}
	docked, fallback := base, base
	docked.Screen, docked.Anchor = &benqSize, TopLeft
	fallback.Anchor = BottomLeft
	cfg := Config{Pins: []Rule{docked, fallback}}
	tests := []struct {
		name     string
		displays []Display
		from     Rect
		want     Rect
	}{
		{"docked", desk, Rect{2100, 300, 1000, 600}, Rect{0, 30, 1000, 600}},
		{"laptop alone", []Display{laptop}, Rect{100, 100, 1000, 600}, Rect{0, 982 - 600, 1000, 600}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := at(kitty, tt.from)
			moves, _, _ := Plan(cfg, []CGWindow{w}, tt.displays, nil, &fakeInspector{infos: testInfos})
			if len(moves) != 1 || moves[0].To != tt.want {
				t.Fatalf("moves = %+v, want one to %+v", moves, tt.want)
			}
		})
	}
}

// Two margin rules on displays of the same size: the second sees where
// the first put the window.
func TestPlanLaterRulesSeeMovedFrames(t *testing.T) {
	cfg := Config{Margins: []MarginRule{{Screen: benqSize, Margin: 66}, {Screen: benqSize, Margin: 100}}}
	w := at(kitty, Rect{0, 100, 1920, 600})
	moves, _, _ := Plan(cfg, []CGWindow{w}, desk, nil, &fakeInspector{infos: testInfos})
	if len(moves) != 2 {
		t.Fatalf("len(moves) = %+v, want %+v", len(moves), 2)
	}
	want := Rect{100, 100, 1820, 600}
	if got := moves[1].To; got != want {
		t.Fatalf("second move = %+v, want %+v", got, want)
	}
}

// TestPlanCache runs rounds one after another, feeding each round the
// cache of the one before, and checks Accessibility is only asked about
// apps whose windows changed.
func TestPlanCache(t *testing.T) {
	huddleOff := at(slackHuddle, Rect{66, 500, 400, 300})
	rounds := []struct {
		name      string
		windows   []CGWindow
		failed    map[uint32]bool
		wantAsked []uint32
		wantMoves int
	}{
		{"首轮全部 inspect", []CGWindow{slackMain, yabMain, kitty}, nil, []uint32{102, 301}, 0},
		{"稳态零 AX", []CGWindow{slackMain, yabMain, kitty}, nil, nil, 0},
		{"再一轮仍零 AX", []CGWindow{slackMain, yabMain, kitty}, nil, nil, 0},
		{"开 huddle：只重查 Slack", []CGWindow{slackMain, huddleOff, yabMain, kitty}, nil, []uint32{102, 3483}, 0},
		{"huddle 稳态零 AX", []CGWindow{slackMain, huddleOff, yabMain, kitty}, nil, nil, 0},
		{"拖动 Yab：只重查 Yab", []CGWindow{slackMain, huddleOff, at(yabMain, Rect{200, 200, 1500, 700}), kitty}, nil, []uint32{301}, 1},
		{"未变的 bounds 沿用结论再移一次", []CGWindow{slackMain, huddleOff, at(yabMain, Rect{200, 200, 1500, 700}), kitty}, nil, nil, 1},
		{"Yab 归位后 bounds 变化重查一次", []CGWindow{slackMain, huddleOff, yabMain, kitty}, nil, []uint32{301}, 0},
		{"关 huddle：Slack 重查", []CGWindow{slackMain, yabMain, kitty}, nil, []uint32{102}, 0},
		{"kitty 越界但 AX 失败", []CGWindow{slackMain, yabMain, at(kitty, Rect{0, 100, 800, 600})}, map[uint32]bool{401: true}, []uint32{401}, 0},
		{"失败结果也缓存", []CGWindow{slackMain, yabMain, at(kitty, Rect{0, 100, 800, 600})}, map[uint32]bool{401: true}, nil, 0},
		{"窗口消失后 app 从缓存删除", []CGWindow{slackMain, yabMain}, nil, nil, 0},
		{"窗口回来重新 inspect", []CGWindow{slackMain, yabMain, at(kitty, Rect{0, 100, 800, 600})}, nil, []uint32{401}, 1},
	}
	var cache Cache
	for _, r := range rounds {
		insp := &fakeInspector{infos: testInfos, failed: r.failed}
		var moves []Move
		var stats Stats
		moves, cache, stats = Plan(testConfig, r.windows, desk, cache, insp)
		if !slices.Equal(insp.asked, r.wantAsked) {
			t.Fatalf("%s: asked = %+v, want %+v", r.name, insp.asked, r.wantAsked)
		}
		if stats.Inspects != insp.queries() {
			t.Fatalf("%s: stats.Inspects = %+v, want %+v", r.name, stats.Inspects, insp.queries())
		}
		if len(moves) != r.wantMoves {
			t.Fatalf("%s: moves = %+v, want %+v", r.name, moves, r.wantMoves)
		}
	}
}

// TestPlanRetriesFailures runs rounds over an unchanged desktop where
// Accessibility fails, and checks a failure is asked again only after
// retryFailedAfter rounds, while a definite answer is never asked again.
func TestPlanRetriesFailures(t *testing.T) {
	strayKitty := at(kitty, Rect{0, 100, 800, 600})
	tests := []struct {
		name     string
		windows  []CGWindow
		insp     fakeInspector
		wantWins bool
		wantApps bool
		// wantMoves is how many moves every round makes: a cached failure
		// must give the same plan as the fresh one.
		wantMoves int
	}{
		{
			name:     "Inspect 失败",
			windows:  []CGWindow{strayKitty},
			insp:     fakeInspector{infos: testInfos, failed: map[uint32]bool{401: true}},
			wantWins: true,
		},
		{
			name:     "AppWindows 失败",
			windows:  []CGWindow{slackHuddle},
			insp:     fakeInspector{infos: testInfos, appsFailed: map[int32]bool{10: true}},
			wantApps: true,
			// The huddle is not pinned, so the margin rule moves it.
			wantMoves: 1,
		},
		{
			name:     "确定结果（无 subrole）不过期",
			windows:  []CGWindow{strayKitty},
			insp:     fakeInspector{infos: map[uint32]Info{401: {Title: "zsh"}}},
			wantWins: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cache Cache
			for round := range retryFailedAfter + 3 {
				insp := tt.insp
				insp.asked, insp.askedApps = nil, nil
				moves, next, _ := Plan(testConfig, tt.windows, desk, cache, &insp)
				cache = next
				first := round == 0
				retry := round == retryFailedAfter+1
				wantWins := first || (tt.wantWins && retry)
				wantApps := tt.wantApps && (first || retry)
				if got := len(insp.asked) > 0; got != wantWins {
					t.Fatalf("round %d: asked = %+v, want asked %v", round, insp.asked, wantWins)
				}
				if got := len(insp.askedApps) > 0; got != wantApps {
					t.Fatalf("round %d: askedApps = %+v, want asked %v", round, insp.askedApps, wantApps)
				}
				if len(moves) != tt.wantMoves {
					t.Fatalf("round %d: moves = %+v, want %d", round, moves, tt.wantMoves)
				}
			}
		})
	}
}

// TestPlanCachesAppWindows checks AppWindows is asked once per app
// snapshot, like Inspect.
func TestPlanCachesAppWindows(t *testing.T) {
	rounds := []struct {
		name     string
		windows  []CGWindow
		wantApps []int32
	}{
		{"首轮查 app 窗口", []CGWindow{slackHuddle}, []int32{10}},
		{"快照不变不再查", []CGWindow{slackHuddle}, nil},
		{"主窗口回到屏上：快照变了重查", []CGWindow{slackMain, slackHuddle}, []int32{10}},
		{"主窗口在屏上稳态不再查", []CGWindow{slackMain, slackHuddle}, nil},
		{"主窗口再离开重新查", []CGWindow{slackHuddle}, []int32{10}},
	}
	var cache Cache
	for _, r := range rounds {
		insp := &fakeInspector{infos: testInfos}
		_, cache, _ = Plan(testConfig, r.windows, desk, cache, insp)
		if !slices.Equal(insp.askedApps, r.wantApps) {
			t.Fatalf("%s: askedApps = %+v, want %+v", r.name, insp.askedApps, r.wantApps)
		}
	}
}

// Plan must not change the cache it is given, so a caller may keep it.
func TestPlanKeepsPrevCache(t *testing.T) {
	windows := []CGWindow{slackMain, at(kitty, Rect{0, 100, 800, 600})}
	_, prev, _ := Plan(Config{Pins: testConfig.Pins}, windows, desk, nil, &fakeInspector{infos: testInfos})
	before := len(prev[kitty.PID].infos)
	Plan(testConfig, windows, desk, prev, &fakeInspector{infos: testInfos})
	if after := len(prev[kitty.PID].infos); after != before {
		t.Fatalf("prev infos = %+v, want %+v", after, before)
	}
}

func TestMoveString(t *testing.T) {
	m := Move{Window: slackHuddle, To: Rect{66, 500, 400, 300}, Title: "huddle", Margin: true}
	want := "Slack moved right by the left margin to (66,500 400x300) (huddle)"
	if got := m.String(); got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
}

// A margin move sets only the position unless the window has to narrow;
// a pin move always sets the size.
func TestPlanPositionOnly(t *testing.T) {
	tests := []struct {
		name string
		w    CGWindow
		want Move
	}{
		{"左边距不超右缘只设位置", at(kitty, Rect{0, 100, 442, 333}),
			Move{To: Rect{66, 100, 442, 333}, Margin: true, PositionOnly: true}},
		{"左边距超出右缘要缩宽", at(kitty, Rect{0, 100, 1920, 600}),
			Move{To: Rect{66, 100, 1854, 600}, Margin: true}},
		{"pin 设大小", at(slackMain, Rect{200, 30, 1500, 800}),
			Move{To: slackMainAt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moves, _, _ := Plan(testConfig, []CGWindow{tt.w}, desk, nil, &fakeInspector{infos: testInfos})
			if len(moves) != 1 {
				t.Fatalf("moves = %+v, want one", moves)
			}
			got := moves[0]
			sameMove := got.To == tt.want.To && got.Margin == tt.want.Margin && got.PositionOnly == tt.want.PositionOnly
			if !sameMove {
				t.Fatalf("move = %+v, want %+v", got, tt.want)
			}
		})
	}
}
