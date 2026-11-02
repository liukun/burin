package activity

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"burin/internal/bar"
)

// Items makes the tracker a section of the bar: its title, with a
// context menu of its stats and actions. OnChange is when to invalidate
// the bar. A disabled tracker has none.
func (t *Tracker) Items() []bar.Item {
	if !t.Enabled() {
		return nil
	}
	return []bar.Item{{
		Text: Title(t.Snapshot(time.Now())),
		Menu: t.menu,
	}}
}

// menu is the title's context menu: the tracker's stats and actions. It
// runs on the main thread.
func (t *Tracker) menu(m *ui.Menu) {
	now := time.Now()
	s := t.Snapshot(now)
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
		t.Rest(now)
	}
	if m.Item("重新计时").Chosen() {
		t.Restart(now)
	}
	if m.Item("测试提醒 (10秒后)").Chosen() {
		t.TestReminder(now)
	}
	if m.Item("重置今日统计...").Chosen() {
		// The dialog waits for the user; the menu runs on the main thread.
		go t.confirmResetToday()
	}
}

// confirmResetToday clears the today counters once the user confirms.
func (t *Tracker) confirmResetToday() {
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
	t.ResetToday(time.Now())
}
