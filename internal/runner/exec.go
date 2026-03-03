// Package runner manages child process lifecycle including privilege de-escalation,
// signal forwarding, and exit code propagation.
package runner

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// Runner manages the lifecycle of a child process.
type Runner struct {
	command string
	args    []string
	cmd     *exec.Cmd
}

// NewRunner creates a new Runner for the given command and arguments.
func NewRunner(command string, args []string) *Runner {
	return &Runner{
		command: command,
		args:    args,
	}
}

// Start launches the child process. If SUDO_UID and SUDO_GID environment
// variables are set, the process runs with those credentials (privilege
// de-escalation). Stdin, stdout, and stderr are connected to the parent.
func (r *Runner) Start() error {
	r.cmd = exec.Command(r.command, r.args...)
	r.cmd.Stdin = os.Stdin
	r.cmd.Stdout = os.Stdout
	r.cmd.Stderr = os.Stderr

	uid, _ := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, _ := strconv.Atoi(os.Getenv("SUDO_GID"))
	if uid > 0 && gid > 0 {
		r.cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
	}

	return r.cmd.Start()
}

// Wait waits for the child process to exit and returns its exit code.
// Returns 0 on success, the process exit code on ExitError, or 1 for
// other errors.
func (r *Runner) Wait() (int, error) {
	err := r.cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 1, err
}

// Signal sends the given signal to the child process.
func (r *Runner) Signal(sig os.Signal) error {
	if r.cmd == nil || r.cmd.Process == nil {
		return errors.New("process not started")
	}
	return r.cmd.Process.Signal(sig)
}
