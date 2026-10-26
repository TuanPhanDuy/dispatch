// Package client defines the SessionManager seam: internal/tui talks only
// to this interface, never to ptyproc or a transport directly. v1's
// implementation (internal/client/inprocess) wraps ptyproc.Process in the
// same OS process as the TUI; a future socket-backed implementation can
// satisfy the same interface without internal/tui changing at all.
package client

import "time"

// SessionStatus is the lifecycle/attention state of a session.
type SessionStatus string

const (
	StatusRunning        SessionStatus = "running"
	StatusNeedsAttention SessionStatus = "needs-attention"
	StatusExited         SessionStatus = "exited"
)

// SessionSummary is the list-view-facing snapshot of a session.
type SessionSummary struct {
	ID        string
	Name      string
	Command   string
	Cwd       string
	Status    SessionStatus
	StartedAt time.Time
	ExitCode  *int
	Account   string // empty: the agent's default configuration
}

// OutputChunk is a batch of raw PTY output bytes pushed to an attached
// viewer, or a final Exited marker when the session's process has ended.
type OutputChunk struct {
	Bytes  []byte
	Exited bool
}
