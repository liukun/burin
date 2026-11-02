package activity

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Layout of the bar, in DIPs.
const (
	barWidth  = 220
	barHeight = 24
	fontSize  = 12
	// rebuildDelay lets a burst of display changes, as one is plugged in,
	// rebuild the bars once.
	rebuildDelay = 500 * time.Millisecond
)

// Colors of the bar's text: white with a dark shadow, legible on any
// wallpaper or window under it.
var (
	textColor   = ui.RGBA(255, 255, 255, 0.95)
	shadowColor = ui.RGBA(0, 0, 0, 0.9)
)

// Bar shows the tracker's title at the top center of every display, in a
// floating panel on all Spaces, with a context menu of its stats and
// actions. Its methods are safe from any goroutine.
type Bar struct {
	tracker *Tracker

	// titleMu guards the title the bars show, the last Invalidate's.
	titleMu sync.Mutex
	title   string

	// winMu guards the windows, one per display. It is never held while
	// waiting on the main thread, which takes it in Invalidate from the
	// context menu.
	winMu sync.Mutex
	wins  []*mygo.Window
	timer *time.Timer
	// rebuildMu keeps two rebuilds from interleaving; only off the main
	// thread.
	rebuildMu sync.Mutex
}

// NewBar shows a bar of t on every display, redrawn as t changes, and
// rebuilds them as the displays change. It takes t's OnChange and must be
// called once the app is ready.
func NewBar(t *Tracker) *Bar {
	b := &Bar{tracker: t}
	b.title = Title(t.Snapshot(time.Now()))
	b.rebuild()
	t.OnChange = b.Invalidate
	mygo.Screen.OnDisplaysChanged(b.displaysChanged)
	return b
}

// displaysChanged rebuilds the bars once the displays settle.
func (b *Bar) displaysChanged() {
	b.winMu.Lock()
	defer b.winMu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(rebuildDelay, func() {
		b.rebuildMu.Lock()
		defer b.rebuildMu.Unlock()
		b.rebuild()
	})
}

// rebuild replaces the bars with one per connected display.
func (b *Bar) rebuild() {
	var wins []*mygo.Window
	for _, d := range mygo.Screen.Displays() {
		wins = append(wins, b.newWindow(d))
	}
	b.winMu.Lock()
	old := b.wins
	b.wins = wins
	b.winMu.Unlock()
	for _, w := range old {
		w.Close()
	}
}

// newWindow shows a bar on d.
func (b *Bar) newWindow(d mygo.Display) *mygo.Window {
	p := barOrigin(d.WorkArea)
	// Created hidden and moved before showing: a window at 0, 0 would be
	// centered instead.
	w := mygo.NewWindow(mygo.WindowOptions{
		Width:          barWidth,
		Height:         barHeight,
		UseContentSize: true,
		Hidden:         true,
		Frameless:      true,
		Transparent:    true,
		AlwaysOnTop:    true,
		Panel:          true,
		DisableResize:  true,
		DisableMove:    true,
		DisableShadow:  true,
		Content:        ui.View(b.view),
	})
	w.SetPosition(p.X, p.Y)
	w.ShowInactive()
	return w
}

// barOrigin is where a bar goes to sit centered at the top of area, the
// display's work area, under the menu bar.
func barOrigin(area mygo.Rectangle) mygo.Point {
	return mygo.Point{
		X: area.X + (area.Width-barWidth)/2,
		Y: area.Y,
	}
}

// Invalidate redraws the bars when the tracker's title changed since the
// last call.
func (b *Bar) Invalidate() {
	title := Title(b.tracker.Snapshot(time.Now()))
	b.titleMu.Lock()
	changed := title != b.title
	b.title = title
	b.titleMu.Unlock()
	if !changed {
		return
	}
	b.winMu.Lock()
	wins := b.wins
	b.winMu.Unlock()
	for _, w := range wins {
		w.Invalidate()
	}
}

func (b *Bar) view(c *ui.Context) {
	b.titleMu.Lock()
	title := b.title
	b.titleMu.Unlock()
	c.Root().Background(ui.Transparent)
	row := ui.Row(c).Fill().Center().Background(ui.Transparent)
	row.Children(func() {
		ui.Text(c, title).FontSize(fontSize).TextColor(textColor).Shadow(0, 1, 3, 0, shadowColor).NoWrap()
	})
	row.ContextMenu(b.menu)
}

// menu is the bar's context menu: the tracker's stats and actions.
func (b *Bar) menu(m *ui.Menu) {
	now := time.Now()
	s := b.tracker.Snapshot(now)
	status := "活跃"
	if s.Status == Idle {
		status = "空闲"
	}
	m.Item(fmt.Sprintf("状态: %s (%s)", status, FormatDuration(s.Elapsed))).Disabled(true)
	m.Separator()
	m.Item("今日活跃: " + FormatDuration(s.TodayActive)).Disabled(true)
	m.Item(fmt.Sprintf("今日工作周期: %d 次", s.Sessions)).Disabled(true)
	m.Separator()
	if m.Item("休息").Chosen() {
		b.tracker.Rest(now)
	}
	if m.Item("重新计时").Chosen() {
		b.tracker.Restart(now)
	}
	if m.Item("测试提醒 (10秒后)").Chosen() {
		b.tracker.TestReminder(now)
	}
	if m.Item("重置今日统计...").Chosen() {
		// The dialog waits for the user; the menu runs on the main thread.
		go b.confirmResetToday()
	}
}

// confirmResetToday clears the today counters once the user confirms.
func (b *Bar) confirmResetToday() {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:               mygo.MessageWarning,
		Title:              "重置今日统计",
		Message:            "确定要清零今日活跃时间和工作周期吗？",
		Buttons:            []string{"确定", "取消"},
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil {
		log.Printf("activity: reset dialog: %v", err)
		return
	}
	if res.Button != 0 {
		return
	}
	b.tracker.ResetToday(time.Now())
}
