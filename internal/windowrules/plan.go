package windowrules

import (
	"cmp"
	"fmt"
	"slices"
)

// Inspector asks Accessibility about one window. It is only called for
// windows whose app changed since the last round, so a steady desktop
// costs no Accessibility IPC.
type Inspector interface {
	// Inspect returns the Info of w. It takes the whole CGWindow rather
	// than its pid and ID because, without _AXUIElementGetWindow, the
	// window is found among the app's Accessibility windows by bounds.
	Inspect(w CGWindow) (Info, error)
	// AppWindows returns the Info of every Accessibility window of pid,
	// minimized ones included, which the CoreGraphics list of windows on
	// screen leaves out.
	AppWindows(pid int32) ([]Info, error)
}

// retryFailedAfter is how many rounds a failed Accessibility answer is
// kept before it is asked again, about 30s at DefaultInterval. A failure
// is often passing (an app busy starting up, a window CoreGraphics lists
// before Accessibility does), so it must not hold for as long as the
// app's windows stay put; but an app that does not answer at all should
// not cost its timeout on every round either.
const retryFailedAfter = 10

// Move is one window to put at a new frame.
type Move struct {
	Window CGWindow
	To     Rect
	// Title is the window's title, for the log.
	Title string
	// Margin is set when a margin rule, not a pin rule, moves the window.
	Margin bool
}

// String describes the move for the log, after the Lua module's lines.
func (m Move) String() string {
	to := fmt.Sprintf("(%g,%g %gx%g)", m.To.X, m.To.Y, m.To.W, m.To.H)
	if m.Margin {
		return fmt.Sprintf("%s moved right by the left margin to %s (%s)", m.Window.Owner, to, m.Title)
	}
	return fmt.Sprintf("%s restored to %s (%s)", m.Window.Owner, to, m.Title)
}

// Cache carries what Accessibility said about windows from one round to
// the next, per app. An app's entry holds as long as the app's windows
// and their bounds stay exactly the same; any change drops it, so a new
// huddle popup, a closed window or a drag gets the app inspected afresh.
// The zero value is empty.
type Cache map[int32]*appCache

type appCache struct {
	snapshot []snapWindow
	infos    map[uint32]inspection
	// windows is the app's AppWindows answer, nil until asked.
	windows *appInspection
}

type snapWindow struct {
	id     uint32
	bounds Rect
}

// inspection is an Inspect result. Errors are kept too, for
// retryFailedAfter rounds: an app that does not answer would otherwise
// cost its timeout on every round.
type inspection struct {
	info Info
	err  error
	// age counts the rounds since the inspection; only failed ones age.
	age int
}

// appInspection is an AppWindows result, kept like an inspection.
type appInspection struct {
	infos []Info
	err   error
	age   int
}

// Stats counts the work of one Plan round.
type Stats struct {
	Windows int
	// Inspects counts Accessibility queries, Inspect and AppWindows calls.
	Inspects int
}

// planner is the state of one Plan round.
type planner struct {
	displays []Display
	insp     Inspector
	next     Cache
	// frames is the current frame of each window, updated by moves so
	// later rules see where earlier ones put a window.
	frames  map[uint32]Rect
	handled map[uint32]bool
	moves   []Move
	stats   Stats
}

// Plan decides which windows to move this round. windows is the
// CoreGraphics window list (layer 0, on screen), displays the connected
// displays and prev the cache Plan returned last round. Pin rules run
// first, in order, and mark every window they apply to as handled whether
// or not it moves; margin rules then skip handled windows.
//
// Plan only calls insp for windows of apps whose snapshot changed or
// whose last answer failed more than retryFailedAfter rounds ago, and,
// for margin rules, only for windows that break the margin. It does not
// modify prev.
func Plan(cfg Config, windows []CGWindow, displays []Display, prev Cache, insp Inspector) ([]Move, Cache, Stats) {
	p := &planner{
		displays: displays,
		insp:     insp,
		next:     nextCache(windows, prev),
		frames:   make(map[uint32]Rect, len(windows)),
		handled:  map[uint32]bool{},
		stats:    Stats{Windows: len(windows)},
	}
	for _, w := range windows {
		p.frames[w.ID] = w.Bounds
	}
	for _, rule := range cfg.Pins {
		p.pin(rule, windows)
	}
	for _, rule := range cfg.Margins {
		p.margin(rule, windows)
	}
	return p.moves, p.next, p.stats
}

// nextCache starts this round's cache: an app whose snapshot is unchanged
// keeps its inspections, any other app starts empty. A failed inspection
// ages a round and is dropped once older than retryFailedAfter, so it is
// asked again.
func nextCache(windows []CGWindow, prev Cache) Cache {
	snapshots := map[int32][]snapWindow{}
	for _, w := range windows {
		snapshots[w.PID] = append(snapshots[w.PID], snapWindow{id: w.ID, bounds: w.Bounds})
	}
	next := make(Cache, len(snapshots))
	for pid, snap := range snapshots {
		slices.SortFunc(snap, func(a, b snapWindow) int { return cmp.Compare(a.id, b.id) })
		entry := &appCache{snapshot: snap, infos: map[uint32]inspection{}}
		old := prev[pid]
		unchanged := old != nil && slices.Equal(old.snapshot, snap)
		if unchanged {
			for id, in := range old.infos {
				if !keepAged(&in.age, in.err) {
					continue
				}
				entry.infos[id] = in
			}
			entry.windows = agedAppInspection(old.windows)
		}
		next[pid] = entry
	}
	return next
}

// keepAged reports whether a cached answer carries over to the next
// round, aging it first if it failed. A successful answer always does.
func keepAged(age *int, err error) bool {
	if err == nil {
		return true
	}
	*age++
	return *age <= retryFailedAfter
}

// agedAppInspection carries an AppWindows answer over to the next round,
// or returns nil when there is none or it failed too long ago.
func agedAppInspection(old *appInspection) *appInspection {
	if old == nil {
		return nil
	}
	in := *old
	if !keepAged(&in.age, in.err) {
		return nil
	}
	return &in
}

// info returns what Accessibility says about w, asking only when the
// cache has no answer. A failed inspection reads as the zero Info, which
// is not a normal window, so rules leave the window alone, as the Lua
// module did with windows it got no element for.
func (p *planner) info(w CGWindow) Info {
	entry := p.next[w.PID]
	if in, ok := entry.infos[w.ID]; ok {
		return in.info
	}
	p.stats.Inspects++
	info, err := p.insp.Inspect(w)
	if err != nil {
		info = Info{}
	}
	entry.infos[w.ID] = inspection{info: info, err: err}
	return info
}

func (p *planner) move(w CGWindow, to Rect, title string, margin bool) {
	p.moves = append(p.moves, Move{Window: w, To: to, Title: title, Margin: margin})
	p.frames[w.ID] = to
}

// pin applies a pin rule to the windows of its app.
func (p *planner) pin(rule Rule, windows []CGWindow) {
	var ruleScreen *Display
	if rule.Screen != nil {
		ruleScreen = FindScreenBySize(*rule.Screen, p.displays)
		if ruleScreen == nil {
			return
		}
	}
	appWindows := WindowsOfApp(rule.App, windows)
	var candidates []Window
	for _, w := range appWindows {
		// A full screen window lives in a Space of its own; skip it
		// before paying for Accessibility.
		if isCGFullScreen(p.frames[w.ID], p.displays) {
			continue
		}
		candidates = append(candidates, Window{CG: w, Info: p.info(w)})
	}
	selected := SelectWindows(rule, candidates)
	if p.mainElsewhere(rule, appWindows, selected) {
		return
	}
	for _, w := range selected {
		frame := p.frames[w.CG.ID]
		// The rule's display wins even when the window is on another.
		screen := cmp.Or(ruleScreen, ScreenOf(frame, p.displays))
		if screen == nil || !w.Info.Normal() {
			continue
		}
		p.handled[w.CG.ID] = true
		target := ExpectedFrame(rule, screen.Visible, frame)
		if SameRect(frame, target) {
			continue
		}
		p.move(w.CG, target, w.Info.Title, false)
	}
}

// mainElsewhere reports whether rule has a main window that is not among
// the selected windows, being minimized, in another Space or full screen:
// the Lua module looked at all of an app's windows, and then pinned only
// the main one, so here the rule leaves the app alone this round. It
// asks Accessibility only when selected is not empty and holds no main
// window. A failed AppWindows also counts as a main window
// elsewhere, so a popup is not pinned on an unknown.
func (p *planner) mainElsewhere(rule Rule, appWindows []CGWindow, selected []Window) bool {
	if rule.MainTitle == nil || len(selected) == 0 {
		return false
	}
	for _, w := range selected {
		if isMain(rule, w.Info) {
			return false
		}
	}
	pids := map[int32]bool{}
	for _, w := range appWindows {
		if pids[w.PID] {
			continue
		}
		pids[w.PID] = true
		infos, err := p.appWindows(w.PID)
		if err != nil || HasMain(rule, infos) {
			return true
		}
	}
	return false
}

// appWindows returns the AppWindows answer for pid, asking only when the
// cache has none.
func (p *planner) appWindows(pid int32) ([]Info, error) {
	entry := p.next[pid]
	if entry.windows != nil {
		return entry.windows.infos, entry.windows.err
	}
	p.stats.Inspects++
	infos, err := p.insp.AppWindows(pid)
	entry.windows = &appInspection{infos: infos, err: err}
	return infos, err
}

// margin applies a margin rule to every window on its display. It needs
// no Accessibility to find the windows that break the margin, and asks
// only about those, which have to move anyway.
func (p *planner) margin(rule MarginRule, windows []CGWindow) {
	screen := FindScreenBySize(rule.Screen, p.displays)
	if screen == nil {
		return
	}
	for _, w := range windows {
		frame := p.frames[w.ID]
		onRuleScreen := ScreenOf(frame, p.displays) == screen
		if !onRuleScreen || IsExcepted(rule, w, p.handled) {
			continue
		}
		target, ok := LeftMarginFrame(rule, screen.Visible, frame)
		if !ok || isCGFullScreen(frame, p.displays) {
			continue
		}
		info := p.info(w)
		if !info.Normal() {
			continue
		}
		p.move(w, target, info.Title, true)
	}
}
