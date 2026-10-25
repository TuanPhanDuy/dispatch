package daemon

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
)

// Run starts the registry + socket server and blocks until the listener
// errors or is closed. socketPath's parent directory is created 0700
// (local-user-only — the socket has no auth beyond filesystem permissions,
// which is sufficient for a per-user local daemon).
func Run(socketPath string, scrollbackCap, defaultIdleSec int) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	_ = os.Remove(socketPath) // clear a stale socket left by a crashed daemon

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	defer l.Close()

	registry := NewRegistry(scrollbackCap, defaultIdleSec)
	defer registry.Close()

	logger := slog.Default()
	logger.Info("dispatch daemon listening", "socket", socketPath)

	srv := NewServer(registry, logger)
	return srv.Serve(l)
}
