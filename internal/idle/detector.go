// Package idle implements the v1 "needs attention" heuristic: a session is
// flagged once its PTY has gone quiet for a configurable threshold. This is
// deliberately simple (no prompt/ANSI content parsing) — see the project
// plan for why that's a documented v1 limitation rather than an oversight.
package idle

import (
	"sync"
	"time"
)

// Tracker tracks a single session's output-silence duration.
type Tracker struct {
	mu        sync.Mutex
	now       func() time.Time
	threshold time.Duration
	lastAt    time.Time
	hadOutput bool
}

// NewTracker creates a tracker that flags attention after threshold of
// silence, once at least one output event has been observed.
func NewTracker(threshold time.Duration) *Tracker {
	return newTrackerWithClock(threshold, time.Now)
}

func newTrackerWithClock(threshold time.Duration, now func() time.Time) *Tracker {
	return &Tracker{threshold: threshold, now: now, lastAt: now()}
}

// OnOutput records that output was just observed, resetting the silence timer.
func (t *Tracker) OnOutput() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hadOutput = true
	t.lastAt = t.now()
}

// NeedsAttention reports whether the session has gone quiet for >= threshold
// since its first output (a session that hasn't printed anything yet isn't
// flagged — it just hasn't started talking).
func (t *Tracker) NeedsAttention() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hadOutput {
		return false
	}
	return t.now().Sub(t.lastAt) >= t.threshold
}
