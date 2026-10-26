package client

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/daemon"
	"github.com/TuanPhanDuy/dispatch/internal/protocol"
)

const callTimeout = 10 * time.Second

// outputSub pairs a per-session output channel with a closed flag, both
// guarded by SocketManager.outputMu. Tracking "closed" explicitly (instead
// of just deleting the map entry) lets deliverOutput and Detach share one
// lock across their whole send-or-close decision, so a send can never land
// on a channel the other side just closed — closing a channel that still
// has an in-flight send racing it is what used to panic here.
type outputSub struct {
	ch     chan OutputChunk
	closed bool
}

// SocketManager is the socket-backed SessionManager: a thin client talking
// to the daemon over a unix socket. It's what makes sessions survive
// closing the TUI — see the project plan for why this architecture was
// chosen for v1 instead of an in-process-only implementation.
type SocketManager struct {
	conn net.Conn

	writeMu sync.Mutex
	w       *bufio.Writer

	mu      sync.Mutex
	pending map[string]chan protocol.Envelope

	listMu   sync.Mutex
	listSubs []chan []SessionSummary

	outputMu   sync.Mutex
	outputSubs map[string]*outputSub // sessionID -> this client's attach channel
}

// Dial ensures a daemon is running on socketPath (spawning one if needed)
// and connects to it.
func Dial(socketPath string, scrollbackCap, defaultIdleSec int) (*SocketManager, error) {
	if err := daemon.EnsureRunning(socketPath, scrollbackCap, defaultIdleSec); err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}

	m := &SocketManager{
		conn:       conn,
		w:          bufio.NewWriter(conn),
		pending:    make(map[string]chan protocol.Envelope),
		outputSubs: make(map[string]*outputSub),
	}
	go m.readLoop()
	return m, nil
}

// Close disconnects from the daemon. It does NOT kill any sessions — they
// keep running in the daemon, by design (that's the whole point of the
// daemon/client split). See the SessionManager interface doc for how this
// differs from inprocess.Manager.Close.
func (m *SocketManager) Close() error {
	return m.conn.Close()
}

func (m *SocketManager) readLoop() {
	r := bufio.NewReader(m.conn)
	for {
		env, err := protocol.ReadEnvelope(r)
		if err != nil {
			m.handleDisconnect()
			return
		}

		switch env.Type {
		case protocol.EventSessionList:
			var ev protocol.SessionListEvent
			_ = protocol.Unmarshal(env.Data, &ev)
			m.broadcastList(toClientSummaries(ev.Sessions))

		case protocol.EventOutput:
			var ev protocol.OutputEvent
			_ = protocol.Unmarshal(env.Data, &ev)
			m.deliverOutput(ev)

		default:
			m.mu.Lock()
			ch, ok := m.pending[env.ID]
			if ok {
				delete(m.pending, env.ID)
			}
			m.mu.Unlock()
			if ok {
				ch <- env
			}
		}
	}
}

// handleDisconnect runs once, when the connection to the daemon is lost.
// It closes every channel this SocketManager has ever handed out —
// pending call() responses, attached-session output streams, and
// session-list subscriptions — so no caller blocked on one of them (e.g.
// AttachModel.waitForSessionOutput, AppModel.waitForSessionList) waits
// forever with no indication anything went wrong.
func (m *SocketManager) handleDisconnect() {
	m.mu.Lock()
	for _, ch := range m.pending {
		close(ch)
	}
	m.pending = map[string]chan protocol.Envelope{}
	m.mu.Unlock()

	m.outputMu.Lock()
	for id, sub := range m.outputSubs {
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		delete(m.outputSubs, id)
	}
	m.outputMu.Unlock()

	m.listMu.Lock()
	for _, ch := range m.listSubs {
		close(ch)
	}
	m.listSubs = nil
	m.listMu.Unlock()
}

func toClientSummaries(in []protocol.SessionSummary) []SessionSummary {
	out := make([]SessionSummary, len(in))
	for i, s := range in {
		out[i] = SessionSummary{
			ID:        s.ID,
			Name:      s.Name,
			Command:   s.Command,
			Cwd:       s.Cwd,
			Status:    SessionStatus(s.Status),
			StartedAt: s.StartedAt,
			Account:   s.Account,
		}
	}
	return out
}

// deliverOutput holds outputMu across the whole send-or-close decision so
// it can never race Detach's own close of the same channel.
func (m *SocketManager) deliverOutput(ev protocol.OutputEvent) {
	m.outputMu.Lock()
	defer m.outputMu.Unlock()

	sub, ok := m.outputSubs[ev.SessionID]
	if !ok || sub.closed {
		return
	}

	select {
	case sub.ch <- OutputChunk{Bytes: ev.Bytes, Exited: ev.Exited}:
	default:
	}
	if ev.Exited {
		sub.closed = true
		delete(m.outputSubs, ev.SessionID)
		close(sub.ch)
	}
}

func (m *SocketManager) broadcastList(sessions []SessionSummary) {
	m.listMu.Lock()
	subs := append([]chan []SessionSummary(nil), m.listSubs...)
	m.listMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- sessions:
		default:
		}
	}
}

func (m *SocketManager) writeEnvelope(env protocol.Envelope) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return protocol.WriteEnvelope(m.w, env)
}

// call sends a request and blocks for its correlated response.
func (m *SocketManager) call(typ protocol.MsgType, payload any) (protocol.Envelope, error) {
	id := uuid.NewString()
	ch := make(chan protocol.Envelope, 1)

	m.mu.Lock()
	m.pending[id] = ch
	m.mu.Unlock()

	if err := m.writeEnvelope(protocol.Envelope{Type: typ, ID: id, Data: protocol.Marshal(payload)}); err != nil {
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()
		return protocol.Envelope{}, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return protocol.Envelope{}, fmt.Errorf("daemon connection closed")
		}
		if resp.Type == protocol.RespError {
			var errResp protocol.ErrorResp
			_ = protocol.Unmarshal(resp.Data, &errResp)
			return protocol.Envelope{}, fmt.Errorf("%s", errResp.Message)
		}
		return resp, nil
	case <-time.After(callTimeout):
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()
		return protocol.Envelope{}, fmt.Errorf("timed out waiting for daemon response")
	}
}

func (m *SocketManager) List() ([]SessionSummary, error) {
	resp, err := m.call(protocol.ReqList, nil)
	if err != nil {
		return nil, err
	}
	var out protocol.ListResp
	if err := protocol.Unmarshal(resp.Data, &out); err != nil {
		return nil, err
	}
	return toClientSummaries(out.Sessions), nil
}

func (m *SocketManager) Create(profile config.AgentProfile, name, cwd string, cols, rows int) (string, error) {
	resp, err := m.call(protocol.ReqCreate, protocol.CreateSessionReq{
		Name: name, Command: profile.Command, Args: profile.Args, Cwd: cwd, Cols: cols, Rows: rows,
		Account: profile.Account,
	})
	if err != nil {
		return "", err
	}
	var out protocol.CreateResp
	if err := protocol.Unmarshal(resp.Data, &out); err != nil {
		return "", err
	}
	return out.SessionID, nil
}

func (m *SocketManager) Attach(sessionID string, cols, rows int) ([]byte, <-chan OutputChunk, error) {
	sub := &outputSub{ch: make(chan OutputChunk, 64)}
	m.outputMu.Lock()
	m.outputSubs[sessionID] = sub
	m.outputMu.Unlock()

	resp, err := m.call(protocol.ReqAttach, protocol.AttachReq{SessionID: sessionID, Cols: cols, Rows: rows})
	if err != nil {
		m.outputMu.Lock()
		delete(m.outputSubs, sessionID)
		m.outputMu.Unlock()
		return nil, nil, err
	}
	var out protocol.AttachResp
	if err := protocol.Unmarshal(resp.Data, &out); err != nil {
		return nil, nil, err
	}
	return out.Scrollback, sub.ch, nil
}

func (m *SocketManager) Detach(sessionID string) error {
	m.outputMu.Lock()
	if sub, ok := m.outputSubs[sessionID]; ok && !sub.closed {
		sub.closed = true
		delete(m.outputSubs, sessionID)
		close(sub.ch)
	}
	m.outputMu.Unlock()

	_, err := m.call(protocol.ReqDetach, protocol.SessionIDReq{SessionID: sessionID})
	return err
}

// Write and Resize are fire-and-forget: the daemon doesn't ack every
// keystroke, and waiting for a round-trip per key would add latency to
// every single input event for no benefit.
func (m *SocketManager) Write(sessionID string, data []byte) error {
	return m.writeEnvelope(protocol.Envelope{
		Type: protocol.ReqInput,
		Data: protocol.Marshal(protocol.InputReq{SessionID: sessionID, Bytes: data}),
	})
}

func (m *SocketManager) Resize(sessionID string, cols, rows int) error {
	return m.writeEnvelope(protocol.Envelope{
		Type: protocol.ReqResize,
		Data: protocol.Marshal(protocol.ResizeReq{SessionID: sessionID, Cols: cols, Rows: rows}),
	})
}

func (m *SocketManager) Kill(sessionID string) error {
	_, err := m.call(protocol.ReqKill, protocol.SessionIDReq{SessionID: sessionID})
	return err
}

func (m *SocketManager) Restart(sessionID string) error {
	_, err := m.call(protocol.ReqRestart, protocol.SessionIDReq{SessionID: sessionID})
	return err
}

func (m *SocketManager) Remove(sessionID string) error {
	_, err := m.call(protocol.ReqRemove, protocol.SessionIDReq{SessionID: sessionID})
	return err
}

func (m *SocketManager) Rename(sessionID, name string) error {
	_, err := m.call(protocol.ReqRename, protocol.RenameReq{SessionID: sessionID, Name: name})
	return err
}

// Subscribe registers ch for future list pushes before fetching the
// current snapshot, so no update is missed in between — but that means a
// concurrent push can land in ch's buffer first. The initial send below
// must stay non-blocking: ch has a capacity-1 buffer, and a blocking send
// here would deadlock forever if that push already filled it (there's no
// reader yet — Subscribe hasn't returned ch to the caller).
func (m *SocketManager) Subscribe() <-chan []SessionSummary {
	ch := make(chan []SessionSummary, 1)
	m.listMu.Lock()
	m.listSubs = append(m.listSubs, ch)
	m.listMu.Unlock()

	if snap, err := m.List(); err == nil {
		select {
		case ch <- snap:
		default:
		}
	}
	return ch
}

var _ SessionManager = (*SocketManager)(nil)
