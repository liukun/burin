package placeholder

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		body string
		want []Call
	}{
		{"date", []Call{{Name: "date"}}},
		{" clipboard | unwrap ", []Call{{Name: "clipboard"}, {Name: "unwrap"}}},
		{`datetime("%Y-%m-%d %H:%M")`, []Call{{Name: "datetime", Args: []string{"%Y-%m-%d %H:%M"}}}},
		{`f( a , "b, c" ,d)`, []Call{{Name: "f", Args: []string{"a", "b, c", "d"}}}},
		{`f()`, []Call{{Name: "f"}}},
		{`f("say \"hi\"")`, []Call{{Name: "f", Args: []string{`say "hi"`}}}},
		{"shell(open -a Tern)|null", []Call{{Name: "shell", Args: []string{"open -a Tern"}}, {Name: "null"}}},
		{"shell(echo a, b)", []Call{{Name: "shell", Args: []string{"echo a, b"}}}},
		{"shell(echo $(date) | tr a b)", []Call{{Name: "shell", Args: []string{"echo $(date) | tr a b"}}}},
		{`shell("echo ')' | awk '{print $1}'")`, []Call{{Name: "shell", Args: []string{`echo ')' | awk '{print $1}'`}}}},
		{`shell("/bin/x" --flag)`, []Call{{Name: "shell", Args: []string{`"/bin/x" --flag`}}}},
		{"clipboard|f2_ip", []Call{{Name: "clipboard"}, {Name: "f2_ip"}}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.body)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.body, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", tt.body, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, body := range []string{"", "a||b", "bad name", "../x", "f(a", `f("a)`, "f(a)b", `f("a"b)`, "shell(echo ))"} {
		if calls, err := Parse(body); err == nil {
			t.Errorf("Parse(%q) = %#v, want error", body, calls)
		}
	}
}

func testEvaluator() *Evaluator {
	return &Evaluator{
		Builtins: map[string]Func{
			"hello": func([]string, string) (string, error) { return "hi", nil },
			"upper": func(_ []string, in string) (string, error) { return strings.ToUpper(in), nil },
			"join": func(args []string, in string) (string, error) {
				return in + "[" + strings.Join(args, "/") + "]", nil
			},
			"fail": func([]string, string) (string, error) { return "", errors.New("boom") },
		},
	}
}

func TestExpand(t *testing.T) {
	tests := []struct{ in, want string }{
		{"no placeholders", "no placeholders"},
		{"say {hello}!", "say hi!"},
		{"{hello|upper}", "HI"},
		{"{hello|join(a, b)|upper}", "HI[A/B]"},
		{"{{hello}", "{hello}"},
		{"{{{hello}", "{hi"},
		{"a } b", "a } b"},
		{"open { brace", "open { brace"},
		{"{hello} and {hello}", "hi and hi"},
		{"{join(\"}\")}", "[}]"},
		{"{join(x})}", "[x}]"},
		{"{fail}", "{fail}"},
		{"x{hello|fail}y", "x{hello|fail}y"},
		{"{nope}", "{nope}"},
		{"{bad name} {hello}", "{bad name} hi"},
	}
	ev := testEvaluator()
	for _, tt := range tests {
		got, _ := ev.Expand(tt.in)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandReportsErrors(t *testing.T) {
	_, errs := testEvaluator().Expand("{hello} {fail} {nope}")
	if len(errs) != 2 {
		t.Fatalf("errors %v", errs)
	}
	var pe *Error
	if !errors.As(errs[0], &pe) || pe.Placeholder != "{fail}" {
		t.Fatalf("first error %v", errs[0])
	}
}

func TestExpandFallback(t *testing.T) {
	ev := testEvaluator()
	var gotName, gotStdin string
	var gotArgs []string
	ev.Fallback = func(name string, args []string, stdin string) (string, error) {
		gotName, gotArgs, gotStdin = name, args, stdin
		return "ok", nil
	}
	out, errs := ev.Expand("{hello|f2_ip(1, 2)}")
	if out != "ok" || len(errs) != 0 {
		t.Fatalf("got %q %v", out, errs)
	}
	if gotName != "f2_ip" || !reflect.DeepEqual(gotArgs, []string{"1", "2"}) || gotStdin != "hi" {
		t.Fatalf("fallback got %q %v %q", gotName, gotArgs, gotStdin)
	}
}

func TestStrftime(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	tm := time.Date(2026, 3, 5, 7, 8, 9, 0, loc)
	tests := []struct{ format, want string }{
		{"%Y-%m-%d", "2026-03-05"},
		{"%Y-%m-%d %H:%M:%S", "2026-03-05 07:08:09"},
		{"%y %b %a %j", "26 Mar Thu 064"},
		{"%Z %z", "CST +0800"},
		{"100%% Monday 2006 Jan", "100% Monday 2006 Jan"},
		{"", ""},
	}
	for _, tt := range tests {
		got, err := Strftime(tm, tt.format)
		if err != nil || got != tt.want {
			t.Errorf("Strftime(%q) = %q, %v; want %q", tt.format, got, err, tt.want)
		}
	}
	for _, bad := range []string{"%Q", "50%"} {
		if _, err := Strftime(tm, bad); err == nil {
			t.Errorf("Strftime(%q) succeeded", bad)
		}
	}
}

func TestUnwrap(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"soft wrap", "hello\nworld", "hello world"},
		{"common indent", "first\n    second\n    third", "first second third"},
		{"keeps paragraphs", "a\nb\n\nc\nd\n", "a b\n\nc d\n"},
		{"list items", "intro\n- one\n  wraps\n- two\n1. num\n# head\n> quote", "intro\n- one wraps\n- two\n1. num\n# head\n> quote"},
		{"cjk tight", "中文\n继续", "中文继续"},
		{"cjk next to ascii", "中文\nabc", "中文 abc"},
		{"indent from line two", "  def f():\n      body\n      more", "  def f(): body more"},
		{"strips indent from all lines", "    a\n    b\n  c", "  a b c"},
		{"tab lines skipped", "a\n\tb\n  c", "a \tb c"},
		{"trailing spaces trimmed on join", "a   \nb", "a b"},
		{"crlf", "a\r\nb\r\n", "a b\n"},
	}
	for _, tt := range tests {
		if got := Unwrap(tt.in); got != tt.want {
			t.Errorf("%s: Unwrap(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestBuiltins(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC)
	var shellCmd, shellIn string
	ev := &Evaluator{Builtins: Builtins(Deps{
		Now:       func() time.Time { return now },
		Clipboard: func() string { return "line one\nline two" },
		Shell: func(cmd, stdin string) (string, error) {
			shellCmd, shellIn = cmd, stdin
			return "shell-out", nil
		},
	})}
	tests := []struct{ in, want string }{
		{"git checkout -b liukun--{date}--", "git checkout -b liukun--2026-10-25--"},
		{`{datetime("%H:%M")}`, "09:30"},
		{"{datetime(%Y/%m/%d)}", "2026/10/25"},
		{"{datetime}", "2026-10-25 09:30:00"},
		{"{clipboard}", "line one\nline two"},
		{"{clipboard|unwrap}", "line one line two"},
		{"{shell(open -a Tern)|null}", ""},
		{"{date(x)}", "{date(x)}"},
		{"{datetime(%Q)}", "{datetime(%Q)}"},
	}
	for _, tt := range tests {
		if got, _ := ev.Expand(tt.in); got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got, _ := ev.Expand("{clipboard|shell(wc -l)}"); got != "shell-out" {
		t.Fatalf("shell pipeline %q", got)
	}
	if shellCmd != "wc -l" || shellIn != "line one\nline two" {
		t.Fatalf("shell got %q stdin %q", shellCmd, shellIn)
	}
}

func writeScript(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), mode); err != nil {
		t.Fatal(err)
	}
}

type stderrLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *stderrLog) add(name, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, name+": "+text)
}

func (l *stderrLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func TestRunnerScript(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "echoargs", `printf '%s|' "$@"; cat; printf '\n\n'`, 0o755)
	writeScript(t, dir, "ext.py", `echo ext`, 0o755)
	writeScript(t, dir, "multi.b", `echo b`, 0o755)
	writeScript(t, dir, "multi.a", `echo a`, 0o755)
	writeScript(t, dir, "noexec", `echo no`, 0o644)
	writeScript(t, dir, "warn", `echo warned >&2; echo out`, 0o755)
	writeScript(t, dir, "fails", `echo bad thing >&2; exit 3`, 0o755)
	writeScript(t, dir, "slow", `sleep 5; echo late`, 0o755)
	writeScript(t, dir, "env", `printf %s "$PATH"`, 0o755)

	logs := &stderrLog{}
	// macOS may scan a new executable on first run, which takes a while.
	r := &Runner{Dir: dir, Timeout: 10 * time.Second, OnStderr: logs.add,
		Env: EnvWithPath(os.Environ(), "/usr/bin:/bin:/custom")}

	out, err := r.Script("echoargs", []string{"a b", "c"}, "in")
	if err != nil || out != "a b|c|in" {
		t.Fatalf("echoargs: %q %v", out, err)
	}
	if out, err := r.Script("ext", nil, ""); err != nil || out != "ext" {
		t.Fatalf("ext: %q %v", out, err)
	}
	if out, err := r.Script("multi", nil, ""); err != nil || out != "a" {
		t.Fatalf("multi: %q %v", out, err)
	}
	if _, err := r.Script("noexec", nil, ""); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("noexec: %v", err)
	}
	if _, err := r.Script("missing", nil, ""); err == nil {
		t.Fatal("missing script succeeded")
	}
	if out, err := r.Script("warn", nil, ""); err != nil || out != "out" {
		t.Fatalf("warn: %q %v", out, err)
	}
	_, err = r.Script("fails", nil, "")
	if err == nil || !strings.Contains(err.Error(), "bad thing") {
		t.Fatalf("fails: %v", err)
	}
	if got := logs.get(); !reflect.DeepEqual(got, []string{"warn: warned", "fails: bad thing"}) {
		t.Fatalf("stderr %v", got)
	}

	start := time.Now()
	short := &Runner{Dir: dir, Timeout: 300 * time.Millisecond}
	_, err = short.Script("slow", nil, "")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("slow: %v", err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("timeout took %v", d)
	}

	if out, err := r.Script("env", nil, ""); err != nil || out != "/usr/bin:/bin:/custom" {
		t.Fatalf("env: %q %v", out, err)
	}
}

func TestRunnerShell(t *testing.T) {
	r := &Runner{Timeout: 10 * time.Second}
	out, err := r.Shell("tr a-z A-Z | sed 's/,/;/'", "a, b\n")
	if err != nil || out != "A; B" {
		t.Fatalf("shell: %q %v", out, err)
	}
	if _, err := r.Shell("exit 1", ""); err == nil {
		t.Fatal("exit 1 succeeded")
	}
}

func TestEvaluatorWithRunner(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "rev", `rev`, 0o755)
	r := &Runner{Dir: dir, Timeout: 10 * time.Second}
	ev := &Evaluator{
		Builtins: Builtins(Deps{Now: time.Now, Clipboard: func() string { return "abc" }, Shell: r.Shell}),
		Fallback: r.Script,
	}
	if out, errs := ev.Expand("{clipboard|rev}"); out != "cba" || len(errs) != 0 {
		t.Fatalf("got %q %v", out, errs)
	}
	if out, _ := ev.Expand("{shell(exit 2)}"); out != "{shell(exit 2)}" {
		t.Fatalf("failed shell gave %q", out)
	}
}

func TestEnvWithPath(t *testing.T) {
	got := EnvWithPath([]string{"A=1", "PATH=/old", "B=2"}, "/new")
	if !reflect.DeepEqual(got, []string{"A=1", "B=2", "PATH=/new"}) {
		t.Fatalf("got %v", got)
	}
}
