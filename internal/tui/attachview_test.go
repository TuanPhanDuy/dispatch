package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TuanPhanDuy/dispatch/internal/client/inprocess"
	"github.com/TuanPhanDuy/dispatch/internal/config"
)

func newTestAttachModel(t *testing.T) *AttachModel {
	t.Helper()
	mgr := inprocess.New(100, 10)
	t.Cleanup(func() { _ = mgr.Close() })

	id, err := mgr.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "test", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m, err := NewAttachModel(mgr, id, "test", 80, 24)
	if err != nil {
		t.Fatalf("NewAttachModel: %v", err)
	}
	return m
}

func TestAttachModelForwardsRawKeyToPTY(t *testing.T) {
	m := newTestAttachModel(t)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = model

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case chunk := <-m.events:
			m.ring.Write(chunk.Bytes)
		default:
		}
		if contains(m.ring.Snapshot(), "x") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for echoed input to reach ring buffer")
}

func TestAttachModelMetaKeyEntersCommandMode(t *testing.T) {
	m := newTestAttachModel(t)

	model, _ := m.Update(tea.KeyMsg{Type: MetaKey})
	m = model
	if !m.commandMode {
		t.Fatal("expected commandMode=true after MetaKey")
	}
}

func TestAttachModelCommandModeQuit(t *testing.T) {
	m := newTestAttachModel(t)
	m.commandMode = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}
	if msg := cmd(); msg != (quitAppMsg{}) {
		t.Fatalf("expected quitAppMsg, got %#v", msg)
	}
}

func TestAttachModelCommandModeExitsAfterOneKey(t *testing.T) {
	m := newTestAttachModel(t)
	m.commandMode = true

	// A key that isn't a recognized command (e.g. 'z') should still fall
	// back out of command mode rather than getting stuck in it.
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = model
	if m.commandMode {
		t.Fatal("expected commandMode=false after any key following MetaKey")
	}
}

func TestAttachModelWindowSizeResizesPTY(t *testing.T) {
	m := newTestAttachModel(t)

	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = model
	if m.vp.Width != 100 || m.vp.Height != 39 {
		t.Fatalf("viewport size = %dx%d, want 100x39", m.vp.Width, m.vp.Height)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
