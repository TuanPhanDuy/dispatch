package client

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/daemon"
)

// startTestDaemon stands up a real Registry+Server listening on a unix
// socket, in-process (as a goroutine, not a subprocess) — the pattern the
// project plan calls out as the highest-value automated test tier for this
// layer. Because the socket already exists before Dial is called,
// daemon.EnsureRunning's dialOK check succeeds immediately and never
// self-execs a real daemon subprocess.
//
// A short hand-rolled temp dir (not t.TempDir(), whose path embeds the full
// test name) is used because unix socket paths are capped at ~104 bytes on
// macOS — t.TempDir()'s path alone can exceed that.
func startTestDaemon(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "am")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "a.sock")

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	registry := daemon.NewRegistry(100, 10)
	srv := daemon.NewServer(registry, nil)
	go srv.Serve(l)

	t.Cleanup(func() {
		l.Close()
		registry.Close()
	})
	return socketPath
}

func TestSocketManagerCreateListAttachWrite(t *testing.T) {
	socketPath := startTestDaemon(t)

	m, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer m.Close()

	id, err := m.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sessions, err := m.List()
	if err != nil || len(sessions) != 1 || sessions[0].ID != id {
		t.Fatalf("List() = %+v, %v", sessions, err)
	}

	_, events, err := m.Attach(id, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := m.Write(id, []byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	deadline := time.After(2 * time.Second)
	var got string
	for {
		select {
		case chunk := <-events:
			got += string(chunk.Bytes)
			if strings.Contains(got, "ping") {
				return
			}
		case <-deadline:
			t.Fatalf("timed out; got %q so far", got)
		}
	}
}

func TestSocketManagerSubscribeReceivesListUpdates(t *testing.T) {
	socketPath := startTestDaemon(t)
	m, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
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

	if _, err := m.Create(config.AgentProfile{Name: "sleep", Command: "sleep", Args: []string{"1"}}, "s1", ".", 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}

	select {
	case snap := <-ch:
		if len(snap) != 1 {
			t.Fatalf("expected 1 session, got %+v", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for updated snapshot")
	}
}

func TestSocketManagerKillUpdatesStatus(t *testing.T) {
	socketPath := startTestDaemon(t)
	m, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer m.Close()

	id, err := m.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sessions, _ := m.List()
		if len(sessions) == 1 && sessions[0].Status == StatusExited {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected session to transition to exited after Kill")
}

// TestSocketManagerSessionSurvivesClientDisconnect is the headline Phase 3
// guarantee: a session created by one client connection keeps running —
// and is visible to a second connection — after the first client
// disconnects. This proves the daemon, not the TUI process, owns the
// session's lifetime, which is the entire reason Phase 3 exists.
func TestSocketManagerSessionSurvivesClientDisconnect(t *testing.T) {
	socketPath := startTestDaemon(t)

	m1, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial m1: %v", err)
	}
	id, err := m1.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m1.Close(); err != nil {
		t.Fatalf("Close m1: %v", err)
	}

	m2, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial m2: %v", err)
	}
	defer m2.Close()

	sessions, err := m2.List()
	if err != nil || len(sessions) != 1 || sessions[0].ID != id || sessions[0].Status != StatusRunning {
		t.Fatalf("expected session %s still running after client disconnect, got %+v, %v", id, sessions, err)
	}
}

// TestSocketManagerScrollbackSurvivesReattach proves scrollback isn't lost
// when a client detaches and a different client attaches later.
func TestSocketManagerScrollbackSurvivesReattach(t *testing.T) {
	socketPath := startTestDaemon(t)

	m1, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial m1: %v", err)
	}
	id, err := m1.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := m1.Attach(id, 80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := m1.Write(id, []byte("hello-scrollback\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let the daemon's pump catch up
	if err := m1.Close(); err != nil {
		t.Fatalf("Close m1: %v", err)
	}

	m2, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial m2: %v", err)
	}
	defer m2.Close()

	scrollback, _, err := m2.Attach(id, 80, 24)
	if err != nil {
		t.Fatalf("Attach m2: %v", err)
	}
	if !strings.Contains(string(scrollback), "hello-scrollback") {
		t.Fatalf("expected scrollback to survive reattach, got %q", scrollback)
	}
}

// TestDetachRacingDeliverOutputDoesNotPanic stresses the exact race that
// used to panic the client: deliverOutput reading `ch` out of outputSubs
// and sending to it after releasing outputMu, racing a concurrent Detach
// that closes that same channel. If the fix regresses, this reliably
// panics the whole test binary (a send on a closed channel is
// unrecoverable), not just this one test.
func TestDetachRacingDeliverOutputDoesNotPanic(t *testing.T) {
	socketPath := startTestDaemon(t)
	m, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer m.Close()

	// `yes` floods output as fast as it can, maximizing the chance a
	// deliverOutput call lands exactly in the window a concurrent Detach
	// is closing the channel.
	id, err := m.Create(config.AgentProfile{Name: "yes", Command: "yes"}, "flood", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := 0; i < 50; i++ {
		if _, _, err := m.Attach(id, 80, 24); err != nil {
			t.Fatalf("Attach: %v", err)
		}
		if err := m.Detach(id); err != nil {
			t.Fatalf("Detach: %v", err)
		}
	}
}

// TestDisconnectUnblocksAttachAndSubscribeChannels verifies that when the
// daemon connection drops, every channel this SocketManager handed out
// (Attach's output channel, Subscribe's list channel) gets closed instead
// of leaving a caller blocked on `<-ch` forever — which is exactly what
// AttachModel.waitForSessionOutput and AppModel.waitForSessionList do.
func TestDisconnectUnblocksAttachAndSubscribeChannels(t *testing.T) {
	socketPath := startTestDaemon(t)
	m, err := Dial(socketPath, 100, 10)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	id, err := m.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, events, err := m.Attach(id, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	listCh := m.Subscribe()
	<-listCh // drain Subscribe's initial buffered snapshot before disconnecting

	// Simulate the daemon connection dying from the client's point of view.
	if err := m.conn.Close(); err != nil {
		t.Fatalf("conn.Close: %v", err)
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected events channel to be closed, got a value instead")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events channel was never closed after disconnect — caller would hang forever")
	}

	select {
	case _, ok := <-listCh:
		if ok {
			t.Fatal("expected list channel to be closed, got a value instead")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("list channel was never closed after disconnect — caller would hang forever")
	}
}
