package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	registry "github.com/eshu-hq/eshu-mcp"
)

// sseStream is one GET /sse driven in the background. The handler streams
// until its context is canceled, so the helper waits for either the endpoint
// event (session admitted) or the handler returning (session refused).
type sseStream struct {
	rec    *syncRecorder
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *sseStream) status() int {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	return s.rec.Code
}

func (s *sseStream) body() string {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	return s.rec.Body.String()
}

// close ends the stream and waits for the handler to return, so a test can
// assert the slot was freed without sleeping.
func (s *sseStream) close(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleSSE did not return after its context was canceled")
	}
}

func openSSE(t *testing.T, h http.Handler, auth string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	stream := &sseStream{
		rec:    &syncRecorder{ResponseRecorder: httptest.NewRecorder()},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	t.Cleanup(cancel)
	go func() {
		defer close(stream.done)
		h.ServeHTTP(stream.rec, req)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-stream.done:
			return stream
		default:
		}
		if strings.Contains(stream.body(), "data: ") {
			return stream
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("GET /sse neither admitted nor refused within 5s")
	return stream
}

func (s *sseStream) admitted() bool {
	select {
	case <-s.done:
		return false
	default:
		return strings.Contains(s.body(), "event: endpoint")
	}
}

func cappedServer(t *testing.T, capPerPrincipal int, opts ...ServerOption) (*Server, http.Handler) {
	t.Helper()
	core := &stubCore{status: 200, body: `{"data":{},"truth":null,"error":null}`}
	opts = append([]ServerOption{WithMaxSSESessionsPerPrincipal(capPerPrincipal)}, opts...)
	mcp, cleanup := testServer(core, registry.Definitions(), opts...)
	t.Cleanup(cleanup)
	return mcp, mcp.Handler(nil)
}

// TestSSESessionCapRefusesWithRetryableError proves the (cap+1)th session for
// one principal is refused with 429, Retry-After, and a JSON-RPC error, and
// that the refused request never inserts a session.
func TestSSESessionCapRefusesWithRetryableError(t *testing.T) {
	t.Parallel()

	mcp, h := cappedServer(t, 2)
	first := openSSE(t, h, "Bearer principal-a")
	second := openSSE(t, h, "Bearer principal-a")
	if !first.admitted() || !second.admitted() {
		t.Fatalf("sessions under the cap were not admitted: %q / %q", first.body(), second.body())
	}

	third := openSSE(t, h, "Bearer principal-a")
	if third.admitted() {
		t.Fatal("session over the cap was admitted")
	}
	if got := third.status(); got != http.StatusTooManyRequests {
		t.Fatalf("over-cap status = %d, want 429", got)
	}
	if got := third.rec.Header().Get("Retry-After"); got == "" {
		t.Error("over-cap response has no Retry-After header")
	}
	if got := third.rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("over-cap Content-Type = %q, want application/json", got)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(third.body()), &resp); err != nil {
		t.Fatalf("decode over-cap body %q: %v", third.body(), err)
	}
	if resp.Error == nil || resp.Error.Code != sseSessionLimitErrorCode {
		t.Errorf("over-cap error = %+v, want code %d", resp.Error, sseSessionLimitErrorCode)
	}
	if strings.Contains(third.body(), "principal-a") {
		t.Error("over-cap body leaks the credential")
	}

	stats := mcp.SSEStats()
	if stats.Active != 2 || stats.Rejected != 1 {
		t.Errorf("SSEStats() = %+v, want Active 2 and Rejected 1", stats)
	}
}

// TestSSESessionSlotFreedOnClose proves a closed connection returns its slot,
// so a reconnecting client is not locked out by its own stale session.
func TestSSESessionSlotFreedOnClose(t *testing.T) {
	t.Parallel()

	mcp, h := cappedServer(t, 1)
	first := openSSE(t, h, "Bearer principal-a")
	if !first.admitted() {
		t.Fatalf("first session not admitted: %q", first.body())
	}
	if blocked := openSSE(t, h, "Bearer principal-a"); blocked.status() != http.StatusTooManyRequests {
		t.Fatalf("second session status = %d, want 429 while the first is open", blocked.status())
	}

	first.close(t)
	if got := mcp.SSEStats().Active; got != 0 {
		t.Fatalf("Active after close = %d, want 0", got)
	}
	again := openSSE(t, h, "Bearer principal-a")
	if !again.admitted() {
		t.Fatalf("session after the slot freed was refused: status %d body %q", again.status(), again.body())
	}
}

// TestSSESessionCapIsPerPrincipal proves one principal at its cap does not
// consume another principal's allowance.
func TestSSESessionCapIsPerPrincipal(t *testing.T) {
	t.Parallel()

	_, h := cappedServer(t, 1)
	a := openSSE(t, h, "Bearer principal-a")
	b := openSSE(t, h, "Bearer principal-b")
	if !a.admitted() || !b.admitted() {
		t.Fatalf("distinct principals shared a cap: a=%q b=%q", a.body(), b.body())
	}
	if again := openSSE(t, h, "Bearer principal-a"); again.status() != http.StatusTooManyRequests {
		t.Errorf("principal-a second session = %d, want 429", again.status())
	}
	if again := openSSE(t, h, "Bearer principal-b"); again.status() != http.StatusTooManyRequests {
		t.Errorf("principal-b second session = %d, want 429", again.status())
	}
}

// TestSSESessionCapBoundsUnauthenticatedTransport proves the escape-hatch
// transport (no edge auth, so every session has an empty principal) is
// bounded as one shared bucket rather than left unbounded.
func TestSSESessionCapBoundsUnauthenticatedTransport(t *testing.T) {
	t.Parallel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	noDispatch := func(context.Context, string, map[string]any, string) (any, error) { return nil, nil }
	mcp := NewServer(registry.Definitions(), noDispatch, quiet, "eshu-mcp-test", "test",
		WithMaxSSESessionsPerPrincipal(2))
	h := mcp.Handler(nil)

	first := openSSE(t, h, "")
	second := openSSE(t, h, "")
	if !first.admitted() || !second.admitted() {
		t.Fatalf("anonymous sessions under the cap were refused: %q / %q", first.body(), second.body())
	}
	if third := openSSE(t, h, ""); third.status() != http.StatusTooManyRequests {
		t.Errorf("third anonymous session = %d, want 429", third.status())
	}
}

// Probabilistic by nature: the check-and-insert atomicity is one sessMu
// section, so this and -race are stress evidence, not a proof; the direct
// registerSession stress in sse_limit_accounting_test.go is the tighter loop.
// TestSSESessionCapNeverExceededUnderConcurrentMints races many mints for one
// principal against a small cap: exactly cap sessions are admitted, every
// other request is refused, and the live count never passes the cap.
func TestSSESessionCapNeverExceededUnderConcurrentMints(t *testing.T) {
	t.Parallel()

	const (
		capPerPrincipal = 5
		attempts        = 64
	)
	mcp, h := cappedServer(t, capPerPrincipal)

	var (
		admitted atomic.Int64
		refused  atomic.Int64
		peak     atomic.Int64
		wg       sync.WaitGroup
		start    = make(chan struct{})
	)
	streams := make([]*sseStream, attempts)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			stream := openSSE(t, h, "Bearer principal-a")
			streams[i] = stream
			if stream.admitted() {
				admitted.Add(1)
			} else if stream.status() == http.StatusTooManyRequests {
				refused.Add(1)
			}
			if active := int64(mcp.SSEStats().Active); active > peak.Load() {
				peak.Store(active)
			}
		}()
	}
	close(start)
	wg.Wait()

	if admitted.Load() != capPerPrincipal {
		t.Errorf("admitted = %d, want exactly %d", admitted.Load(), capPerPrincipal)
	}
	if refused.Load() != attempts-capPerPrincipal {
		t.Errorf("refused = %d, want %d", refused.Load(), attempts-capPerPrincipal)
	}
	if peak.Load() > capPerPrincipal {
		t.Errorf("peak live sessions = %d, exceeds the cap %d", peak.Load(), capPerPrincipal)
	}
	for _, stream := range streams {
		stream.close(t)
	}
	if got := mcp.SSEStats().Active; got != 0 {
		t.Errorf("Active after every close = %d, want 0", got)
	}
}

// TestSSESessionCapChurnKeepsAccounting opens and closes sessions from many
// goroutines and proves the books balance: no leaked slot, no underflow.
func TestSSESessionCapChurnKeepsAccounting(t *testing.T) {
	t.Parallel()

	mcp, h := cappedServer(t, 3)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 8 {
				stream := openSSE(t, h, "Bearer principal-a")
				stream.close(t)
			}
		}()
	}
	wg.Wait()
	if got := mcp.SSEStats().Active; got != 0 {
		t.Errorf("Active after churn = %d, want 0 (leaked slot)", got)
	}
	if after := openSSE(t, h, "Bearer principal-a"); !after.admitted() {
		t.Errorf("session after churn refused: %d", after.status())
	}
}

// TestSSESessionCapDefaultsAndInvalidValues proves an unset or non-positive
// option keeps the default instead of disabling the bound.
func TestSSESessionCapDefaultsAndInvalidValues(t *testing.T) {
	t.Parallel()

	if defaultMaxSSESessionsPerPrincipal <= 0 {
		t.Fatalf("defaultMaxSSESessionsPerPrincipal = %d, want > 0", defaultMaxSSESessionsPerPrincipal)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, opts := range map[string][]ServerOption{
		"unset":    nil,
		"zero":     {WithMaxSSESessionsPerPrincipal(0)},
		"negative": {WithMaxSSESessionsPerPrincipal(-3)},
	} {
		s := NewServer(registry.Definitions(), nil, quiet, "t", "t", opts...)
		if got := s.maxSSEPerPrincipal(); got != defaultMaxSSESessionsPerPrincipal {
			t.Errorf("%s: cap = %d, want default %d", name, got, defaultMaxSSESessionsPerPrincipal)
		}
	}
}
