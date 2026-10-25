package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tuanbin007/dispatch/internal/client"
	"github.com/tuanbin007/dispatch/internal/config"
)

func TestListModelHelpOverlayTogglesAndBlocksOtherKeys(t *testing.T) {
	m := NewListModel()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = model
	if !m.showHelp {
		t.Fatal("expected showHelp=true after '?'")
	}

	// While help is open, unrelated keys (like 'q') must not quit the app.
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = model
	if cmd != nil {
		t.Fatal("expected 'q' to be swallowed while help overlay is open")
	}
	if !m.showHelp {
		t.Fatal("expected showHelp to remain true after an unrelated key")
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model
	if m.showHelp {
		t.Fatal("expected showHelp=false after esc")
	}
}

func TestListModelEnterOnStoppedRowEmitsRelaunch(t *testing.T) {
	m := NewListModel()
	m.SetRows([]Row{{Live: false, Def: config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "/tmp"}}})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(relaunchDefMsg)
	if !ok {
		t.Fatalf("expected relaunchDefMsg, got %#v", msg)
	}
	if msg.def.Name != "ghost" {
		t.Fatalf("expected def name ghost, got %+v", msg.def)
	}
}

func TestListModelDeleteOnStoppedRowAsksConfirmThenRemoves(t *testing.T) {
	m := NewListModel()
	m.SetRows([]Row{{Live: false, Def: config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "/tmp"}}})

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = model
	if m.confirmRemoveName != "ghost" {
		t.Fatalf("expected confirmRemoveName=ghost, got %q", m.confirmRemoveName)
	}

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = model
	if cmd == nil {
		t.Fatal("expected a command after confirming")
	}
	msg, ok := cmd().(removeDefMsg)
	if !ok || msg.name != "ghost" {
		t.Fatalf("expected removeDefMsg{ghost}, got %#v", msg)
	}
	if m.confirmRemoveName != "" {
		t.Fatal("expected confirmRemoveName cleared after confirming")
	}
}

func TestListModelDeleteConfirmNoLeavesRowIntact(t *testing.T) {
	m := NewListModel()
	m.SetRows([]Row{{Live: false, Def: config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "/tmp"}}})

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = model

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = model
	if cmd != nil {
		t.Fatal("expected no command when declining the confirm")
	}
	if len(m.rows) != 1 {
		t.Fatalf("expected row still present, got %+v", m.rows)
	}
}

func TestListModelDeleteOnExitedLiveRowEmitsRemoveWithID(t *testing.T) {
	m := NewListModel()
	m.SetRows([]Row{{Live: true, Summary: client.SessionSummary{ID: "id-1", Name: "s1", Status: client.StatusExited}}})

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = model
	if m.confirmRemoveID != "id-1" || m.confirmRemoveName != "s1" {
		t.Fatalf("expected confirmRemoveID=id-1 confirmRemoveName=s1, got id=%q name=%q", m.confirmRemoveID, m.confirmRemoveName)
	}
	if m.confirmKillID != "" {
		t.Fatalf("expected confirmKillID empty for an exited row, got %q", m.confirmKillID)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected a command after confirming")
	}
	msg, ok := cmd().(removeDefMsg)
	if !ok || msg.id != "id-1" || msg.name != "s1" {
		t.Fatalf("expected removeDefMsg{id-1, s1}, got %#v", msg)
	}
}

func TestListModelDeleteOnRunningLiveRowEmitsKill(t *testing.T) {
	m := NewListModel()
	m.SetRows([]Row{{Live: true, Summary: client.SessionSummary{ID: "id-1", Name: "s1", Status: client.StatusRunning}}})

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = model
	if m.confirmKillID != "id-1" {
		t.Fatalf("expected confirmKillID=id-1, got %q", m.confirmKillID)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected a command after confirming")
	}
	if msg, ok := cmd().(killRequestMsg); !ok || msg.id != "id-1" {
		t.Fatalf("expected killRequestMsg{id-1}, got %#v", msg)
	}
}

func TestListModelRestartIgnoredOnStoppedRow(t *testing.T) {
	m := NewListModel()
	m.SetRows([]Row{{Live: false, Def: config.SessionDef{Name: "ghost", Profile: "cat", Cwd: "/tmp"}}})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if cmd != nil {
		t.Fatal("expected 'R' on a stopped row to be a no-op")
	}
}
