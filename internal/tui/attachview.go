package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tuanbin007/dispatch/internal/client"
	"github.com/tuanbin007/dispatch/internal/ptyproc"
)

// AttachModel is a full-screen view attached to a single session via the
// client.SessionManager seam: every keystroke is forwarded raw to the
// session's PTY except the single reserved MetaKey, which opens a one-shot
// command mode. It never talks to ptyproc.Process directly — only to the
// manager — so swapping in a socket-backed manager in Phase 3 needs no
// changes here.
type AttachModel struct {
	manager   client.SessionManager
	sessionID string
	name      string

	ring   *ptyproc.RingBuffer // local mirror cache fed by manager events
	vp     viewport.Model
	events <-chan client.OutputChunk

	commandMode bool
	exited      bool
}

// NewAttachModel attaches to sessionID via manager, sized cols x rows, and
// seeds the local scrollback mirror from the returned snapshot.
func NewAttachModel(manager client.SessionManager, sessionID, name string, cols, rows int) (*AttachModel, error) {
	scrollback, events, err := manager.Attach(sessionID, cols, rows)
	if err != nil {
		return nil, err
	}

	ring := ptyproc.NewRingBuffer(0)
	_, _ = ring.Write(scrollback)

	vp := viewport.New(cols, rows-1)
	m := &AttachModel{
		manager:   manager,
		sessionID: sessionID,
		name:      name,
		ring:      ring,
		vp:        vp,
		events:    events,
	}
	m.refresh()
	return m, nil
}

// waitForSessionOutput is the self-re-arming tea.Cmd that pumps the
// manager's per-session event channel into the Bubble Tea Update loop.
func waitForSessionOutput(ch <-chan client.OutputChunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-ch
		if !ok || chunk.Exited {
			return procExitMsg{}
		}
		return outputMsg{chunk: chunk.Bytes}
	}
}

func (m *AttachModel) Init() tea.Cmd {
	return waitForSessionOutput(m.events)
}

func (m *AttachModel) Update(msg tea.Msg) (*AttachModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.vp.Width = msg.Width
		m.vp.Height = msg.Height - 1
		_ = m.manager.Resize(m.sessionID, msg.Width, msg.Height)
		m.refresh()
		return m, nil

	case outputMsg:
		_, _ = m.ring.Write(msg.chunk)
		m.refresh()
		return m, waitForSessionOutput(m.events)

	case procExitMsg:
		m.exited = true
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *AttachModel) refresh() {
	m.vp.SetContent(m.ring.Snapshot())
	m.vp.GotoBottom()
}

func (m *AttachModel) handleKey(msg tea.KeyMsg) (*AttachModel, tea.Cmd) {
	if m.exited {
		if msg.Type == tea.KeyEnter {
			return m, func() tea.Msg { return backToListMsg{} }
		}
		return m, nil
	}

	if m.commandMode {
		m.commandMode = false
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'd':
				_ = m.manager.Detach(m.sessionID)
				return m, func() tea.Msg { return backToListMsg{} }
			case 'k':
				_ = m.manager.Kill(m.sessionID)
				_ = m.manager.Detach(m.sessionID)
				return m, func() tea.Msg { return backToListMsg{} }
			case 'q':
				return m, func() tea.Msg { return quitAppMsg{} }
			}
		}
		return m, nil
	}

	if msg.Type == MetaKey {
		m.commandMode = true
		return m, nil
	}

	if b := keyToBytes(msg); b != nil {
		_ = m.manager.Write(m.sessionID, b)
	}
	return m, nil
}

func (m *AttachModel) View() string {
	var footer string
	switch {
	case m.exited:
		footer = statusBarStyle.Render(fmt.Sprintf(" %s — process exited — enter to go back ", m.name))
	case m.commandMode:
		footer = commandModeStyle.Render(" ^\\ then: d detach · k kill · q quit · esc cancel ")
	default:
		footer = statusBarStyle.Render(fmt.Sprintf(" %s — ^\\ for commands ", m.name))
	}
	return m.vp.View() + "\n" + footer
}
