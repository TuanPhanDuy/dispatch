// Package inprocess implements client.SessionManager by owning
// ptyproc.Process instances directly in the TUI's own OS process. It proves
// out the SessionManager seam before Phase 3 swaps in a socket-backed
// implementation talking to a daemon — see the project plan for why that
// swap is expected to be low-risk.
package inprocess

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TuanPhanDuy/dispatch/internal/accounts"
	"github.com/TuanPhanDuy/dispatch/internal/client"
	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/idle"
	"github.com/TuanPhanDuy/dispatch/internal/ptyproc"
)

const idleSweepInterval = 2 * time.Second

type session struct {
	mu sync.Mutex

	id      string
	name    string
	profile config.AgentProfile
	cwd     string
	env     []string // nil inherits this process's environment

	proc      *ptyproc.Process
	ring      *ptyproc.RingBuffer
	idleTrack *idle.Tracker

	status    client.SessionStatus
	startedAt time.Time
	exitCode  *int

	subs      map[int]chan client.OutputChunk
	nextSubID int
}

// Manager is an in-process client.SessionManager.
type Manager struct {
	mu             sync.Mutex
	sessions       map[string]*session
	order          []string // creation order, for stable List() output
	listSubs       []chan []client.SessionSummary
	scrollbackCap  int
	defaultIdleSec int

	stopSweep func()
}

// New creates an in-process Manager. scrollbackCap bounds each session's
// ring buffer (lines); defaultIdleSec is the fallback "needs attention"
// threshold for profiles that don't override it.
func New(scrollbackCap, defaultIdleSec int) *Manager {
	m := &Manager{
		sessions:       make(map[string]*session),
		scrollbackCap:  scrollbackCap,
		defaultIdleSec: defaultIdleSec,
	}
	m.stopSweep = m.startIdleSweep()
	return m
}

// Close stops the idle-sweep ticker and kills all sessions. Unlike
// SocketManager.Close (which only disconnects — the daemon keeps sessions
// alive), there is no separate daemon here to keep sessions running once
// this Manager stops owning them, so Close kills every session. See the
// SessionManager interface doc for why this asymmetry is intentional.
func (m *Manager) Close() error {
	m.stopSweep()
	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		if s.proc != nil {
			_ = s.proc.Kill()
		}
		s.mu.Unlock()
	}
	return nil
}

func (m *Manager) idleThreshold(profile config.AgentProfile) time.Duration {
	sec := profile.IdleAttentionSeconds
	if sec <= 0 {
		sec = m.defaultIdleSec
	}
	if sec <= 0 {
		sec = 10
	}
	return time.Duration(sec) * time.Second
}

// uniqueNameLocked returns name, or name suffixed with " (2)", " (3)", ...
// until it no longer collides with any existing session's name. Must be
// called with m.mu already held, and the result inserted into m.sessions
// before releasing it — otherwise two concurrent Create calls could both
// resolve the same "unique" name (check-then-act race).
func (m *Manager) uniqueNameLocked(name string) string {
	inUse := func(candidate string) bool {
		for _, s := range m.sessions {
			s.mu.Lock()
			match := s.name == candidate
			s.mu.Unlock()
			if match {
				return true
			}
		}
		return false
	}
	candidate := name
	for i := 2; inUse(candidate); i++ {
		candidate = fmt.Sprintf("%s (%d)", name, i)
	}
	return candidate
}

func (m *Manager) Create(profile config.AgentProfile, name, cwd string, cols, rows int) (string, error) {
	env, err := accounts.SessionEnv(profile.Account)
	if err != nil {
		return "", err
	}
	proc, err := ptyproc.Start(profile.Command, profile.Args, cwd, env, cols, rows)
	if err != nil {
		return "", fmt.Errorf("spawn %s: %w", profile.Command, err)
	}

	id := uuid.NewString()
	s := &session{
		id:        id,
		profile:   profile,
		cwd:       cwd,
		env:       env,
		proc:      proc,
		ring:      ptyproc.NewRingBuffer(m.scrollbackCap),
		idleTrack: idle.NewTracker(m.idleThreshold(profile)),
		status:    client.StatusRunning,
		startedAt: time.Now(),
		subs:      make(map[int]chan client.OutputChunk),
	}

	m.mu.Lock()
	s.name = m.uniqueNameLocked(name)
	m.sessions[id] = s
	m.order = append(m.order, id)
	m.mu.Unlock()

	go m.pump(s, proc)
	m.notifyList()
	return id, nil
}

// pump owns reading one specific *ptyproc.Process (passed explicitly, not
// read from s.proc) for that process's entire lifetime, independent of
// whether anyone's attached — this is what keeps scrollback/idle state
// current in the background.
//
// Taking proc as a parameter instead of dereferencing the mutable s.proc
// field matters: Restart() can reassign s.proc and start a *new* pump
// goroutine while this one's blocking Read on the *old* proc is still
// unwinding. Reading s.proc directly in that window would race Restart's
// locked write and could even make this stale goroutine start reading the
// new process's PTY concurrently with its own fresh pump goroutine. The
// `s.proc == proc` checks below make sure a stale pump can't clobber state
// that already belongs to a newer generation.
func (m *Manager) pump(s *session, proc *ptyproc.Process) {
	buf := make([]byte, 8192)
	for {
		n, err := proc.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			if s.proc == proc {
				s.ring.Write(chunk)
				s.idleTrack.OnOutput()
				for _, ch := range s.subs {
					select {
					case ch <- client.OutputChunk{Bytes: chunk}:
					default:
					}
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			// Reap the child now that its PTY has gone away — without
			// this, Wait() is never called anywhere and every killed,
			// restarted, or naturally-exited session leaves a zombie
			// process behind.
			_ = proc.Wait()

			s.mu.Lock()
			if s.proc == proc {
				s.status = client.StatusExited
				for subID, ch := range s.subs {
					select {
					case ch <- client.OutputChunk{Exited: true}:
					default:
					}
					close(ch)
					delete(s.subs, subID)
				}
			}
			s.mu.Unlock()
			m.notifyList()
			return
		}
	}
}

func (m *Manager) startIdleSweep() func() {
	ticker := time.NewTicker(idleSweepInterval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if m.sweepIdle() {
					m.notifyList()
				}
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}

func (m *Manager) sweepIdle() bool {
	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()

	changed := false
	for _, s := range sessions {
		s.mu.Lock()
		if s.status == client.StatusRunning && s.idleTrack.NeedsAttention() {
			s.status = client.StatusNeedsAttention
			changed = true
		} else if s.status == client.StatusNeedsAttention && !s.idleTrack.NeedsAttention() {
			s.status = client.StatusRunning
			changed = true
		}
		s.mu.Unlock()
	}
	return changed
}

func (m *Manager) List() ([]client.SessionSummary, error) {
	m.mu.Lock()
	order := append([]string(nil), m.order...)
	sessions := make(map[string]*session, len(m.sessions))
	for k, v := range m.sessions {
		sessions[k] = v
	}
	m.mu.Unlock()

	out := make([]client.SessionSummary, 0, len(order))
	for _, id := range order {
		s, ok := sessions[id]
		if !ok {
			continue
		}
		s.mu.Lock()
		out = append(out, client.SessionSummary{
			ID:        s.id,
			Name:      s.name,
			Command:   s.profile.Command,
			Cwd:       s.cwd,
			Status:    s.status,
			StartedAt: s.startedAt,
			ExitCode:  s.exitCode,
			Account:   s.profile.Account,
		})
		s.mu.Unlock()
	}
	return out, nil
}

func (m *Manager) get(id string) (*session, error) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return s, nil
}

func (m *Manager) Attach(id string, cols, rows int) ([]byte, <-chan client.OutputChunk, error) {
	s, err := m.get(id)
	if err != nil {
		return nil, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan client.OutputChunk, 64)
	if s.status == client.StatusExited {
		// The pump goroutine already returned and will never signal this
		// channel, so tell the caller immediately instead of leaving it
		// to block forever waiting for output that will never arrive.
		ch <- client.OutputChunk{Exited: true}
		close(ch)
		return []byte(s.ring.Snapshot()), ch, nil
	}

	if s.proc != nil {
		_ = s.proc.Resize(cols, rows)
	}
	s.nextSubID++
	s.subs[s.nextSubID] = ch
	return []byte(s.ring.Snapshot()), ch, nil
}

func (m *Manager) Detach(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for subID, ch := range s.subs {
		close(ch)
		delete(s.subs, subID)
	}
	s.mu.Unlock()
	return nil
}

// Write, Resize, and Kill hold s.mu for the whole call into proc, not just
// while copying the pointer out — copying `proc := s.proc` and calling into
// it after unlocking let a concurrent Restart swap/close s.proc in between,
// silently targeting an already-closed process. PTY writes/resizes/signals
// are fast, non-blocking syscalls, so holding the lock for the duration
// costs nothing measurable.
func (m *Manager) Write(id string, data []byte) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return fmt.Errorf("session %s is not running", id)
	}
	_, err = s.proc.Write(data)
	return err
}

func (m *Manager) Resize(id string, cols, rows int) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return nil
	}
	return s.proc.Resize(cols, rows)
}

// killGracePeriod is how long Kill waits for SIGTERM to take effect before
// escalating to SIGKILL — see Registry.Kill's doc comment in the daemon
// package for why this is needed (some interactive shells ignore SIGTERM).
const killGracePeriod = 3 * time.Second

func (m *Manager) Kill(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	proc := s.proc
	if proc == nil {
		s.mu.Unlock()
		return nil
	}
	err = proc.Kill()
	s.mu.Unlock()
	if err != nil {
		return err
	}

	go func() {
		time.Sleep(killGracePeriod)
		s.mu.Lock()
		stillRunning := s.proc == proc && s.status != client.StatusExited
		s.mu.Unlock()
		if stillRunning {
			_ = proc.KillForce()
		}
	}()
	return nil
}

// Remove permanently discards an exited session's entry, freeing its ring
// buffer/idle tracker and freeing its name for reuse. It refuses to remove
// a session that's still running — callers that want that must Kill first.
// Without this, every session that ever exits lingers in m.sessions/m.order
// for the Manager's entire lifetime.
func (m *Manager) Remove(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	exited := s.status == client.StatusExited
	s.mu.Unlock()
	if !exited {
		return fmt.Errorf("session %s is still running", id)
	}

	m.mu.Lock()
	delete(m.sessions, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	m.notifyList()
	return nil
}

func (m *Manager) Restart(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.proc != nil {
		_ = s.proc.Kill()
		_ = s.proc.Close()
	}
	proc, startErr := ptyproc.Start(s.profile.Command, s.profile.Args, s.cwd, s.env, 80, 24)
	if startErr != nil {
		s.mu.Unlock()
		return fmt.Errorf("restart %s: %w", s.profile.Command, startErr)
	}
	s.proc = proc
	s.ring = ptyproc.NewRingBuffer(m.scrollbackCap)
	s.idleTrack = idle.NewTracker(m.idleThreshold(s.profile))
	s.status = client.StatusRunning
	s.startedAt = time.Now()
	s.exitCode = nil
	s.mu.Unlock()

	go m.pump(s, proc)
	m.notifyList()
	return nil
}

func (m *Manager) Rename(id, name string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
	m.notifyList()
	return nil
}

// Subscribe registers ch before fetching the current snapshot so no update
// is missed in between — but that means a concurrent notifyList() can land
// in ch's buffer first. The initial send below must stay non-blocking: a
// blocking send here would deadlock if that push already filled the
// capacity-1 buffer (there's no reader yet — Subscribe hasn't returned ch).
func (m *Manager) Subscribe() <-chan []client.SessionSummary {
	ch := make(chan []client.SessionSummary, 1)
	m.mu.Lock()
	m.listSubs = append(m.listSubs, ch)
	m.mu.Unlock()

	if snapshot, err := m.List(); err == nil {
		select {
		case ch <- snapshot:
		default:
		}
	}
	return ch
}

func (m *Manager) notifyList() {
	snapshot, err := m.List()
	if err != nil {
		return
	}
	m.mu.Lock()
	subs := append([]chan []client.SessionSummary(nil), m.listSubs...)
	m.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- snapshot:
		default:
			// Non-blocking: a slow subscriber gets the next snapshot
			// instead of stalling session pumps.
		}
	}
}

var _ client.SessionManager = (*Manager)(nil)
