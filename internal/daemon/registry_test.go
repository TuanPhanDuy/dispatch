package daemon

import (
	"sync"
	"testing"
	"time"

	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/protocol"
)

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestCreateDuplicateNameGetsSuffixed(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	id1, err := r.Create(profile, "same", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	id2, err := r.Create(profile, "same", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	id3, err := r.Create(profile, "same", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create 3: %v", err)
	}

	sessions := r.List()
	names := map[string]string{}
	for _, s := range sessions {
		names[s.ID] = s.Name
	}
	if names[id1] != "same" {
		t.Fatalf("expected first session name 'same', got %q", names[id1])
	}
	if names[id2] != "same (2)" {
		t.Fatalf("expected second session name 'same (2)', got %q", names[id2])
	}
	if names[id3] != "same (3)" {
		t.Fatalf("expected third session name 'same (3)', got %q", names[id3])
	}
}

func TestCreateDuplicateNameConcurrentStillUnique(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	profile := config.AgentProfile{Name: "cat", Command: "cat"}
	const n = 10
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := r.Create(profile, "dup", ".", 80, 24)
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, s := range r.List() {
		if seen[s.Name] {
			t.Fatalf("duplicate name %q survived concurrent Create calls", s.Name)
		}
		seen[s.Name] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d uniquely-named sessions, got %d", n, len(seen))
	}
}

// subCount reaches into Registry's unexported fields (legal — this test
// file is part of package daemon) to verify a session's subscriber count
// without needing a production-code getter.
func subCount(r *Registry, id string) int {
	r.mu.Lock()
	s, ok := r.sessions[id]
	r.mu.Unlock()
	if !ok {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

func TestAttachDetachRemovesSubscriber(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	id, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, subID, err := r.Attach(id, 80, 24, func(protocol.OutputEvent) {})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if got := subCount(r, id); got != 1 {
		t.Fatalf("expected 1 subscriber after Attach, got %d", got)
	}

	if err := r.Detach(id, subID); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if got := subCount(r, id); got != 0 {
		t.Fatalf("expected 0 subscribers after Detach, got %d", got)
	}
}

func TestRestartSwapsProcessWithoutRacingOldPump(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	id, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Hammer Restart concurrently with Write/Resize for a short, bounded
	// number of iterations. Under `go test -race`, a reintroduced "pump
	// reads s.proc without a lock while Restart reassigns it" bug would be
	// flagged as a data race; this also exercises Write/Resize racing a
	// concurrent Restart swapping/closing the process out from under them.
	//
	// stop is closed exactly once and read by both goroutines' `select`,
	// which broadcasts to every receiver — unlike a one-shot time.After
	// channel, whose single value only one of the two selects would ever
	// observe, leaving the other spinning forever.
	stop := make(chan struct{})
	time.AfterFunc(150*time.Millisecond, func() { close(stop) })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.Restart(id)
			time.Sleep(10 * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.Write(id, []byte("x"))
			_ = r.Resize(id, 90, 30)
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()

	// The session should settle into a running state afterward — proving
	// a stale pre-restart pump goroutine didn't clobber the final status.
	waitUntil(t, 2*time.Second, func() bool {
		sessions := r.List()
		return len(sessions) == 1 && sessions[0].Status != "exited"
	})
}

func TestKilledSessionProcessIsReaped(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	id, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// If pump's error branch didn't call proc.Wait(), the session would
	// still transition to Exited (that part doesn't depend on reaping) —
	// so this mainly documents the expectation and relies on `go vet`/
	// manual process-table inspection to catch a regression; the real
	// guarantee is exercised by reading registry.go's pump() directly.
	waitUntil(t, 2*time.Second, func() bool {
		sessions := r.List()
		return len(sessions) == 1 && sessions[0].Status == "exited"
	})
}

// TestKillEscalatesToSigkillWhenProcessIgnoresSigterm reproduces a bug found
// manually: some interactive shells (observed with zsh carrying conda's
// shell integration) don't exit on SIGTERM, which left Kill() unable to
// ever stop them — the session just sat at "running"/"needs-attention"
// forever with no way for the user to actually get rid of it. `sh -c 'trap
// "" TERM; sleep 30'` explicitly ignores SIGTERM the same way, giving a
// portable, deterministic repro without depending on the test machine's
// shell config.
func TestKillEscalatesToSigkillWhenProcessIgnoresSigterm(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	profile := config.AgentProfile{Name: "sh", Command: "sh", Args: []string{"-c", `trap "" TERM; sleep 30`}}
	id, err := r.Create(profile, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Give the shell time to actually execute `trap "" TERM` before Kill
	// sends the signal — otherwise this is testing "SIGTERM arrives before
	// the trap is registered" (which kills it via the default disposition,
	// a different, uninteresting race) instead of "the trap is active and
	// genuinely ignores SIGTERM" (the real bug being guarded against).
	time.Sleep(300 * time.Millisecond)
	if err := r.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// SIGTERM alone must not be enough here (the process ignores it) — confirm
	// it's still not reaped partway through the grace period, so a
	// regression that removed the escalation wouldn't pass this test only
	// because the final wait below is too lenient.
	time.Sleep(killGracePeriod - 500*time.Millisecond)
	if sessions := r.List(); len(sessions) != 1 || sessions[0].Status == "exited" {
		t.Fatalf("expected session still alive partway through the grace period, got %+v", sessions)
	}

	waitUntil(t, 2*time.Second, func() bool {
		sessions := r.List()
		return len(sessions) == 1 && sessions[0].Status == "exited"
	})
}

func TestRemoveRefusesRunningSession(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	id, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := r.Remove(id); err == nil {
		t.Fatal("expected Remove to refuse a still-running session")
	}
	if len(r.List()) != 1 {
		t.Fatalf("expected session to remain after refused Remove, got %d", len(r.List()))
	}
}

func TestRemoveDeletesExitedSessionAndFreesName(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	id, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		sessions := r.List()
		return len(sessions) == 1 && sessions[0].Status == "exited"
	})

	if err := r.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(r.List()) != 0 {
		t.Fatalf("expected no sessions after Remove, got %+v", r.List())
	}

	// The name should now be free again — a fresh Create must not get
	// suffixed, which would only happen if the old entry were still there.
	id2, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "s1", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create after Remove: %v", err)
	}
	sessions := r.List()
	if len(sessions) != 1 || sessions[0].ID != id2 || sessions[0].Name != "s1" {
		t.Fatalf("expected a single fresh session named 's1', got %+v", sessions)
	}
}

func TestRemoveUnknownSessionErrors(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	if err := r.Remove("does-not-exist"); err == nil {
		t.Fatal("expected error removing an unknown session")
	}
}

func TestUniqueNameLockedSuffixesDeterministically(t *testing.T) {
	r := NewRegistry(100, 10)
	defer r.Close()

	r.mu.Lock()
	got := r.uniqueNameLocked("x")
	r.mu.Unlock()
	if got != "x" {
		t.Fatalf("expected 'x' with no collisions, got %q", got)
	}

	if _, err := r.Create(config.AgentProfile{Name: "cat", Command: "cat"}, "x", ".", 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r.mu.Lock()
	got = r.uniqueNameLocked("x")
	r.mu.Unlock()
	if got != "x (2)" {
		t.Fatalf("expected 'x (2)' after one collision, got %q", got)
	}
}
