package tui

import (
	"github.com/tuanbin007/dispatch/internal/client"
	"github.com/tuanbin007/dispatch/internal/config"
)

// outputMsg carries a chunk of raw PTY bytes delivered to an attached view.
type outputMsg struct{ chunk []byte }

// procExitMsg signals the attached session's process has ended.
type procExitMsg struct{}

// sessionListMsg is pushed whenever the session list changes.
type sessionListMsg []client.SessionSummary

// attachRequestMsg asks AppModel to attach to an existing session.
type attachRequestMsg struct{ id, name string }

// backToListMsg returns from the attached/wizard view to the list.
type backToListMsg struct{}

// newSessionRequestMsg opens the new-session wizard.
type newSessionRequestMsg struct{}

// cancelWizardMsg aborts the new-session wizard.
type cancelWizardMsg struct{}

// createSessionMsg submits the new-session wizard.
type createSessionMsg struct {
	profile   config.AgentProfile
	name, cwd string
}

// killRequestMsg asks AppModel to kill a session.
type killRequestMsg struct{ id string }

// restartRequestMsg asks AppModel to restart a session.
type restartRequestMsg struct{ id string }

// renameSubmitMsg asks AppModel to rename a row (live session or stopped
// definition — AppModel branches on row.Live to apply it correctly).
type renameSubmitMsg struct {
	row  Row
	name string
}

// relaunchDefMsg asks AppModel to spawn a persisted-but-stopped definition.
type relaunchDefMsg struct{ def config.SessionDef }

// removeDefMsg asks AppModel to delete a persisted definition. If id is
// non-empty, the row being removed is a live-but-exited session rather
// than a stopped-only definition, so AppModel must also remove that
// session from the manager (not just the config entry).
type removeDefMsg struct{ id, name string }

// quitAppMsg asks AppModel to quit the whole program.
type quitAppMsg struct{}
