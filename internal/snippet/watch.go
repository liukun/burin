package snippet

import (
	"io/fs"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultWatchDelay is how long the watcher waits for changes to settle.
const DefaultWatchDelay = 500 * time.Millisecond

// Watcher calls a function once changes under a directory settle.
type Watcher struct {
	fs       *fsnotify.Watcher
	delay    time.Duration
	onChange func()

	mu     sync.Mutex
	timer  *time.Timer
	closed bool
	done   chan struct{}
}

// Watch watches dir and its subdirectories, including ones created later,
// and calls onChange on its own goroutine delay after the last change.
func Watch(dir string, delay time.Duration, onChange func()) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{fs: fw, delay: delay, onChange: onChange, done: make(chan struct{})}
	if err := w.addTree(resolveRoot(dir)); err != nil {
		fw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// addTree watches root and every directory below it.
func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		return w.fs.Add(path)
	})
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			log.Printf("snippet: watch: %v", err)
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	if ev.Op == fsnotify.Chmod {
		return
	}
	if ev.Op.Has(fsnotify.Create) {
		// A new directory may already hold files; adding a regular file
		// fails quietly enough to ignore.
		if err := w.addTree(ev.Name); err != nil {
			log.Printf("snippet: watch %s: %v", ev.Name, err)
		}
	}
	w.schedule()
}

func (w *Watcher) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.delay, w.fire)
}

func (w *Watcher) fire() {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	w.onChange()
}

// Close stops watching and cancels a pending call.
func (w *Watcher) Close() error {
	w.mu.Lock()
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	err := w.fs.Close()
	<-w.done
	return err
}
