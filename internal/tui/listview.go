package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/TuanPhanDuy/dispatch/internal/client"
	"github.com/TuanPhanDuy/dispatch/internal/config"
)

// Row is one rendered list entry: either a live session backed by the
// daemon, or a stopped, persisted definition with no matching live session
// (shown dimmed, one key-press away from being relaunched).
type Row struct {
	Live    bool
	Summary client.SessionSummary
	Def     config.SessionDef
	// DefAccount is the account a stopped row's profile would relaunch
	// under; live rows report their own via Summary.Account.
	DefAccount string
}

// Account is the account the row's session runs (or would run) under,
// empty for the agent's default configuration.
func (r Row) Account() string {
	if r.Live {
		return r.Summary.Account
	}
	return r.DefAccount
}

func (r Row) Name() string {
	if r.Live {
		return r.Summary.Name
	}
	return r.Def.Name
}

func (r Row) Cwd() string {
	if r.Live {
		return r.Summary.Cwd
	}
	return r.Def.Cwd
}

// ListModel renders the merged session list and handles its single-key
// navigation (no tmux-style prefix key needed here since nothing else is
// contending for the keyboard while not attached).
type ListModel struct {
	rows   []Row
	cursor int

	confirmKillID     string
	confirmRemoveID   string // non-empty only when removing a live-but-exited row (vs. a stopped-only def)
	confirmRemoveName string

	renamingRow *Row
	renameInput textinput.Model

	showHelp bool
}

func NewListModel() *ListModel {
	return &ListModel{}
}

// SetRows replaces the rendered rows, clamping the cursor.
func (m *ListModel) SetRows(rows []Row) {
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *ListModel) selected() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

func (m *ListModel) Update(msg tea.Msg) (*ListModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.showHelp {
		if keyMsg.String() == "?" || keyMsg.String() == "esc" {
			m.showHelp = false
		}
		return m, nil
	}

	if m.renamingRow != nil {
		return m.updateRenaming(keyMsg)
	}
	if m.confirmKillID != "" || m.confirmRemoveName != "" {
		return m.updateConfirm(keyMsg)
	}

	switch keyMsg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "enter":
		return m.activateSelected()
	case "n":
		return m, func() tea.Msg { return newSessionRequestMsg{} }
	case "d":
		if row, ok := m.selected(); ok {
			switch {
			case row.Live && row.Summary.Status != client.StatusExited:
				m.confirmKillID = row.Summary.ID
			case row.Live:
				// Exited but still occupying the registry — kill would be a
				// no-op (nothing left to kill), so this is really a remove.
				m.confirmRemoveID = row.Summary.ID
				m.confirmRemoveName = row.Summary.Name
			default:
				m.confirmRemoveName = row.Def.Name
			}
		}
	case "r":
		if row, ok := m.selected(); ok {
			r := row
			m.renamingRow = &r
			m.renameInput = textinput.New()
			m.renameInput.SetValue(row.Name())
			m.renameInput.Focus()
		}
	case "R":
		if row, ok := m.selected(); ok && row.Live {
			return m, func() tea.Msg { return restartRequestMsg{id: row.Summary.ID} }
		}
	case "?":
		m.showHelp = true
	case "q", "ctrl+c":
		return m, func() tea.Msg { return quitAppMsg{} }
	}
	return m, nil
}

func (m *ListModel) activateSelected() (*ListModel, tea.Cmd) {
	row, ok := m.selected()
	if !ok {
		return m, nil
	}
	if row.Live {
		return m, func() tea.Msg { return attachRequestMsg{id: row.Summary.ID, name: row.Summary.Name} }
	}
	return m, func() tea.Msg { return relaunchDefMsg{def: row.Def} }
}

func (m *ListModel) updateRenaming(keyMsg tea.KeyMsg) (*ListModel, tea.Cmd) {
	switch keyMsg.String() {
	case "enter":
		row := *m.renamingRow
		name := m.renameInput.Value()
		m.renamingRow = nil
		return m, func() tea.Msg { return renameSubmitMsg{row: row, name: name} }
	case "esc":
		m.renamingRow = nil
		return m, nil
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(keyMsg)
	return m, cmd
}

func (m *ListModel) updateConfirm(keyMsg tea.KeyMsg) (*ListModel, tea.Cmd) {
	killID, removeID, removeName := m.confirmKillID, m.confirmRemoveID, m.confirmRemoveName
	m.confirmKillID, m.confirmRemoveID, m.confirmRemoveName = "", "", ""
	if keyMsg.String() != "y" {
		return m, nil
	}
	if killID != "" {
		return m, func() tea.Msg { return killRequestMsg{id: killID} }
	}
	return m, func() tea.Msg { return removeDefMsg{id: removeID, name: removeName} }
}

func (m *ListModel) View() string {
	if m.showHelp {
		return helpText
	}

	var b strings.Builder
	b.WriteString(helpHintStyle.Render("n new · enter attach/launch · r rename · d kill/remove · R restart · ? help · q quit") + "\n\n")

	if len(m.rows) == 0 {
		b.WriteString("  (no sessions yet — press 'n' to launch one)\n")
	}

	for i, row := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		var line string
		if m.renamingRow != nil && row.Name() == m.renamingRow.Name() {
			line = fmt.Sprintf("%srename: %s", cursor, m.renameInput.View())
		} else {
			status := "stopped"
			if row.Live {
				status = string(row.Summary.Status)
			}
			account := ""
			if row.Account() != "" {
				account = "@" + row.Account()
			}
			line = fmt.Sprintf("%s%-20s %-16s %-14s %s", cursor, truncate(row.Name(), 20), status, truncate(account, 14), row.Cwd())
		}

		switch {
		case row.Live && row.Summary.Status == client.StatusNeedsAttention:
			line = attentionStyle.Render(line)
		case !row.Live, row.Live && row.Summary.Status == client.StatusExited:
			line = helpHintStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}

	if m.confirmKillID != "" {
		b.WriteString("\n" + commandModeStyle.Render(" kill this session? y/n ") + "\n")
	}
	if m.confirmRemoveName != "" {
		prompt := " remove this saved session? y/n "
		if m.confirmRemoveID != "" {
			prompt = " remove this exited session? y/n "
		}
		b.WriteString("\n" + commandModeStyle.Render(prompt) + "\n")
	}

	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

const helpText = `dispatch — help (? or esc to close)

List view:
  up/down, j/k   move selection
  enter          attach to a running session, or launch a stopped one
  n              new session
  r              rename
  d              kill (running) / remove (exited or stopped) — asks to confirm
  R              restart a live session
  q, ctrl+c      quit the TUI (sessions keep running in the daemon)

Attached view:
  (anything)     forwarded raw to the session
  ctrl+\         one-shot command mode:
                   d detach · k kill · q quit

Sessions are owned by a background daemon, not the TUI — closing the TUI
does not stop your agents. Run dispatch again to reattach.
`
