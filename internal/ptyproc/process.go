// Package ptyproc wraps a child process attached to a pseudo-terminal.
package ptyproc

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
)

// Process is a single child process running inside its own PTY.
type Process struct {
	cmd *exec.Cmd
	pty *os.File
}

// Start spawns command/args in cwd, attached to a new PTY sized cols x rows.
// env, if non-nil, replaces the child's environment (nil inherits os.Environ()).
func Start(command string, args []string, cwd string, env []string, cols, rows int) (*Process, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	if env != nil {
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, err
	}
	return &Process{cmd: cmd, pty: f}, nil
}

// Read reads raw output bytes (including ANSI escapes) from the PTY.
func (p *Process) Read(b []byte) (int, error) { return p.pty.Read(b) }

// Write sends raw input bytes to the PTY (forwarded to the child's stdin).
func (p *Process) Write(b []byte) (int, error) { return p.pty.Write(b) }

// Resize updates the PTY window size, triggering SIGWINCH in the child.
func (p *Process) Resize(cols, rows int) error {
	return pty.Setsize(p.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Signal sends an OS signal to the child process.
func (p *Process) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

// Kill terminates the child process via SIGTERM.
func (p *Process) Kill() error {
	return p.Signal(syscall.SIGTERM)
}

// KillForce terminates the child process immediately via SIGKILL — for
// escalating after Kill's grace period expires and the process is still
// running. Some interactive shells don't exit on SIGTERM alone (observed
// with zsh sessions that install their own trap/init hooks, e.g. via
// conda's shell integration), so a caller that needs "really gone, no
// matter what" must be able to follow up with this.
func (p *Process) KillForce() error {
	return p.Signal(syscall.SIGKILL)
}

// Wait blocks until the child exits and returns its exit error (nil on success).
func (p *Process) Wait() error {
	return p.cmd.Wait()
}

// Close releases the PTY file descriptor.
func (p *Process) Close() error {
	return p.pty.Close()
}

// WatchResize forwards the controlling terminal's SIGWINCH (and an initial
// sync) to a callback — used by the client to keep the PTY sized to the
// real terminal window.
func WatchResize(onResize func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				onResize()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
