package placeholder

import (
	"fmt"
	"strings"
	"time"
)

// strftimeLayouts maps strftime directives to Go layouts.
var strftimeLayouts = map[byte]string{
	'Y': "2006",
	'm': "01",
	'd': "02",
	'H': "15",
	'M': "04",
	'S': "05",
	'y': "06",
	'b': "Jan",
	'a': "Mon",
	'j': "002",
	'Z': "MST",
	'z': "-0700",
}

// Strftime formats t with a strftime-style format. Text outside
// directives is copied as is, so it is never read as a Go layout. "%%" is
// a literal "%".
func Strftime(t time.Time, format string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(format) {
			return "", fmt.Errorf("trailing %% in %q", format)
		}
		i++
		d := format[i]
		if d == '%' {
			b.WriteByte('%')
			continue
		}
		layout, ok := strftimeLayouts[d]
		if !ok {
			return "", fmt.Errorf("unsupported directive %%%c in %q", d, format)
		}
		b.WriteString(t.Format(layout))
	}
	return b.String(), nil
}
