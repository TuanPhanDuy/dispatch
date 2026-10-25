package daemon

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// DefaultSocketPath returns the per-user socket path. $XDG_RUNTIME_DIR is
// preferred when set (the Linux convention for exactly this kind of
// ephemeral per-user socket); otherwise it falls back to a user-scoped
// directory under os.TempDir() (covers macOS, which has no XDG_RUNTIME_DIR
// convention).
func DefaultSocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("dispatch-%d", os.Getuid()))
	}
	return filepath.Join(dir, "dispatch.sock")
}

// EnsureRunning checks whether a daemon is already listening on
// socketPath; if not, it self-execs `<argv0> daemon --socket <path>`,
// detached from the current terminal (Setsid), and waits for the socket to
// come up. This is what makes `dispatch` "just work" without the user ever
// running a separate server command, mirroring tmux's own experience.
func EnsureRunning(socketPath string, scrollbackCap, defaultIdleSec int) error {
	if dialOK(socketPath) {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own executable: %w", err)
	}

	cmd := exec.Command(exe, "daemon",
		"--socket", socketPath,
		"--scrollback", fmt.Sprint(scrollbackCap),
		"--idle-seconds", fmt.Sprint(defaultIdleSec),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// The daemon is now an independent (Setsid) process; don't wait on it
	// or it becomes a zombie once it outlives this client.
	go func() { _ = cmd.Process.Release() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if dialOK(socketPath) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not come up on %s within 3s", socketPath)
}

func dialOK(socketPath string) bool {
	c, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
