package client

import "github.com/tuanbin007/dispatch/internal/config"

// SessionManager is the one interface internal/tui is allowed to depend on
// for session lifecycle and I/O. It's the seam that lets the daemon/socket
// decision (Phase 3) swap in a different implementation without touching
// the TUI layer.
type SessionManager interface {
	// List returns a snapshot of all known sessions.
	List() ([]SessionSummary, error)

	// Create spawns a new session from profile in cwd, sized cols x rows,
	// and returns its session ID.
	Create(profile config.AgentProfile, name, cwd string, cols, rows int) (sessionID string, err error)

	// Attach returns the current scrollback plus a channel of future
	// output chunks for sessionID, and resizes its PTY to cols x rows
	// ("last attacher wins" sizing, per the v1 scrollback design).
	Attach(sessionID string, cols, rows int) (scrollback []byte, events <-chan OutputChunk, err error)

	// Detach stops streaming output for sessionID to the caller (the
	// session itself keeps running in the background).
	Detach(sessionID string) error

	// Write forwards raw input bytes to sessionID's PTY.
	Write(sessionID string, data []byte) error

	// Resize updates sessionID's PTY window size.
	Resize(sessionID string, cols, rows int) error

	// Kill terminates sessionID.
	Kill(sessionID string) error

	// Remove permanently discards sessionID's entry. It errors if the
	// session is still running — Kill it first.
	Remove(sessionID string) error

	// Restart kills (if alive) and relaunches sessionID with its original
	// profile/cwd, clearing scrollback.
	Restart(sessionID string) error

	// Rename changes sessionID's display name.
	Rename(sessionID, name string) error

	// Subscribe returns a channel that receives the full session list
	// whenever it changes (create/kill/restart/rename/status transition).
	Subscribe() <-chan []SessionSummary

	// Close releases this manager's own client-side resources. Its effect
	// on live sessions is NOT unified across implementations — check the
	// concrete type's own doc comment before relying on which behavior you
	// get. The socket-backed implementation only disconnects (sessions
	// keep running in the daemon, by design); an in-process implementation
	// has no separate daemon to keep sessions alive, so Close necessarily
	// kills them. This asymmetry is inherent to the two architectures, not
	// a bug — putting Close() on the interface is what makes it a visible,
	// documented part of the contract instead of a silent surprise if the
	// concrete type ever gets swapped.
	Close() error
}
