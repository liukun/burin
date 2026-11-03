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
	status := "Active"
	if s.Status == Idle {
		status = "Idle"
	}
	m.Item(fmt.Sprintf("Status: %s (%s)", status, FormatDuration(s.Elapsed))).Disabled(true)
	m.Separator()
	m.Item("Active today: " + FormatDuration(s.TodayActive)).Disabled(true)
	m.Item(fmt.Sprintf("Work sessions today: %d", s.Sessions)).Disabled(true)
	m.Separator()
	if m.Item("Take a break").Chosen() {
		t.Rest(now)
	}
	if m.Item("Restart timer").Chosen() {
		t.Restart(now)
	}
	if m.Item("Test reminder (in 10s)").Chosen() {
		t.TestReminder(now)
	}
	if m.Item("Reset today's stats...").Chosen() {
		// The dialog waits for the user; the menu runs on the main thread.
		go t.confirmResetToday()
	}
}

// confirmResetToday clears the today counters once the user confirms.
func (t *Tracker) confirmResetToday() {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:               mygo.MessageWarning,
		Title:              "Reset today's stats",
		Message:            "Clear today's active time and work sessions?",
		Buttons:            []string{"Reset", "Cancel"},
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
