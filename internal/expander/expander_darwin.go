//go:build darwin

package expander

import (
	"log"
	"time"
	"unicode/utf8"

	"burin/internal/eventtap"
	"burin/internal/keys"
	"burin/internal/pasteboard"
)

// Paste deletes the keyword with backspaces and pastes text through a
// concealed pasteboard entry.
func Paste(keyword, text string) {
	keys.Backspaces(utf8.RuneCountInString(keyword))
	time.Sleep(20 * time.Millisecond)
	if !pasteboard.WriteConcealed(text) {
		log.Printf("expander: pasteboard write failed for %q", keyword)
		return
	}
	time.Sleep(20 * time.Millisecond)
	keys.CmdV()
}

// Listen feeds keyDown events from a new tap into e until the tap is
// stopped.
func Listen(e *Expander) (*eventtap.Tap, error) {
	tap := &eventtap.Tap{}
	err := tap.Start(eventtap.MaskBit(eventtap.EventKeyDown), func(eventType uint32, event uintptr) {
		if eventType != eventtap.EventKeyDown {
			return
		}
		e.HandleKey(Key{
			Keycode: eventtap.Keycode(event),
			Flags:   eventtap.Flags(event),
			Chars:   eventtap.UnicodeString(event),
		})
	})
	if err != nil {
		return nil, err
	}
	return tap, nil
}
