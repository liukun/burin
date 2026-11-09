//go:build darwin

package expander

import (
	"log"
	"time"

	"burin/internal/keys"
	"burin/internal/pasteboard"
)

// Delete removes the last n typed characters with backspaces.
func Delete(n int) {
	keys.Backspaces(n)
}

// Paste inserts text through a concealed pasteboard entry. It first waits
// briefly so preceding backspaces settle before the paste. It returns the
// pasteboard change count its write made, ok false if the write failed.
func Paste(keyword, text string) (count int, ok bool) {
	time.Sleep(20 * time.Millisecond)
	count, ok = pasteboard.WriteConcealed(text)
	if !ok {
		// The keyword is already deleted and cannot be restored without
		// the pasteboard, so the failure is only logged.
		log.Printf("expander: pasteboard write failed for %q", keyword)
		return 0, false
	}
	time.Sleep(20 * time.Millisecond)
	keys.CmdV()
	return count, true
}
