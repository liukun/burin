package placeholder

import (
	"fmt"
	"strings"
)

// Func is a built-in function. args are the parsed arguments and stdin is
// the output of the previous pipeline stage, empty for the first.
type Func func(args []string, stdin string) (string, error)

// Evaluator runs placeholder pipelines. Names not in Builtins go to
// Fallback, which runs external scripts.
type Evaluator struct {
	Builtins map[string]Func
	Fallback func(name string, args []string, stdin string) (string, error)
}

// Error reports a placeholder that failed and was kept as written.
type Error struct {
	Placeholder string
	Err         error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Placeholder, e.Err) }

func (e *Error) Unwrap() error { return e.Err }

// Expand replaces each placeholder in content with its pipeline's output.
// "{{" stands for a literal "{". A placeholder that fails to parse or
// whose pipeline fails at any stage is kept as written and reported.
func (ev *Evaluator) Expand(content string) (string, []error) {
	var (
		b    strings.Builder
		errs []error
	)
	for {
		i := strings.IndexByte(content, '{')
		if i < 0 {
			b.WriteString(content)
			return b.String(), errs
		}
		b.WriteString(content[:i])
		rest := content[i+1:]
		if strings.HasPrefix(rest, "{") {
			b.WriteByte('{')
			content = rest[1:]
			continue
		}
		end := findClose(rest)
		if end < 0 {
			b.WriteByte('{')
			content = rest
			continue
		}
		raw := content[i : i+1+end+1]
		out, err := ev.run(rest[:end])
		if err != nil {
			errs = append(errs, &Error{Placeholder: raw, Err: err})
			out = raw
		}
		b.WriteString(out)
		content = rest[end+1:]
	}
}

func (ev *Evaluator) run(body string) (string, error) {
	calls, err := Parse(body)
	if err != nil {
		return "", err
	}
	out := ""
	for _, c := range calls {
		out, err = ev.call(c, out)
		if err != nil {
			return "", err
		}
	}
	return out, nil
}

func (ev *Evaluator) call(c Call, stdin string) (string, error) {
	if fn, ok := ev.Builtins[c.Name]; ok {
		out, err := fn(c.Args, stdin)
		if err != nil {
			return "", fmt.Errorf("%s: %w", c.Name, err)
		}
		return out, nil
	}
	if ev.Fallback == nil {
		return "", fmt.Errorf("unknown function %q", c.Name)
	}
	return ev.Fallback(c.Name, c.Args, stdin)
}
