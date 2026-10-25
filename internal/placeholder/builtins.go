package placeholder

import (
	"fmt"
	"time"
)

const (
	dateFormat     = "%Y-%m-%d"
	datetimeFormat = "%Y-%m-%d %H:%M:%S"
)

// Deps are what the built-in functions need from the outside.
type Deps struct {
	Now       func() time.Time
	Clipboard func() string
	// Shell runs a command line with sh -c.
	Shell func(cmd, stdin string) (string, error)
}

// Builtins returns the built-in functions. Adding one is adding an entry.
func Builtins(d Deps) map[string]Func {
	return map[string]Func{
		"date": func(args []string, _ string) (string, error) {
			if len(args) != 0 {
				return "", fmt.Errorf("takes no arguments, got %d", len(args))
			}
			return Strftime(d.Now(), dateFormat)
		},
		"datetime": func(args []string, _ string) (string, error) {
			switch len(args) {
			case 0:
				return Strftime(d.Now(), datetimeFormat)
			case 1:
				return Strftime(d.Now(), args[0])
			}
			return "", fmt.Errorf("takes one format, got %d arguments", len(args))
		},
		"clipboard": func(_ []string, _ string) (string, error) {
			return d.Clipboard(), nil
		},
		"unwrap": func(_ []string, stdin string) (string, error) {
			return Unwrap(stdin), nil
		},
		shellName: func(args []string, stdin string) (string, error) {
			if len(args) != 1 || args[0] == "" {
				return "", fmt.Errorf("needs a command")
			}
			return d.Shell(args[0], stdin)
		},
		"null": func(_ []string, _ string) (string, error) {
			return "", nil
		},
	}
}
