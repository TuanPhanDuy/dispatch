package inprocess

import (
	"strings"
	"testing"
	"time"

	"github.com/TuanPhanDuy/dispatch/internal/client"
	"github.com/TuanPhanDuy/dispatch/internal/config"
)

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestCreateListAttachKill(t *testing.T) {
	m := New(100, 10)
	defer m.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	id, err := m.Create(profile, "mysession", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sessions, err := m.List()
	if err != nil || len(sessions) != 1 || sessions[0].ID != id {
		t.Fatalf("List() = %+v, %v", sessions, err)
	}
	if sessions[0].Status != client.StatusRunning {
		t.Fatalf("expected status running, got %s", sessions[0].Status)
	}

	scrollback, events, err := m.Attach(id, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(scrollback) != 0 {
		t.Fatalf("expected empty initial scrollback, got %q", scrollback)
	}

	if err := m.Write(id, []byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var got string
	waitUntil(t, 2*time.Second, func() bool {
		select {
		case chunk := <-events:
			got += string(chunk.Bytes)
		default:
		}
		return strings.Contains(got, "ping")
	})

	if err := m.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		sessions, _ := m.List()
		return len(sessions) == 1 && sessions[0].Status == client.StatusExited
	})
}

func TestSubscribeReceivesInitialAndUpdatedSnapshots(t *testing.T) {
	m := New(100, 10)
	defer m.Close()

	ch := m.Subscribe()
	select {
	case snap := <-ch:
		if len(snap) != 0 {
			t.Fatalf("expected empty initial snapshot, got %+v", snap)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial snapshot")
	}

	profile := config.AgentProfile{Name: "sleep", Command: "sleep", Args: []string{"1"}}
	if _, err := m.Create(profile, "s1", ".", 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}

	select {
	case snap := <-ch:
		if len(snap) != 1 {
			t.Fatalf("expected 1 session in updated snapshot, got %+v", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for updated snapshot after Create")
	}
}

func TestRenameUpdatesName(t *testing.T) {
	m := New(100, 10)
	defer m.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	id, err := m.Create(profile, "old-name", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Rename(id, "new-name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	sessions, _ := m.List()
	if sessions[0].Name != "new-name" {
		t.Fatalf("expected renamed session, got %+v", sessions[0])
	}
}

func TestRemoveRefusesRunningSession(t *testing.T) {
	m := New(100, 10)
	defer m.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	id, err := m.Create(profile, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := m.Remove(id); err == nil {
		t.Fatal("expected Remove to refuse a still-running session")
	}
	sessions, _ := m.List()
	if len(sessions) != 1 {
		t.Fatalf("expected session to remain after refused Remove, got %+v", sessions)
	}
}

func TestRemoveDeletesExitedSessionAndFreesName(t *testing.T) {
	m := New(100, 10)
	defer m.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	id, err := m.Create(profile, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		sessions, _ := m.List()
		return len(sessions) == 1 && sessions[0].Status == client.StatusExited
	})

	if err := m.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	sessions, _ := m.List()
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions after Remove, got %+v", sessions)
	}

	id2, err := m.Create(profile, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create after Remove: %v", err)
	}
	sessions, _ = m.List()
	if len(sessions) != 1 || sessions[0].ID != id2 || sessions[0].Name != "s1" {
		t.Fatalf("expected a single fresh session named 's1', got %+v", sessions)
	}
}

func TestRestartClearsScrollbackAndKeepsID(t *testing.T) {
	m := New(100, 10)
	defer m.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	id, err := m.Create(profile, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = m.Write(id, []byte("before-restart\n"))
	time.Sleep(100 * time.Millisecond)

	if err := m.Restart(id); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	scrollback, _, err := m.Attach(id, 80, 24)
	if err != nil {
		t.Fatalf("Attach after restart: %v", err)
	}
	if strings.Contains(string(scrollback), "before-restart") {
		t.Fatalf("expected scrollback cleared after restart, got %q", scrollback)
	}

	sessions, _ := m.List()
	if len(sessions) != 1 || sessions[0].ID != id {
		t.Fatalf("expected same session ID to survive restart, got %+v", sessions)
	}
}
