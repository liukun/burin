package activity

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StateFileName is the file the state is kept in, in the user data dir.
const StateFileName = "activity-state.json"

// State is the persisted state of a tracker, with the fields of the
// Hammerspoon script's file. Times are Unix seconds; IdleStartTime is
// nil while active.
type State struct {
	State              Status `json:"state"`
	ActiveStartTime    int64  `json:"activeStartTime"`
	IdleStartTime      *int64 `json:"idleStartTime,omitempty"`
	TodayActiveSeconds int64  `json:"todayActiveSeconds"`
	TodayWorkSessions  int    `json:"todayWorkSessions"`
	TodayDate          string `json:"todayDate"`
	NotifiedOvertime   bool   `json:"notifiedOvertime"`
	SavedAt            int64  `json:"savedAt"`
}

// state is t's State saved at now. t.mu is held.
func (t *Tracker) state(now time.Time) State {
	s := State{
		State:              t.status,
		ActiveStartTime:    t.activeStart.Unix(),
		TodayActiveSeconds: int64(t.todayActive / time.Second),
		TodayWorkSessions:  t.sessions,
		TodayDate:          t.todayDate,
		NotifiedOvertime:   t.notified,
		SavedAt:            now.Unix(),
	}
	if !t.idleStart.IsZero() {
		idle := t.idleStart.Unix()
		s.IdleStartTime = &idle
	}
	return s
}

// State returns the state to save at now, as on quit.
func (t *Tracker) State(now time.Time) State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state(now)
}

// Restore takes back a saved state at now. The today counters carry
// over only on the same day; an active period goes on, counting the
// time since the save as active, as the app was only restarted.
func (t *Tracker) Restore(s State, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	today := now.Format(dateLayout)
	if s.TodayDate == today {
		t.todayActive = time.Duration(s.TodayActiveSeconds) * time.Second
		t.sessions = s.TodayWorkSessions
	}
	t.todayDate = today

	wasActive := s.State == Active && s.ActiveStartTime != 0
	wasIdle := s.State == Idle && s.IdleStartTime != nil
	switch {
	case wasActive:
		t.status = Active
		t.activeStart = time.Unix(s.ActiveStartTime, 0)
		t.notified = s.NotifiedOvertime
		savedAt := now
		if s.SavedAt != 0 {
			savedAt = time.Unix(s.SavedAt, 0)
		}
		t.todayActive += now.Sub(savedAt).Truncate(time.Second)
	case wasIdle:
		t.status = Idle
		t.idleStart = time.Unix(*s.IdleStartTime, 0)
	}
	t.lastUpdate = now
}

// Store reads and writes the state file. Its methods are safe from any
// goroutine.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a store of the state file at path.
func NewStore(path string) *Store { return &Store{path: path} }

// Load reads the saved state; ok is false when there is none.
func (s *Store) Load() (st State, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, false, err
	}
	return st, true, nil
}

// Save writes st, through a temporary file so a crash midway leaves the
// last state whole.
func (s *Store) Save(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
