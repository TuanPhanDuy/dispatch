// Package daemon implements the dumb-on-purpose background process that
// owns PTYs across TUI client connects/disconnects: it spawns processes,
// keeps per-session scrollback + idle state, and serves the protocol
// socket. This is what makes sessions survive closing the TUI.
package daemon

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/idle"
	"github.com/TuanPhanDuy/dispatch/internal/protocol"
	"github.com/TuanPhanDuy/dispatch/internal/ptyproc"
)

const idleSweepInterval = 2 * time.Second

// outputSub's send callback is supplied by server.go and must itself be
// non-blocking (a select-with-default enqueue onto that connection's
// outbound queue) — Registry calls it synchronously from the pump
// goroutine, so a slow/stuck client must never be able to stall PTY reads
// for other sessions.
type outputSub struct {
	send func(protocol.OutputEvent)
}

type session struct {
	mu sync.Mutex

	id      string
	name    string
	cwd     string
	profile config.AgentProfile

	proc      *ptyproc.Process
	ring      *ptyproc.RingBuffer
	idleTrack *idle.Tracker

	status    protocol.SessionStatus
	startedAt time.Time

	subs      map[int]outputSub
	nextSubID int
}

func (s *session) summary() protocol.SessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return protocol.SessionSummary{
		ID:        s.id,
		Name:      s.name,
		Command:   s.profile.Command,
		Cwd:       s.cwd,
		Status:    s.status,
		StartedAt: s.startedAt,
	}
}

// Registry owns every session's PTY, scrollback, and idle state — the
// daemon's one piece of business logic. Output and session-list changes
// are pushed to callers via caller-supplied callbacks rather than Go
// channels, since those callbacks are what let server.go serialize writes
// onto each socket connection.
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*session
	order    []string // creation order, for stable List() output

	listSubs      map[int]func([]protocol.SessionSummary)
	nextListSubID int

	scrollbackCap  int
	defaultIdleSec int

	stopSweep func()
}

// NewRegistry creates a Registry. scrollbackCap bounds each session's ring
// buffer (lines); defaultIdleSec is the fallback "needs attention"
// threshold for profiles that don't override it.
func NewRegistry(scrollbackCap, defaultIdleSec int) *Registry {
	r := &Registry{
		sessions:       make(map[string]*session),
		listSubs:       make(map[int]func([]protocol.SessionSummary)),
		scrollbackCap:  scrollbackCap,
		defaultIdleSec: defaultIdleSec,
	}
	r.stopSweep = r.startIdleSweep()
	return r
}

// Close stops the idle-sweep ticker and kills every session. Call this only
// on daemon shutdown — never on a client disconnecting, which is the whole
// point of the daemon existing.
func (r *Registry) Close() {
	r.stopSweep()
	r.mu.Lock()
	sessions := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		if s.proc != nil {
			_ = s.proc.Kill()
		}
		s.mu.Unlock()
	}
}

func (r *Registry) idleThreshold(profile config.AgentProfile) time.Duration {
	sec := profile.IdleAttentionSeconds
	if sec <= 0 {
		sec = r.defaultIdleSec
	}
	if sec <= 0 {
		sec = 10
	}
	return time.Duration(sec) * time.Second
}

// uniqueNameLocked returns name, or name suffixed with " (2)", " (3)", ...
// until it no longer collides with any existing session's name. Must be
// called with r.mu already held, and the result inserted into r.sessions
// before releasing it — otherwise two concurrent Create calls could both
// resolve the same "unique" name (check-then-act race).
func (r *Registry) uniqueNameLocked(name string) string {
	inUse := func(candidate string) bool {
		for _, s := range r.sessions {
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

func (r *Registry) Create(profile config.AgentProfile, name, cwd string, cols, rows int) (string, error) {
	proc, err := ptyproc.Start(profile.Command, profile.Args, cwd, nil, cols, rows)
	if err != nil {
		return "", fmt.Errorf("spawn %s: %w", profile.Command, err)
	}

	id := uuid.NewString()
	s := &session{
		id:        id,
		cwd:       cwd,
		profile:   profile,
		proc:      proc,
		ring:      ptyproc.NewRingBuffer(r.scrollbackCap),
		idleTrack: idle.NewTracker(r.idleThreshold(profile)),
		status:    protocol.StatusRunning,
		startedAt: time.Now(),
		subs:      make(map[int]outputSub),
	}

	r.mu.Lock()
	s.name = r.uniqueNameLocked(name)
	r.sessions[id] = s
	r.order = append(r.order, id)
	r.mu.Unlock()

	go r.pump(s, proc)
	r.notifyList()
	return id, nil
}

// pump owns reading one specific *ptyproc.Process (passed explicitly, not
// read from s.proc) for that process's entire lifetime, independent of
// whether any client is attached — this is what keeps scrollback/idle
// state current across attach/detach/reconnect cycles.
//
// Taking proc as a parameter instead of dereferencing the mutable s.proc
// field matters: Restart() can reassign s.proc and start a *new* pump
// goroutine while this one's blocking Read on the *old* proc is still
// unwinding. Reading s.proc directly in that window would race Restart's
// locked write and could even make this stale goroutine start reading the
// new process's PTY concurrently with its own fresh pump goroutine. By
// closing over a specific proc, this goroutine only ever touches the
// process it was spawned for, and the `s.proc == proc` checks below make
// sure a stale pump can't clobber state that already belongs to a newer
// generation.
func (r *Registry) pump(s *session, proc *ptyproc.Process) {
	buf := make([]byte, 8192)
	for {
		n, err := proc.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			if s.proc == proc {
				s.ring.Write(chunk)
				s.idleTrack.OnOutput()
				for _, sub := range s.subs {
					sub.send(protocol.OutputEvent{SessionID: s.id, Bytes: chunk})
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			// Reap the child now that its PTY has gone away — without
			// this, Wait() is never called anywhere and every killed,
			// restarted, or naturally-exited session leaves a zombie
			// process for the life of the (long-running) daemon.
			_ = proc.Wait()

			s.mu.Lock()
			if s.proc == proc {
				s.status = protocol.StatusExited
				for subID, sub := range s.subs {
					sub.send(protocol.OutputEvent{SessionID: s.id, Exited: true})
					delete(s.subs, subID)
				}
			}
			s.mu.Unlock()
			r.notifyList()
			return
		}
	}
}

func (r *Registry) startIdleSweep() func() {
	ticker := time.NewTicker(idleSweepInterval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if r.sweepIdle() {
					r.notifyList()
				}
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}

func (r *Registry) sweepIdle() bool {
	r.mu.Lock()
	sessions := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()

	changed := false
	for _, s := range sessions {
		s.mu.Lock()
		if s.status == protocol.StatusRunning && s.idleTrack.NeedsAttention() {
			s.status = protocol.StatusNeedsAttention
			changed = true
		} else if s.status == protocol.StatusNeedsAttention && !s.idleTrack.NeedsAttention() {
			s.status = protocol.StatusRunning
			changed = true
		}
		s.mu.Unlock()
	}
	return changed
}

func (r *Registry) List() []protocol.SessionSummary {
	r.mu.Lock()
	order := append([]string(nil), r.order...)
	sessions := make(map[string]*session, len(r.sessions))
	for k, v := range r.sessions {
		sessions[k] = v
	}
	r.mu.Unlock()

	out := make([]protocol.SessionSummary, 0, len(order))
	for _, id := range order {
		if s, ok := sessions[id]; ok {
			out = append(out, s.summary())
		}
	}
	return out
}

func (r *Registry) get(id string) (*session, error) {
	r.mu.Lock()
	s, ok := r.sessions[id]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return s, nil
}

// Attach registers onEvent to receive this session's future output and
// returns its current scrollback plus a subscription ID for Detach. If the
// session has already exited, onEvent is invoked immediately with an
// Exited marker instead of being registered — the pump goroutine that
// would otherwise deliver it is already gone.
func (r *Registry) Attach(id string, cols, rows int, onEvent func(protocol.OutputEvent)) ([]byte, int, error) {
	s, err := r.get(id)
	if err != nil {
		return nil, 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == protocol.StatusExited {
		onEvent(protocol.OutputEvent{SessionID: id, Exited: true})
		return []byte(s.ring.Snapshot()), 0, nil
	}

	if s.proc != nil {
		_ = s.proc.Resize(cols, rows)
	}
	s.nextSubID++
	subID := s.nextSubID
	s.subs[subID] = outputSub{send: onEvent}
	return []byte(s.ring.Snapshot()), subID, nil
}

func (r *Registry) Detach(id string, subID int) error {
	s, err := r.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.subs, subID)
	s.mu.Unlock()
	return nil
}

// Write, Resize, and Kill hold s.mu for the whole call into proc, not just
// while copying the pointer out — copying `proc := s.proc` and calling into
// it after unlocking let a concurrent Restart swap/close s.proc in between,
// silently targeting an already-closed process. PTY writes/resizes/signals
// are fast, non-blocking syscalls, so holding the lock for the duration
// costs nothing measurable.
func (r *Registry) Write(id string, data []byte) error {
	s, err := r.get(id)
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

func (r *Registry) Resize(id string, cols, rows int) error {
	s, err := r.get(id)
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
// escalating to SIGKILL. Some interactive shells don't exit on SIGTERM
// alone (observed with zsh sessions carrying conda's shell integration),
// which would otherwise leave a session showing "running" forever with no
// way for the user to actually stop it.
const killGracePeriod = 3 * time.Second

func (r *Registry) Kill(id string) error {
	s, err := r.get(id)
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

	// The s.proc == proc check mirrors pump()'s guard: if Restart() swapped
	// in a newer process during the grace period, this stale timer must
	// never force-kill it.
	go func() {
		time.Sleep(killGracePeriod)
		s.mu.Lock()
		stillRunning := s.proc == proc && s.status != protocol.StatusExited
		s.mu.Unlock()
		if stillRunning {
			_ = proc.KillForce()
		}
	}()
	return nil
}

// Remove permanently discards an exited session's entry, freeing its ring
// buffer/idle tracker and freeing its name for reuse. It refuses to remove
// a session that's still running — callers that want that must Kill first
// (removing a live session out from under its own pump goroutine would
// leave that goroutine writing into a session struct nothing references
// anymore). Without this, every session that ever exits lingers in
// r.sessions/r.order for the daemon's entire lifetime.
func (r *Registry) Remove(id string) error {
	s, err := r.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	exited := s.status == protocol.StatusExited
	s.mu.Unlock()
	if !exited {
		return fmt.Errorf("session %s is still running", id)
	}

	r.mu.Lock()
	delete(r.sessions, id)
	for i, oid := range r.order {
		if oid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.mu.Unlock()

	r.notifyList()
	return nil
}

func (r *Registry) Restart(id string) error {
	s, err := r.get(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.proc != nil {
		_ = s.proc.Kill()
		_ = s.proc.Close()
	}
	proc, startErr := ptyproc.Start(s.profile.Command, s.profile.Args, s.cwd, nil, 80, 24)
	if startErr != nil {
		s.mu.Unlock()
		return fmt.Errorf("restart %s: %w", s.profile.Command, startErr)
	}
	s.proc = proc
	s.ring = ptyproc.NewRingBuffer(r.scrollbackCap)
	s.idleTrack = idle.NewTracker(r.idleThreshold(s.profile))
	s.status = protocol.StatusRunning
	s.startedAt = time.Now()
	s.mu.Unlock()

	go r.pump(s, proc)
	r.notifyList()
	return nil
}

func (r *Registry) Rename(id, name string) error {
	s, err := r.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
	r.notifyList()
	return nil
}

// SubscribeList registers onChange to receive the full session list
// whenever it changes, and immediately delivers the current snapshot.
func (r *Registry) SubscribeList(onChange func([]protocol.SessionSummary)) int {
	r.mu.Lock()
	r.nextListSubID++
	id := r.nextListSubID
	r.listSubs[id] = onChange
	r.mu.Unlock()
	onChange(r.List())
	return id
}

func (r *Registry) UnsubscribeList(subID int) {
	r.mu.Lock()
	delete(r.listSubs, subID)
	r.mu.Unlock()
}

func (r *Registry) notifyList() {
	snapshot := r.List()
	r.mu.Lock()
	subs := make([]func([]protocol.SessionSummary), 0, len(r.listSubs))
	for _, fn := range r.listSubs {
		subs = append(subs, fn)
	}
	r.mu.Unlock()
	for _, fn := range subs {
		fn(snapshot)
	}
}
