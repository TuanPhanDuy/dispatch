package idle

import (
	"testing"
	"time"
)

func TestNeedsAttentionFalseBeforeFirstOutput(t *testing.T) {
	now := time.Now()
	tr := newTrackerWithClock(5*time.Second, func() time.Time { return now })
	now = now.Add(time.Hour)
	if tr.NeedsAttention() {
		t.Fatal("expected NeedsAttention=false before any output observed")
	}
}

func TestNeedsAttentionAfterSilenceThreshold(t *testing.T) {
	now := time.Now()
	tr := newTrackerWithClock(5*time.Second, func() time.Time { return now })

	tr.OnOutput()
	if tr.NeedsAttention() {
		t.Fatal("expected NeedsAttention=false immediately after output")
	}

	now = now.Add(4 * time.Second)
	if tr.NeedsAttention() {
		t.Fatal("expected NeedsAttention=false before threshold elapses")
	}

	now = now.Add(2 * time.Second)
	if !tr.NeedsAttention() {
		t.Fatal("expected NeedsAttention=true once threshold has elapsed")
	}
}

func TestOnOutputResetsTimer(t *testing.T) {
	now := time.Now()
	tr := newTrackerWithClock(5*time.Second, func() time.Time { return now })

	tr.OnOutput()
	now = now.Add(10 * time.Second)
	if !tr.NeedsAttention() {
		t.Fatal("expected NeedsAttention=true after long silence")
	}

	tr.OnOutput()
	if tr.NeedsAttention() {
		t.Fatal("expected NeedsAttention=false right after fresh output")
	}
}
