package main

import (
	"testing"
	"time"

	"burin/internal/eventtap"
	"burin/internal/expander"
	"burin/internal/gesture"
	"burin/internal/keys"
)

// Key events of the expansion tests.
const (
	typedKey uintptr = iota + 200
	expansionKey
	leaderKey
)

func newExpanderBurin(t *testing.T) (*burin, *fakeIO) {
	f := newFakeIO(t)
	f.key[typedKey] = expander.Key{Keycode: 0, Chars: "a"}
	f.key[expansionKey] = expander.Key{Keycode: 51}
	f.key[leaderKey] = expander.Key{Keycode: 0, Chars: "b"}
	f.user[expansionKey] = keys.ExpansionMark
	f.user[leaderKey] = keys.SyntheticMark
	b := &burin{io: f.eventIO()}
	b.ex = expander.New(nil, time.Hour, func(int) {}, func(string, string) {})
	t.Cleanup(b.ex.Stop)
	return b, f
}

func TestHandleExpanderIgnoresExpansionKeys(t *testing.T) {
	b, _ := newExpanderBurin(t)
	if b.handleExpander(eventtap.EventKeyDown, typedKey) {
		t.Fatal("typed key consumed")
	}
	// A fed Backspace would delete the "a".
	if b.handleExpander(eventtap.EventKeyDown, expansionKey) {
		t.Fatal("expansion key consumed")
	}
	if got := b.ex.Buffer(); got != "a" {
		t.Fatalf("buffer = %q, want %q", got, "a")
	}
}

func TestHandleExpanderFeedsLeaderKeys(t *testing.T) {
	b, _ := newExpanderBurin(t)
	b.handleExpander(eventtap.EventKeyDown, typedKey)
	b.handleExpander(eventtap.EventKeyDown, leaderKey)
	if got := b.ex.Buffer(); got != "ab" {
		t.Fatalf("buffer = %q, want %q", got, "ab")
	}
}

func TestHandleLeaderSkipsMarkedKeys(t *testing.T) {
	b, _ := newExpanderBurin(t)
	// A key that reached the leader would panic in eventtap.Autorepeat,
	// whose purego function is not loaded in tests.
	for _, ev := range []uintptr{expansionKey, leaderKey} {
		if b.handleLeader(eventtap.EventKeyDown, ev) {
			t.Errorf("event %d consumed", ev)
		}
	}
}

func TestHandleGestureSkipsExpansionMark(t *testing.T) {
	b, f, ran := newGestureBurin(t)
	f.user[rightDown] = keys.ExpansionMark
	before := b.recognizer
	if b.handleGesture(gesture.EventRightMouseDown, rightDown) {
		t.Fatal("expansion event consumed")
	}
	if b.recognizer != before {
		t.Errorf("recognizer = %+v, want %+v", b.recognizer, before)
	}
	if len(*ran) != 0 {
		t.Errorf("ran %v", *ran)
	}
	f.checkCalls()
}
