// Package protocol defines the wire format exchanged between the dispatch
// daemon and its clients: one JSON object per line (newline-framed), chosen
// over gob/protobuf for debuggability (`nc -U` the socket and read it) and
// because there's no codegen tax or cross-language pressure in v1. This
// package has no internal dependencies — it's pure wire format — so it can
// be imported by both internal/daemon and internal/client without a cycle.
package protocol

import (
	"encoding/json"
	"time"
)

// MsgType identifies the payload carried in an Envelope's Data field.
type MsgType string

const (
	ReqList    MsgType = "req.list"
	ReqCreate  MsgType = "req.create"
	ReqAttach  MsgType = "req.attach"
	ReqDetach  MsgType = "req.detach"
	ReqInput   MsgType = "req.input"
	ReqResize  MsgType = "req.resize"
	ReqKill    MsgType = "req.kill"
	ReqRestart MsgType = "req.restart"
	ReqRename  MsgType = "req.rename"
	ReqRemove  MsgType = "req.remove"
	// ReqAccounts lists the accounts that exist (see internal/accounts).
	// Daemons older than this request answer it with RespError.
	ReqAccounts MsgType = "req.accounts"

	RespOK       MsgType = "resp.ok"
	RespError    MsgType = "resp.error"
	RespList     MsgType = "resp.list"
	RespCreate   MsgType = "resp.create"
	RespAttach   MsgType = "resp.attach"
	RespAccounts MsgType = "resp.accounts"

	EventOutput      MsgType = "event.output"
	EventSessionList MsgType = "event.session_list"
)

// Envelope frames every message: ID correlates a request with its
// response (request/response channel); events carry no ID (server-push
// channel). Both channels share the one socket connection.
type Envelope struct {
	Type MsgType         `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// SessionStatus mirrors client.SessionStatus on the wire.
type SessionStatus string

const (
	StatusRunning        SessionStatus = "running"
	StatusNeedsAttention SessionStatus = "needs-attention"
	StatusExited         SessionStatus = "exited"
)

// SessionSummary is the wire form of a session-list row.
type SessionSummary struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Command   string        `json:"command"`
	Cwd       string        `json:"cwd"`
	Status    SessionStatus `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	// Account is the account the session runs under; empty means the
	// agent's own default configuration (e.g. ~/.claude).
	Account string `json:"account,omitempty"`
}

type CreateSessionReq struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd"`
	Cols    int      `json:"cols"`
	Rows    int      `json:"rows"`
	// Account, if set, runs the session under that account's own
	// configuration home (created on first use).
	Account string `json:"account,omitempty"`
}

// SessionIDReq is shared by Detach/Kill/Restart — requests that only need
// to name a session.
type SessionIDReq struct {
	SessionID string `json:"session_id"`
}

type AttachReq struct {
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

type ResizeReq struct {
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

type InputReq struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"`
}

type RenameReq struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

type ErrorResp struct {
	Message string `json:"message"`
}

type CreateResp struct {
	SessionID string `json:"session_id"`
}

type AttachResp struct {
	Scrollback []byte `json:"scrollback"`
}

type AccountsResp struct {
	Accounts []string `json:"accounts"`
}

type ListResp struct {
	Sessions []SessionSummary `json:"sessions"`
}

// OutputEvent is a server push: either a chunk of raw PTY bytes, or (when
// Exited is set) the final signal that the session's process has ended.
type OutputEvent struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes,omitempty"`
	Exited    bool   `json:"exited,omitempty"`
}

type SessionListEvent struct {
	Sessions []SessionSummary `json:"sessions"`
}
