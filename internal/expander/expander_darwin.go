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
