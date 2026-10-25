package daemon

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/protocol"
)

// Server speaks the protocol wire format over accepted connections,
// translating each request into a Registry call and fanning Registry
// pushes (output, session-list changes) back out over the connection.
type Server struct {
	registry *Registry
	logger   *slog.Logger
}

func NewServer(registry *Registry, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{registry: registry, logger: logger}
}

// Serve accepts connections until l errors (e.g. because it was closed).
func (srv *Server) Serve(l net.Listener) error {
	for {
		nc, err := l.Accept()
		if err != nil {
			return err
		}
		go srv.handleConn(nc)
	}
}

type conn struct {
	srv *Server
	nc  net.Conn
	out chan protocol.Envelope

	// mu guards every field below, including outClosed. It is never held
	// while calling into the Registry — Registry methods take their own
	// session/registry locks, and the Registry's pump goroutine calls
	// back into c.send while holding a session lock, so holding c.mu
	// across a Registry call here would risk an AB-BA deadlock with that
	// goroutine. Capture-then-release-then-call instead.
	mu        sync.Mutex
	outClosed bool
	listSubID int
	attached  map[string]int // sessionID -> output subscription ID
}

func (srv *Server) handleConn(nc net.Conn) {
	c := &conn{srv: srv, nc: nc, out: make(chan protocol.Envelope, 256), attached: make(map[string]int)}
	defer nc.Close()

	go c.writeLoop()

	// Every connection gets live session-list pushes for free — the TUI
	// always wants this, so there's no separate subscribe request type.
	c.listSubID = srv.registry.SubscribeList(func(sessions []protocol.SessionSummary) {
		c.send(protocol.Envelope{
			Type: protocol.EventSessionList,
			Data: protocol.Marshal(protocol.SessionListEvent{Sessions: sessions}),
		})
	})
	defer func() {
		srv.registry.UnsubscribeList(c.listSubID)
		c.cleanup()
	}()

	r := bufio.NewReader(nc)
	for {
		env, err := protocol.ReadEnvelope(r)
		if err != nil {
			if err != io.EOF {
				srv.logger.Debug("connection read error", "err", err)
			}
			c.closeOutput()
			return
		}
		c.handle(env)
	}
}

func (c *conn) writeLoop() {
	w := bufio.NewWriter(c.nc)
	for env := range c.out {
		if err := protocol.WriteEnvelope(w, env); err != nil {
			return
		}
	}
}

// send enqueues env for this connection's writer, dropping it if the queue
// is full or already closed rather than blocking or panicking — a
// slow/stuck/disconnected client must never be able to stall the
// Registry's pump goroutine for other sessions, and a send racing a close
// must never panic the process.
func (c *conn) send(env protocol.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outClosed {
		return
	}
	select {
	case c.out <- env:
	default:
	}
}

// closeOutput closes c.out exactly once, holding the same lock send()
// checks — so a send can never land on an already-closed channel.
func (c *conn) closeOutput() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outClosed {
		return
	}
	c.outClosed = true
	close(c.out)
}

func (c *conn) sendResp(id string, typ protocol.MsgType, data any) {
	c.send(protocol.Envelope{Type: typ, ID: id, Data: protocol.Marshal(data)})
}

func (c *conn) sendErr(id string, err error) {
	c.sendResp(id, protocol.RespError, protocol.ErrorResp{Message: err.Error()})
}

func (c *conn) handle(env protocol.Envelope) {
	switch env.Type {
	case protocol.ReqList:
		c.sendResp(env.ID, protocol.RespList, protocol.ListResp{Sessions: c.srv.registry.List()})

	case protocol.ReqCreate:
		var req protocol.CreateSessionReq
		if err := protocol.Unmarshal(env.Data, &req); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		profile := config.AgentProfile{Name: req.Name, Command: req.Command, Args: req.Args}
		id, err := c.srv.registry.Create(profile, req.Name, req.Cwd, req.Cols, req.Rows)
		if err != nil {
			c.sendErr(env.ID, err)
			return
		}
		c.sendResp(env.ID, protocol.RespCreate, protocol.CreateResp{SessionID: id})

	case protocol.ReqAttach:
		c.handleAttach(env)

	case protocol.ReqDetach:
		var req protocol.SessionIDReq
		if err := protocol.Unmarshal(env.Data, &req); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		c.detach(req.SessionID)
		c.sendResp(env.ID, protocol.RespOK, nil)

	case protocol.ReqInput:
		var req protocol.InputReq
		if err := protocol.Unmarshal(env.Data, &req); err == nil {
			_ = c.srv.registry.Write(req.SessionID, req.Bytes)
		}

	case protocol.ReqResize:
		var req protocol.ResizeReq
		if err := protocol.Unmarshal(env.Data, &req); err == nil {
			_ = c.srv.registry.Resize(req.SessionID, req.Cols, req.Rows)
		}

	case protocol.ReqKill:
		var req protocol.SessionIDReq
		if err := protocol.Unmarshal(env.Data, &req); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		if err := c.srv.registry.Kill(req.SessionID); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		c.sendResp(env.ID, protocol.RespOK, nil)

	case protocol.ReqRestart:
		var req protocol.SessionIDReq
		if err := protocol.Unmarshal(env.Data, &req); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		if err := c.srv.registry.Restart(req.SessionID); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		c.sendResp(env.ID, protocol.RespOK, nil)

	case protocol.ReqRename:
		var req protocol.RenameReq
		if err := protocol.Unmarshal(env.Data, &req); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		if err := c.srv.registry.Rename(req.SessionID, req.Name); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		c.sendResp(env.ID, protocol.RespOK, nil)

	case protocol.ReqRemove:
		var req protocol.SessionIDReq
		if err := protocol.Unmarshal(env.Data, &req); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		if err := c.srv.registry.Remove(req.SessionID); err != nil {
			c.sendErr(env.ID, err)
			return
		}
		c.sendResp(env.ID, protocol.RespOK, nil)
	}
}

func (c *conn) handleAttach(env protocol.Envelope) {
	var req protocol.AttachReq
	if err := protocol.Unmarshal(env.Data, &req); err != nil {
		c.sendErr(env.ID, err)
		return
	}

	// A connection may be attached to many sessions at once (the GUI
	// tiles several in one window over a single socket), but to each
	// session only once: re-attaching replaces the old subscription so it
	// can't leak or deliver every chunk twice.
	c.detach(req.SessionID)

	scrollback, subID, err := c.srv.registry.Attach(req.SessionID, req.Cols, req.Rows, func(ev protocol.OutputEvent) {
		c.send(protocol.Envelope{Type: protocol.EventOutput, Data: protocol.Marshal(ev)})
	})
	if err != nil {
		c.sendErr(env.ID, err)
		return
	}

	c.mu.Lock()
	c.attached[req.SessionID] = subID
	c.mu.Unlock()
	c.sendResp(env.ID, protocol.RespAttach, protocol.AttachResp{Scrollback: scrollback})
}

// detach drops this connection's attachment to sessionID, if it has one;
// a stale request naming a session it isn't attached to is a no-op.
func (c *conn) detach(sessionID string) {
	c.mu.Lock()
	subID, ok := c.attached[sessionID]
	delete(c.attached, sessionID)
	c.mu.Unlock()

	if ok {
		_ = c.srv.registry.Detach(sessionID, subID)
	}
}

// cleanup detaches (but does not kill) every session this connection was
// attached to — the sessions keep running; only the output streams to this
// now-dead connection stop.
func (c *conn) cleanup() {
	c.mu.Lock()
	attached := c.attached
	c.attached = make(map[string]int)
	c.mu.Unlock()

	for id, subID := range attached {
		_ = c.srv.registry.Detach(id, subID)
	}
}
