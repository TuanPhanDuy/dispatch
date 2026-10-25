package ptyproc

import (
	"strings"
	"sync"
)

// RingBuffer is a thread-safe, line-bounded scrollback buffer. It retains at
// most maxLines completed lines (raw bytes, including ANSI escapes, so the
// child's own colors/styles render through later) plus one trailing partial
// line not yet terminated by '\n'. Oldest lines are dropped once the cap is
// exceeded — resize/reflow of historical lines is intentionally not
// attempted (matches tmux's own scrollback behavior).
type RingBuffer struct {
	mu       sync.Mutex
	maxLines int
	lines    []string
	partial  []byte
}

// NewRingBuffer creates a buffer capped at maxLines (defaults to 5000 if <= 0).
func NewRingBuffer(maxLines int) *RingBuffer {
	if maxLines <= 0 {
		maxLines = 5000
	}
	return &RingBuffer{maxLines: maxLines}
}

// Write implements io.Writer, splitting p into completed lines on '\n'.
func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data := append(r.partial, p...)
	r.partial = nil

	start := 0
	for i, b := range data {
		if b == '\n' {
			r.appendLine(string(data[start : i+1]))
			start = i + 1
		}
	}
	if start < len(data) {
		r.partial = append([]byte(nil), data[start:]...)
	}
	return len(p), nil
}

func (r *RingBuffer) appendLine(line string) {
	r.lines = append(r.lines, line)
	if len(r.lines) > r.maxLines {
		r.lines = r.lines[len(r.lines)-r.maxLines:]
	}
}

// Snapshot returns the current scrollback (completed lines + trailing
// partial line) as a single renderable string.
func (r *RingBuffer) Snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var b strings.Builder
	for _, l := range r.lines {
		b.WriteString(l)
	}
	b.Write(r.partial)
	return b.String()
}
