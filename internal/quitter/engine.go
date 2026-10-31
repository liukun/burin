//go:build darwin

package quitter

import (
	"log"
	"os"
	"sync"
	"time"

	"burin/internal/workspace"
)

const (
	// DefaultInterval is how often the engine looks at the running apps,
	// which bounds how late after its period an app is hidden or quit.
	DefaultInterval = 30 * time.Second

	// DefaultActiveInterval is how often the engine samples the frontmost
	// app between rounds, so a switch to an app longer than this resets
	// its timer even when no round sees it frontmost.
	DefaultActiveInterval = 2 * time.Second

	// stopTimeout bounds how long Stop waits for a round to finish: a
	// round asks the main thread for the running apps, which may no
	// longer run once the app quits.
	stopTimeout = time.Second
)

// Engine applies a Config on its own goroutine, every interval, and
// samples the frontmost app every DefaultActiveInterval in between. It
// asks the main thread for the running apps and to hide or quit them, and
// runs no run loop of its own.
type Engine struct {
	mu   sync.Mutex
	cfg  Config
	stop chan struct{}
	done chan struct{}
}

// SetConfig replaces the rules from the next round on. The state is kept:
// it records when apps were frontmost, not rules. Going to zero rules
// clears the state on the next round.
func (e *Engine) SetConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
}

func (e *Engine) config() Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// Start runs a round now and then every interval until Stop, and samples
// the frontmost app every DefaultActiveInterval. Starting a started
// engine does nothing.
func (e *Engine) Start(interval time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stop != nil {
		return
	}
	e.stop = make(chan struct{})
	e.done = make(chan struct{})
	go e.run(interval, e.stop, e.done)
}

// Stop ends the rounds, waiting for a running one up to stopTimeout.
func (e *Engine) Stop() {
	e.mu.Lock()
	stop, done := e.stop, e.done
	e.stop, e.done = nil, nil
	e.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(stopTimeout):
		log.Printf("quitter: round did not finish within %v", stopTimeout)
	}
}

func (e *Engine) run(interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	activeTicker := time.NewTicker(DefaultActiveInterval)
	defer activeTicker.Stop()
	self := int32(os.Getpid())
	state := e.round(nil, self)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			state = e.round(state, self)
		case <-activeTicker.C:
			state = e.sampleFrontmost(state, self)
		}
	}
}

// sampleFrontmost marks the frontmost app active now, so a switch shorter
// than the round interval still resets its timer. It returns the state
// for the next round.
func (e *Engine) sampleFrontmost(state State, self int32) State {
	// Without rules the round keeps no state, so neither does this.
	if e.config().Len() == 0 {
		return state
	}
	pid, ok := workspace.FrontmostPID()
	if !ok || pid == self {
		return state
	}
	return MarkActive(state, pid, time.Now())
}

// round looks at the running apps once and hides or quits those whose
// rule says so. It returns the state for the next round.
func (e *Engine) round(state State, self int32) State {
	cfg := e.config()
	// Without rules there is nothing to time, and nothing to list.
	if cfg.Len() == 0 {
		return nil
	}
	apps, ok := workspace.RunningApps()
	if !ok {
		return state
	}
	steps, next := Plan(cfg, withoutPID(apps, self), state, time.Now())
	for _, s := range steps {
		log.Printf("quitter: %s", s)
		// hide and terminate are asynchronous: NO means the app did not
		// take the request at once, not that it refused. A busy app
		// often hides a moment later; the next round sees it hidden
		// or gone and leaves it alone, or asks again.
		if !apply(s) {
			log.Printf("quitter: %s %s (pid %d) returned NO, checking next round", s.Rule.Action, s.App.Name, s.App.PID)
		}
	}
	return next
}

// apply hides or quits the app of step. It reports false when the app is
// gone or did not take the request at once; the request may still land,
// as hide and terminate are asynchronous.
func apply(step Step) bool {
	if step.Rule.Action == Quit {
		return workspace.Terminate(step.App.PID)
	}
	return workspace.Hide(step.App.PID)
}
