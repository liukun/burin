package badge

import (
	"bytes"
	"errors"
	"log"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInWindow(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 3, hour, minute, 0, 0, time.Local)
	}
	tests := []struct {
		name        string
		now         time.Time
		from, until int
		want        bool
	}{
		{"inside", at(12, 0), 10 * 60, 22 * 60, true},
		{"at from", at(10, 0), 10 * 60, 22 * 60, true},
		{"at until", at(22, 0), 10 * 60, 22 * 60, false},
		{"before", at(9, 59), 10 * 60, 22 * 60, false},
		{"after", at(23, 0), 10 * 60, 22 * 60, false},
		{"past midnight, late", at(23, 0), 22 * 60, 2 * 60, true},
		{"past midnight, early", at(1, 59), 22 * 60, 2 * 60, true},
		{"past midnight, outside", at(12, 0), 22 * 60, 2 * 60, false},
		{"past midnight, at until", at(2, 0), 22 * 60, 2 * 60, false},
		{"empty", at(10, 0), 10 * 60, 10 * 60, false},
		{"empty, elsewhere", at(15, 0), 10 * 60, 10 * 60, false},
	}
	for _, tt := range tests {
		if got := inWindow(tt.now, tt.from, tt.until); got != tt.want {
			t.Errorf("%s: inWindow(%s, %d, %d) = %v, want %v", tt.name, tt.now.Format("15:04"), tt.from, tt.until, got, tt.want)
		}
	}
}

func TestMessage(t *testing.T) {
	tests := []struct{ label, want string }{
		{"1", "Slack has 1 message"},
		{"3", "Slack has 3 messages"},
		{"99+", "Slack has 99 plus messages"},
		{"1+", "Slack has 1 plus messages"},
		{"12", "Slack has 12 messages"},
	}
	for _, tt := range tests {
		if got := message("Slack", tt.label); got != tt.want {
			t.Errorf("message(Slack, %q) = %q, want %q", tt.label, got, tt.want)
		}
	}
}

// fakeSay records what is said; with release set each say waits for
// it to close.
type fakeSay struct {
	mu      sync.Mutex
	said    []string
	release chan struct{}
	err     error
}

func (f *fakeSay) say(text string) error {
	f.mu.Lock()
	f.said = append(f.said, text)
	release, err := f.release, f.err
	f.mu.Unlock()
	if release != nil {
		<-release
	}
	return err
}

func (f *fakeSay) spoken() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.said...)
}

func newTestSpeaker(f *fakeSay) *Speaker {
	s := NewSpeaker()
	s.say = f.say
	return s
}

// tick ticks s at now and waits for the say it starts, if any, which
// ends at now too.
func tick(s *Speaker, now time.Time, badges []Badge) {
	s.now = func() time.Time { return now }
	s.Tick(now, badges)
	s.done.Wait()
}

func TestSpeakerTick(t *testing.T) {
	noon := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	slack := []Badge{{Name: "Slack", Label: "3", Speak: true}}

	t.Run("says the count", func(t *testing.T) {
		f := &fakeSay{}
		s := newTestSpeaker(f)
		tick(s, noon, slack)
		if got, want := f.spoken(), []string{"Slack has 3 messages"}; !reflect.DeepEqual(got, want) {
			t.Errorf("said %q, want %q", got, want)
		}
	})

	t.Run("says every due app in one say", func(t *testing.T) {
		f := &fakeSay{}
		s := newTestSpeaker(f)
		badges := []Badge{{Name: "Slack", Label: "3", Speak: true}, {Name: "Telegram", Label: "•", Speak: true}, {Name: "WhatsApp", Label: "1", Speak: true}}
		tick(s, noon, badges)
		if got, want := f.spoken(), []string{"Slack has 3 messages. WhatsApp has 1 message"}; !reflect.DeepEqual(got, want) {
			t.Errorf("said %q, want %q", got, want)
		}
	})

	t.Run("only inside the window", func(t *testing.T) {
		f := &fakeSay{}
		s := newTestSpeaker(f)
		evening := time.Date(2026, 11, 3, 22, 30, 0, 0, time.Local)
		tick(s, evening, slack)
		if got := f.spoken(); len(got) != 0 {
			t.Errorf("said %q outside the window, want nothing", got)
		}
		s.SetWindow(22*60, 2*60)
		tick(s, evening, slack)
		if got := f.spoken(); len(got) != 1 {
			t.Errorf("said %q in a window past midnight, want once", got)
		}
	})

	t.Run("only speak apps with a count", func(t *testing.T) {
		f := &fakeSay{}
		s := newTestSpeaker(f)
		badges := []Badge{{Name: "Telegram", Label: "2"}, {Name: "Slack", Label: "•", Speak: true}}
		tick(s, noon, badges)
		if got := f.spoken(); len(got) != 0 {
			t.Errorf("said %q, want nothing for a dot or an app that does not speak", got)
		}
	})

	t.Run("repeated once the gap after a say has passed", func(t *testing.T) {
		f := &fakeSay{}
		s := newTestSpeaker(f)
		tick(s, noon, slack)
		tick(s, noon.Add(speakGap-time.Millisecond), slack)
		if got := f.spoken(); len(got) != 1 {
			t.Fatalf("said %q within %v of the say ending, want once", got, speakGap)
		}
		tick(s, noon.Add(speakGap), slack)
		if got := f.spoken(); len(got) != 2 {
			t.Errorf("said %q %v after the say ended, want twice", got, speakGap)
		}
	})

	t.Run("nothing started while a say runs", func(t *testing.T) {
		f := &fakeSay{release: make(chan struct{})}
		s := newTestSpeaker(f)
		chat := []Badge{{Name: "Chat", Label: "1", Speak: true}}
		s.Tick(noon, slack)
		// Wait for the first say to start, so the next ticks find it
		// running.
		for len(f.spoken()) == 0 {
			time.Sleep(time.Millisecond)
		}
		s.Tick(noon.Add(speakGap), chat)
		s.Tick(noon.Add(time.Hour), chat)
		// The say ends an hour after it started.
		ended := noon.Add(time.Hour)
		s.now = func() time.Time { return ended }
		close(f.release)
		s.done.Wait()
		if got, want := f.spoken(), []string{"Slack has 3 messages"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("said %q while a say ran, want %q", got, want)
		}
		// The gap counts from when the say ended.
		tick(s, ended.Add(speakGap-time.Millisecond), chat)
		tick(s, ended.Add(speakGap), chat)
		if got, want := f.spoken(), []string{"Slack has 3 messages", "Chat has 1 message"}; !reflect.DeepEqual(got, want) {
			t.Errorf("said %q, want %q", got, want)
		}
	})

	t.Run("a failed say is spoken again", func(t *testing.T) {
		f := &fakeSay{err: errors.New("no voice")}
		s := newTestSpeaker(f)
		tick(s, noon, slack)
		tick(s, noon.Add(speakGap), slack)
		if got := f.spoken(); len(got) != 2 {
			t.Errorf("said %q, want twice", got)
		}
		if s.sayErrors.last != "no voice" {
			t.Errorf("lastErr = %q, want the say's error", s.sayErrors.last)
		}
	})

	t.Run("a failure is logged once and cleared by a success", func(t *testing.T) {
		var logged bytes.Buffer
		log.SetOutput(&logged)
		log.SetFlags(0)
		t.Cleanup(func() {
			log.SetOutput(os.Stderr)
			log.SetFlags(log.LstdFlags)
		})
		f := &fakeSay{err: errors.New("no voice")}
		s := newTestSpeaker(f)
		tick(s, noon, slack)
		tick(s, noon.Add(speakGap), slack)
		if got := f.spoken(); len(got) != 2 {
			t.Fatalf("said %q, want twice", got)
		}
		if got := strings.Count(logged.String(), "no voice"); got != 1 {
			t.Fatalf("logged %q, want the failure once", logged.String())
		}
		f.mu.Lock()
		f.err = nil
		f.mu.Unlock()
		tick(s, noon.Add(2*speakGap), slack)
		if s.sayErrors.last != "" {
			t.Fatalf("lastErr = %q after a success, want it cleared", s.sayErrors.last)
		}
		f.mu.Lock()
		f.err = errors.New("no voice")
		f.mu.Unlock()
		tick(s, noon.Add(3*speakGap), slack)
		if got := strings.Count(logged.String(), "no voice"); got != 2 {
			t.Errorf("logged %q, want the failure again after a success", logged.String())
		}
	})
}
