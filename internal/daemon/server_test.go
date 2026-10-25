package daemon

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/protocol"
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

func (tc *testClient) attach(t *testing.T, reqID, sessionID string) {
	t.Helper()
	tc.send(t, protocol.Envelope{
		Type: protocol.ReqAttach, ID: reqID,
		Data: protocol.Marshal(protocol.AttachReq{SessionID: sessionID, Cols: 80, Rows: 24}),
	})
	tc.recv(t, protocol.RespAttach, 2*time.Second)
}

func createCatSessions(t *testing.T, registry *Registry, names ...string) []string {
	t.Helper()
	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	ids := make([]string, len(names))
	for i, name := range names {
		id, err := registry.Create(profile, name, ".", 80, 24)
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		ids[i] = id
	}
	return ids
}

// One connection attached to several sessions must keep receiving output
// from all of them — the GUI tiles many panes over a single socket. This
// used to silently detach the earlier session on every new attach.
func TestConnectionReceivesOutputFromEverySessionItIsAttachedTo(t *testing.T) {
	socketPath, registry := startTestServer(t)
	ids := createCatSessions(t, registry, "a", "b")
	idA, idB := ids[0], ids[1]

	tc := dialTestClient(t, socketPath)
	tc.attach(t, "r1", idA)
	tc.attach(t, "r2", idB)

	for _, id := range ids {
		tc.send(t, protocol.Envelope{
			Type: protocol.ReqInput,
			Data: protocol.Marshal(protocol.InputReq{SessionID: id, Bytes: []byte("ping-" + id + "\n")}),
		})
	}

	seen := map[string]string{}
	_ = tc.nc.SetReadDeadline(time.Now().Add(3 * time.Second))
	for !strings.Contains(seen[idA], "ping-"+idA) || !strings.Contains(seen[idB], "ping-"+idB) {
		env, err := protocol.ReadEnvelope(tc.r)
		if err != nil {
			t.Fatalf("waiting for output from both sessions (got %q): %v", seen, err)
		}
		if env.Type != protocol.EventOutput {
			continue
		}
		var ev protocol.OutputEvent
		if err := protocol.Unmarshal(env.Data, &ev); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		seen[ev.SessionID] += string(ev.Bytes)
	}
}

func TestReattachingSameSessionReplacesSubscription(t *testing.T) {
	socketPath, registry := startTestServer(t)
	idA := createCatSessions(t, registry, "a")[0]

	tc := dialTestClient(t, socketPath)
	tc.attach(t, "r1", idA)
	tc.attach(t, "r2", idA)

	if got := subCount(registry, idA); got != 1 {
		t.Fatalf("expected re-attach to replace the subscription, got %d subscribers", got)
	}
}

func TestDetachDropsOnlyTheNamedSession(t *testing.T) {
	socketPath, registry := startTestServer(t)
	ids := createCatSessions(t, registry, "a", "b")
	idA, idB := ids[0], ids[1]

	tc := dialTestClient(t, socketPath)
	tc.attach(t, "r1", idA)
	tc.attach(t, "r2", idB)

	tc.send(t, protocol.Envelope{Type: protocol.ReqDetach, ID: "r3", Data: protocol.Marshal(protocol.SessionIDReq{SessionID: idA})})
	tc.recv(t, protocol.RespOK, 2*time.Second)

	if got := subCount(registry, idA); got != 0 {
		t.Fatalf("expected A detached, got %d subscribers", got)
	}
	if got := subCount(registry, idB); got != 1 {
		t.Fatalf("expected B still attached, got %d subscribers", got)
	}
}

func TestDisconnectDetachesEverySession(t *testing.T) {
	socketPath, registry := startTestServer(t)
	ids := createCatSessions(t, registry, "a", "b")

	tc := dialTestClient(t, socketPath)
	tc.attach(t, "r1", ids[0])
	tc.attach(t, "r2", ids[1])
	tc.nc.Close()

	waitUntil(t, 2*time.Second, func() bool { return subCount(registry, ids[0]) == 0 && subCount(registry, ids[1]) == 0 })
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
