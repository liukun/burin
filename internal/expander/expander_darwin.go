//go:build darwin

package expander

import (
	"log"
	"time"

	"burin/internal/eventtap"
	"burin/internal/keys"
	"burin/internal/pasteboard"
)

// Delete removes the last n typed characters with backspaces.
func Delete(n int) {
	keys.Backspaces(n)
}

// Paste inserts text through a concealed pasteboard entry. It first waits
// briefly so preceding backspaces settle before the paste.
func Paste(keyword, text string) {
	time.Sleep(20 * time.Millisecond)
	if !pasteboard.WriteConcealed(text) {
		// The keyword is already deleted and cannot be restored without
		// the pasteboard, so the failure is only logged.
		log.Printf("expander: pasteboard write failed for %q", keyword)
		return
	}
	time.Sleep(20 * time.Millisecond)
	keys.CmdV()
}

// Listen feeds keyDown events from a new tap into e until the tap is
// stopped. Keys that HandleKey consumes never reach the focused app.
func Listen(e *Expander) (*eventtap.Tap, error) {
	tap := &eventtap.Tap{}
	err := tap.Start(eventtap.MaskBit(eventtap.EventKeyDown), func(eventType uint32, event uintptr) bool {
		if eventType != eventtap.EventKeyDown {
			return false
		}
		return e.HandleKey(Key{
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
