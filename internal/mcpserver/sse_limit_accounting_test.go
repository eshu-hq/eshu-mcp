package mcpserver

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

func accountingServer(capPerPrincipal int) *Server {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(registry.Definitions(), nil, quiet, "t", "t", WithMaxSSESessionsPerPrincipal(capPerPrincipal))
}

func registerN(t *testing.T, s *Server, principal, prefix string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := range n {
		id := fmt.Sprintf("%s-%d", prefix, i)
		if got := s.registerSession(id, &sseSession{principal: principal}); got != sseAdmitted {
			t.Fatalf("registerSession(%s) = %v, want admitted", id, got)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestUnregisterSessionIsIdempotentAndExact proves the slot is returned once
// per registered id: a repeated or unknown unregister neither underflows the
// count nor frees a slot that belongs to a live session, and the cap stays
// exactly N afterwards.
func TestUnregisterSessionIsIdempotentAndExact(t *testing.T) {
	t.Parallel()

	const capPerPrincipal = 3
	s := accountingServer(capPerPrincipal)

	ids := registerN(t, s, "p", "first", 1)
	s.unregisterSession(ids[0])
	s.unregisterSession(ids[0])      // second close of the same id
	s.unregisterSession("never-set") // id that was never registered
	if len(s.sseLimit.perPrincipal) != 0 || len(s.sessions) != 0 {
		t.Fatalf("books not empty: perPrincipal=%v sessions=%d", s.sseLimit.perPrincipal, len(s.sessions))
	}

	registerN(t, s, "p", "again", capPerPrincipal)
	if got := s.registerSession("over", &sseSession{principal: "p"}); got != sseAdmitPrincipalCap {
		t.Errorf("cap after double unregister is not exactly %d: over-cap register = %v", capPerPrincipal, got)
	}
}

// TestUnregisterSessionDoesNotTouchOtherSessions proves a duplicate unregister
// of one session leaves its siblings' slots, and another principal's, intact.
func TestUnregisterSessionDoesNotTouchOtherSessions(t *testing.T) {
	t.Parallel()

	s := accountingServer(2)
	a := registerN(t, s, "a", "a", 2)
	registerN(t, s, "b", "b", 1)

	s.unregisterSession(a[0])
	s.unregisterSession(a[0])
	if got := s.sseLimit.perPrincipal["a"]; got != 1 {
		t.Errorf("principal a count = %d, want 1 (one live sibling)", got)
	}
	if got := s.sseLimit.perPrincipal["b"]; got != 1 {
		t.Errorf("principal b count = %d, want 1", got)
	}
	if got := s.registerSession("a-new", &sseSession{principal: "a"}); got != sseAdmitted {
		t.Errorf("one freed slot did not admit one session: %v", got)
	}
	if got := s.registerSession("a-over", &sseSession{principal: "a"}); got != sseAdmitPrincipalCap {
		t.Errorf("second register after one free = %v, want the cap refusal", got)
	}
}

// TestSessionSlotsFreeOneAtATime opens N sessions, closes one, and asserts
// exactly one more is admitted and the next refused.
func TestSessionSlotsFreeOneAtATime(t *testing.T) {
	t.Parallel()

	const capPerPrincipal = 4
	s := accountingServer(capPerPrincipal)
	ids := registerN(t, s, "p", "s", capPerPrincipal)
	if got := s.registerSession("full", &sseSession{principal: "p"}); got != sseAdmitPrincipalCap {
		t.Fatalf("register at the cap = %v, want refusal", got)
	}

	s.unregisterSession(ids[0])
	if got := s.registerSession("one", &sseSession{principal: "p"}); got != sseAdmitted {
		t.Fatalf("register after one close = %v, want admitted", got)
	}
	if got := s.registerSession("two", &sseSession{principal: "p"}); got != sseAdmitPrincipalCap {
		t.Fatalf("second register after one close = %v, want refusal", got)
	}
	if got := s.sseLimit.perPrincipal["p"]; got != capPerPrincipal {
		t.Errorf("count = %d, want exactly %d", got, capPerPrincipal)
	}
}

// TestRegisterSessionConcurrentNeverExceedsCap hammers registerSession
// directly, with no HTTP in the way, so the contention on the check-and-insert
// section is as high as it can be. Atomicity itself is a property of the code
// (one sessMu section), which no hook can prove from outside; this test and
// -race are the stress evidence. It is probabilistic: a regression that splits
// the section fails it only when the scheduler interleaves the halves, and the
// exact-count assertion makes that a likely, not certain, catch.
func TestRegisterSessionConcurrentNeverExceedsCap(t *testing.T) {
	t.Parallel()

	const (
		capPerPrincipal = 7
		attempts        = 512
	)
	s := accountingServer(capPerPrincipal)
	var (
		admitted atomic.Int64
		wg       sync.WaitGroup
		start    = make(chan struct{})
	)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if s.registerSession(fmt.Sprintf("c-%d", i), &sseSession{principal: "p"}) == sseAdmitted {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if admitted.Load() != capPerPrincipal {
		t.Errorf("admitted = %d, want exactly %d", admitted.Load(), capPerPrincipal)
	}
	if got := s.sseLimit.perPrincipal["p"]; got != capPerPrincipal {
		t.Errorf("count = %d, want %d", got, capPerPrincipal)
	}
}

// TestCeilingAccountingSurvivesDoubleUnregister covers the process-wide
// ceiling. It is derived from the live session map (there is no second
// counter to drift), so a repeated or unknown unregister cannot underflow it;
// the test pins that: after every unregister the total returns to zero, and
// the ceiling then admits exactly its limit again.
func TestCeilingAccountingSurvivesDoubleUnregister(t *testing.T) {
	t.Parallel()

	const ceiling = 3
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(registry.Definitions(), nil, quiet, "t", "t",
		WithMaxSSESessionsPerPrincipal(10), WithMaxSSESessions(ceiling))

	var ids []string
	for i := range ceiling {
		ids = append(ids, registerN(t, s, fmt.Sprintf("p%d", i), fmt.Sprintf("c%d", i), 1)...)
	}
	if got := s.registerSession("over", &sseSession{principal: "p9"}); got != sseAdmitCeiling {
		t.Fatalf("register at the ceiling = %v, want the ceiling refusal", got)
	}

	s.unregisterSession(ids[0])
	s.unregisterSession(ids[0])
	s.unregisterSession("never-set")
	if got := s.SSEStats().Active; got != ceiling-1 {
		t.Fatalf("Active after a double unregister = %d, want %d", got, ceiling-1)
	}
	if got := s.registerSession("one", &sseSession{principal: "p9"}); got != sseAdmitted {
		t.Fatalf("register after one freed slot = %v, want admitted", got)
	}
	if got := s.registerSession("two", &sseSession{principal: "p9"}); got != sseAdmitCeiling {
		t.Fatalf("second register after one freed slot = %v, want the ceiling refusal", got)
	}

	for _, id := range append(ids[1:], "one") {
		s.unregisterSession(id)
		s.unregisterSession(id)
	}
	if got := s.SSEStats().Active; got != 0 {
		t.Fatalf("Active after unregistering everything twice = %d, want 0", got)
	}
	if len(s.sseLimit.perPrincipal) != 0 {
		t.Errorf("perPrincipal not empty: %v", s.sseLimit.perPrincipal)
	}
	for i := range ceiling {
		if got := s.registerSession(fmt.Sprintf("r%d", i), &sseSession{principal: "q"}); got != sseAdmitted {
			t.Fatalf("re-register %d = %v, want admitted after the books returned to zero", i, got)
		}
	}
	if got := s.registerSession("r-over", &sseSession{principal: "q"}); got != sseAdmitCeiling {
		t.Errorf("ceiling after the round trip is not exactly %d: %v", ceiling, got)
	}
}

// TestRegisterSessionConcurrentNeverExceedsCeiling hammers the process-wide
// ceiling the way the per-principal stress test hammers the cap: 512
// goroutines over distinct principals, each far below its own cap, against a
// ceiling of 7. Only the ceiling can refuse, so a regression that splits its
// check from the insert (read under RLock, then insert under Lock) admits more
// than 7.
//
// Stress evidence, not proof: the atomicity is one sessMu section and this
// test catches a split only when the scheduler interleaves the halves. It
// does catch the widened-window mutation (check, sleep, insert) reliably.
func TestRegisterSessionConcurrentNeverExceedsCeiling(t *testing.T) {
	t.Parallel()

	const (
		ceiling  = 7
		attempts = 512
	)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(registry.Definitions(), nil, quiet, "t", "t",
		WithMaxSSESessionsPerPrincipal(10), WithMaxSSESessions(ceiling))

	var (
		admitted atomic.Int64
		wg       sync.WaitGroup
		start    = make(chan struct{})
	)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			sess := &sseSession{principal: fmt.Sprintf("p-%d", i)}
			if s.registerSession(fmt.Sprintf("c-%d", i), sess) == sseAdmitted {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if admitted.Load() != ceiling {
		t.Errorf("admitted = %d, want exactly %d", admitted.Load(), ceiling)
	}
	if got := s.SSEStats().Active; got != ceiling {
		t.Errorf("Active = %d, want %d", got, ceiling)
	}
}
