package badge

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// speakEvery is how long an app waits after its count was spoken
	// before it is spoken again, though the count is the same: a break
	// may run long, and a missed message should not be missed for good.
	speakEvery = time.Minute
	// sayTimeout stops a say that hangs, so it does not hold back the
	// alerts after it.
	sayTimeout = 30 * time.Second
	// sayPath is the speech command of macOS.
	sayPath = "/usr/bin/say"
)

// Speaker speaks the counts of the badges of Speak apps while the user
// is on a break, within a window of the day, each app at most every
// speakEvery. Its methods are safe from any goroutine.
type Speaker struct {
	// say runs the speech; tests stand in for it.
	say func(text string) error

	// mu guards the rest.
	mu sync.Mutex
	// from and until are the window in minutes since midnight.
	from, until int
	// last is when the count of each app was last spoken, by name.
	last map[string]time.Time
	// running is set while a say runs, so they do not talk over each
	// other.
	running bool
	// sayErrors logs say failures, a lasting one once.
	sayErrors errorLog
	// done counts the says running, so tests can wait for them.
	done sync.WaitGroup
}

// NewSpeaker returns a speaker with the default window.
func NewSpeaker() *Speaker {
	return &Speaker{
		say:       say,
		from:      DefaultSpeakFrom,
		until:     DefaultSpeakUntil,
		last:      map[string]time.Time{},
		sayErrors: errorLog{prefix: "badges: say: "},
	}
}

// SetWindow sets the time of day the counts may be spoken, in minutes
// since midnight, as badges.md is reloaded.
func (s *Speaker) SetWindow(from, until int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.from, s.until = from, until
}

// Tick speaks at now the counts of the Speak apps of badges that are
// due, in one say, when now is in the window; it is called while the
// user is on a break. The say runs on its own goroutine; while one runs
// Tick speaks nothing, and the apps it would have spoken stay due.
func (s *Speaker) Tick(now time.Time, badges []Badge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running || !inWindow(now, s.from, s.until) {
		return
	}
	var messages []string
	for _, b := range badges {
		if !s.due(now, b) {
			continue
		}
		s.last[b.Name] = now
		messages = append(messages, message(b.Name, b.Label))
	}
	if len(messages) == 0 {
		return
	}
	s.running = true
	s.done.Add(1)
	go s.run(strings.Join(messages, ". "))
}

// due reports whether the badge b is to be spoken at now: its app
// speaks, it is a count, and it was not spoken in the last speakEvery.
// s.mu is held.
func (s *Speaker) due(now time.Time, b Badge) bool {
	if !b.Speak || !isCount(b.Label) {
		return false
	}
	last, spoken := s.last[b.Name]
	return !spoken || now.Sub(last) >= speakEvery
}

// run says text and clears running, logging a failure unless it is the
// last one logged.
func (s *Speaker) run(text string) {
	defer s.done.Done()
	err := s.say(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.sayErrors.log(err)
}

// say speaks text with the say command, stopping it after sayTimeout.
func say(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sayTimeout)
	defer cancel()
	return exec.CommandContext(ctx, sayPath, text).Run()
}

// message is what is spoken for the badge label of the app named name:
// "Slack has 3 messages", "Slack has 1 message", and for a label such
// as "99+", read by its digits and a trailing "+",
// "Slack has 99 plus messages".
func message(name, label string) string {
	var digits strings.Builder
	for _, r := range label {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	count := digits.String()
	plus := strings.HasSuffix(strings.TrimSpace(label), "+")
	if plus {
		count += " plus"
	}
	noun := "messages"
	if count == "1" {
		noun = "message"
	}
	return fmt.Sprintf("%s has %s %s", name, count, noun)
}

// inWindow reports whether the local time of day of now is in the
// window from from up to until, in minutes since midnight. A window
// whose until is earlier than from crosses midnight; one whose until is
// from is empty.
func inWindow(now time.Time, from, until int) bool {
	minute := minuteOfDay(now)
	if from <= until {
		return from <= minute && minute < until
	}
	return minute >= from || minute < until
}

// minuteOfDay is the time of day of t in minutes since midnight.
func minuteOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}
