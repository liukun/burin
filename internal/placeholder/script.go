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
	"syscall"
	"time"
)

// DefaultTimeout bounds each external command.
const DefaultTimeout = 3 * time.Second

// Runner runs external scripts and shell commands.
type Runner struct {
	// Dir holds the scripts, looked up by name.
	Dir string
	// Env is the environment of the commands; nil inherits Burin's.
	Env     []string
	Timeout time.Duration
	// OnStderr, if set, receives non-empty stderr of a command, whether
	// or not it succeeded.
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
	cmd.Env = r.Env
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
	if errText != "" && r.OnStderr != nil {
		r.OnStderr(name, errText)
	}
	if err == nil {
		return strings.TrimRight(stdout.String(), "\r\n"), nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s: timed out after %v", name, timeout)
	}
	return "", fmt.Errorf("%s: %w%s", name, err, stderrSuffix(errText))
}

func stderrSuffix(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// LoginPath returns PATH as the user's login shell sets it, or Burin's
// own PATH when the shell cannot tell.
func LoginPath() string {
	fallback := os.Getenv("PATH")
	shell := os.Getenv("SHELL")
	if shell == "" {
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", "echo $PATH").Output()
	if err != nil {
		log.Printf("placeholder: login PATH: %v", err)
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
