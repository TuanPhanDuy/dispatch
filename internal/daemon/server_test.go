package daemon

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuanbin007/dispatch/internal/config"
	"github.com/tuanbin007/dispatch/internal/protocol"
)

// startTestServer stands up a real Registry+Server on a short-path unix
// socket (not t.TempDir(), whose path embeds the full test name and can
// exceed the ~104-byte unix socket path limit on macOS).
func startTestServer(t *testing.T) (socketPath string, registry *Registry) {
	t.Helper()
	dir, err := os.MkdirTemp("", "dd")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socketPath = filepath.Join(dir, "d.sock")

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	registry = NewRegistry(100, 10)
	srv := NewServer(registry, nil)
	go srv.Serve(l)
	t.Cleanup(func() {
		l.Close()
		registry.Close()
	})
	return socketPath, registry
}

type testClient struct {
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer
}

func dialTestClient(t *testing.T, socketPath string) *testClient {
	t.Helper()
	nc, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	return &testClient{nc: nc, r: bufio.NewReader(nc), w: bufio.NewWriter(nc)}
}

func (tc *testClient) send(t *testing.T, env protocol.Envelope) {
	t.Helper()
	if err := protocol.WriteEnvelope(tc.w, env); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// recv reads envelopes until it finds one with the given type (skipping
// any interleaved pushes, e.g. the session-list snapshot every connection
// gets immediately on connect) or the deadline passes.
func (tc *testClient) recv(t *testing.T, want protocol.MsgType, timeout time.Duration) protocol.Envelope {
	t.Helper()
	_ = tc.nc.SetReadDeadline(time.Now().Add(timeout))
	for {
		env, err := protocol.ReadEnvelope(tc.r)
		if err != nil {
			t.Fatalf("recv %s: %v", want, err)
		}
		if env.Type == want {
			return env
		}
	}
}

func TestHandleAttachDetachesPreviousAttachment(t *testing.T) {
	socketPath, registry := startTestServer(t)

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	idA, err := registry.Create(profile, "a", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	idB, err := registry.Create(profile, "b", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	tc := dialTestClient(t, socketPath)

	tc.send(t, protocol.Envelope{
		Type: protocol.ReqAttach, ID: "r1",
		Data: protocol.Marshal(protocol.AttachReq{SessionID: idA, Cols: 80, Rows: 24}),
	})
	tc.recv(t, protocol.RespAttach, 2*time.Second)

	if got := subCount(registry, idA); got != 1 {
		t.Fatalf("expected 1 subscriber on A right after attaching, got %d", got)
	}

	// Attach to B WITHOUT ever sending ReqDetach for A — this is exactly
	// the sequence that used to leak A's subscription forever.
	tc.send(t, protocol.Envelope{
		Type: protocol.ReqAttach, ID: "r2",
		Data: protocol.Marshal(protocol.AttachReq{SessionID: idB, Cols: 80, Rows: 24}),
	})
	tc.recv(t, protocol.RespAttach, 2*time.Second)

	waitUntil(t, 2*time.Second, func() bool { return subCount(registry, idA) == 0 })
	if got := subCount(registry, idB); got != 1 {
		t.Fatalf("expected 1 subscriber on B after attaching, got %d", got)
	}
}

func TestClientDisconnectDuringActiveOutputDoesNotCrashServer(t *testing.T) {
	socketPath, registry := startTestServer(t)

	// `yes` floods output as fast as it can — the goal is to maximize the
	// chance of a pump() output-fanout call landing exactly in the window
	// between a client's abrupt disconnect and its subscription cleanup,
	// which is the race the fix in conn.send/closeOutput closes. If that
	// fix regresses, this reliably panics the whole test binary (a send
	// on a closed channel is unrecoverable), not just this one test.
	profile := config.AgentProfile{Name: "yes", Command: "yes"}
	id, err := registry.Create(profile, "flood", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := 0; i < 20; i++ {
		tc := dialTestClient(t, socketPath)
		tc.send(t, protocol.Envelope{
			Type: protocol.ReqAttach, ID: "r1",
			Data: protocol.Marshal(protocol.AttachReq{SessionID: id, Cols: 80, Rows: 24}),
		})
		tc.recv(t, protocol.RespAttach, 2*time.Second)
		// Abrupt disconnect while output is actively flowing, with no
		// ReqDetach handshake — the server must not crash.
		tc.nc.Close()
	}

	if err := registry.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// If the server process (this test binary) survived the loop above
	// without panicking, a brand-new connection should still work fine.
	tc := dialTestClient(t, socketPath)
	tc.send(t, protocol.Envelope{Type: protocol.ReqList, ID: "r2"})
	tc.recv(t, protocol.RespList, 2*time.Second)
}
