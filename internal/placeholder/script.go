package placeholder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultTimeout bounds each external command.
const DefaultTimeout = 3 * time.Second

// Runner runs external scripts and shell commands.
type Runner struct {
	// Dir holds the scripts, looked up by name.
	Dir string
	// Env, if set, returns the environment of each command; called per
	// run so the environment can change later (e.g. once the login PATH
	// is known). nil, or a nil result, inherits Burin's.
	Env     func() []string
	Timeout time.Duration
	// OnStderr, if set, receives non-empty stderr of a command that
	// succeeded. A failed command reports its stderr in the returned
	// error instead, so callers see one message per failure.
	OnStderr func(name, stderr string)
}

// find returns the script for name: Dir/name, or else the first in
// sorted order of Dir/name.*. It must be executable.
func (r *Runner) find(name string) (string, error) {
	path := filepath.Join(r.Dir, name)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, checkExecutable(path, info)
	}
	matches, err := filepath.Glob(filepath.Join(r.Dir, name+".*"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("unknown function %q and no script in %s", name, r.Dir)
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		log.Printf("placeholder: several scripts for %q, using %s: %v", name, matches[0], matches)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		return "", err
	}
	return matches[0], checkExecutable(matches[0], info)
}

func checkExecutable(path string, info os.FileInfo) error {
	isExecutable := info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
	if !isExecutable {
		return fmt.Errorf("script %s is not executable", path)
	}
	return nil
}

// Script runs the script for name with args as argv and stdin as input.
// It is an Evaluator's Fallback.
func (r *Runner) Script(name string, args []string, stdin string) (string, error) {
	path, err := r.find(name)
	if err != nil {
		return "", err
	}
	return r.run(name, path, args, stdin)
}

// Shell runs cmd with sh -c.
func (r *Runner) Shell(cmd, stdin string) (string, error) {
	return r.run(shellName, "/bin/sh", []string{"-c", cmd}, stdin)
}

func (r *Runner) run(name, path string, args []string, stdin string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	if r.Env != nil {
		cmd.Env = r.Env()
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Kill the whole process group, so children holding the pipes open
	// do not outlive the timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond

	err := cmd.Run()
	errText := strings.TrimSpace(stderr.String())
	if err == nil {
		if errText != "" && r.OnStderr != nil {
			r.OnStderr(name, errText)
		}
		return strings.TrimRight(stdout.String(), "\r\n"), nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s: timed out after %v%s", name, timeout, stderrSuffix(errText))
	}
	return "", fmt.Errorf("%s: %w%s", name, err, stderrSuffix(errText))
}

func stderrSuffix(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// loginPathTimeout bounds the login shell that reports PATH.
const loginPathTimeout = 5 * time.Second

// StartLoginPath looks up PATH as the user's login shell sets it in the
// background and returns a function that reports Burin's own PATH until
// the lookup is done, and the login PATH after (or Burin's own PATH if
// the shell cannot tell).
func StartLoginPath() func() string {
	return startLoginPath(os.Getenv("SHELL"), os.Getenv("PATH"), loginPathTimeout)
}

func startLoginPath(shell, fallback string, timeout time.Duration) func() string {
	var resolved atomic.Pointer[string]
	go func() {
		path := loginPath(shell, fallback, timeout)
		resolved.Store(&path)
	}()
	return func() string {
		if p := resolved.Load(); p != nil {
			return *p
		}
		return fallback
	}
}

// loginPath runs shell as a login shell to print PATH, returning
// fallback on any failure.
func loginPath(shell, fallback string, timeout time.Duration) string {
	if shell == "" {
		return fallback
	}
	// fish keeps PATH as a list, which echo would join with spaces.
	script := "echo $PATH"
	if filepath.Base(shell) == "fish" {
		script = "string join : $PATH"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", script)
	// Do not wait on children of the login scripts that hold stdout open.
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	// ErrWaitDelay: the shell exited normally but a child still held
	// stdout; out is complete.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		log.Printf("placeholder: login PATH from %s: %v", shell, err)
		return fallback
	}
	// Login scripts may print banners; PATH is the last line.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	path := strings.TrimSpace(lines[len(lines)-1])
	if path == "" {
		return fallback
	}
	return path
}

// EnvWithPath returns env with PATH set to path.
func EnvWithPath(env []string, path string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}
