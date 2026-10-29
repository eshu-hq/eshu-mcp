package dispatch

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
)

func testClient(t *testing.T, baseURL string) *readcontract.Client {
	t.Helper()

	client, err := readcontract.NewClient(readcontract.DefaultConfig(baseURL, ""))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

// stubCore serves canned answers and records what the dispatch sent. The
// handler runs on server goroutines, so it reports failures with Errorf.
type stubCore struct {
	t          *testing.T
	status     int
	body       string
	delay      time.Duration
	seenAuth   string
	seenAccept string
	seenCT     string
	seenMethod string
	seenPath   string
	seenQuery  string
	seenBody   string
}

func (s *stubCore) handler(w http.ResponseWriter, r *http.Request) {
	s.seenAuth = r.Header.Get("Authorization")
	s.seenAccept = r.Header.Get("Accept")
	s.seenCT = r.Header.Get("Content-Type")
	s.seenMethod = r.Method
	s.seenPath = r.URL.Path
	s.seenQuery = r.URL.RawQuery
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("read stub request body: %v", err)
	}
	s.seenBody = string(raw)
	if s.delay > 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(s.delay):
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	if _, err := io.WriteString(w, s.body); err != nil {
		s.t.Errorf("write stub response: %v", err)
	}
}

func dispatchAgainstStub(t *testing.T, stub *stubCore, tool string, args map[string]any, auth string, opts Options) (*Result, error) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	t.Cleanup(srv.Close)
	client := testClient(t, srv.URL)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Dispatch(context.Background(), client, tool, args, auth, quiet, opts)
}

// TestDispatchPinsBounds locks the census bounds from the dispatch side:
// the 30s default timeout and the 256 KiB default response budget.
func TestDispatchPinsBounds(t *testing.T) {
	t.Parallel()

	if defaultToolDispatchTimeout != 30*time.Second {
		t.Errorf("defaultToolDispatchTimeout = %v, want 30s", defaultToolDispatchTimeout)
	}
	if defaultToolResponseByteBudget != 256*1024 {
		t.Errorf("defaultToolResponseByteBudget = %d, want %d", defaultToolResponseByteBudget, 256*1024)
	}
	got := DefaultOptions()
	if got.Timeout != 30*time.Second || got.ResponseByteBudget != 256*1024 {
		t.Errorf("DefaultOptions() = %+v, want the pinned bounds", got)
	}
}

// TestDispatchEnvelopePassThrough proves a canonical core envelope crosses
// the dispatch untouched, with IsError following the core status.
func TestDispatchEnvelopePassThrough(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: `{"data":{"count":2},"truth":null,"error":null}`}
	result, err := dispatchAgainstStub(t, stub, "count_repositories_by_language",
		map[string]any{"language": "go"}, "Bearer core-credential", DefaultOptions())
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}
	if result.IsError {
		t.Error("Dispatch() IsError = true, want false for a 200 envelope")
	}
	if result.Envelope == nil {
		t.Fatal("Dispatch() envelope is nil, want the passed-through envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok || data["count"] != float64(2) {
		t.Errorf("Dispatch() data = %v, want the core payload", result.Envelope.Data)
	}
	if stub.seenAuth != "Bearer core-credential" {
		t.Errorf("forwarded Authorization = %q, want the caller credential verbatim", stub.seenAuth)
	}
	if stub.seenAccept != "application/eshu.envelope+json" {
		t.Errorf("Accept = %q, want the canonical envelope MIME", stub.seenAccept)
	}
}

// TestDispatchErrorEnvelopeMarksIsError proves a core error envelope with a
// non-2xx status arrives as an error result carrying the same envelope.
func TestDispatchErrorEnvelopeMarksIsError(t *testing.T) {
	t.Parallel()

	stub := &stubCore{
		status: 404,
		body:   `{"data":null,"truth":null,"error":{"code":"not_found","message":"no such repository","capability":"repos"}}`,
	}
	result, err := dispatchAgainstStub(t, stub, "get_repo_summary",
		map[string]any{"repo_id": "missing"}, "", DefaultOptions())
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}
	if !result.IsError {
		t.Error("Dispatch() IsError = false, want true for a 404 envelope")
	}
	if result.Envelope == nil || result.Envelope.Error == nil || result.Envelope.Error.Code != "not_found" {
		t.Errorf("Dispatch() envelope = %+v, want the core error envelope", result.Envelope)
	}
}

// TestDispatchPlainPayload proves a non-envelope JSON answer (the
// status/readiness family) decodes to a plain value.
func TestDispatchPlainPayload(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: `{"status":"ok","collectors":3}`}
	result, err := dispatchAgainstStub(t, stub, "list_collectors", map[string]any{}, "", DefaultOptions())
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}
	if result.Envelope != nil {
		t.Errorf("Dispatch() envelope = %+v, want nil for a plain payload", result.Envelope)
	}
	payload, ok := result.Value.(map[string]any)
	if !ok || payload["status"] != "ok" {
		t.Errorf("Dispatch() value = %v, want the decoded payload", result.Value)
	}
}

// TestDispatchHTTPErrorText proves a non-envelope error status renders the
// historic "HTTP %d" text error.
func TestDispatchHTTPErrorText(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 404, body: `not found`}
	_, err := dispatchAgainstStub(t, stub, "get_repo_summary",
		map[string]any{"repo_id": "missing"}, "", DefaultOptions())
	if err == nil || !strings.HasPrefix(err.Error(), "HTTP 404:") {
		t.Fatalf("Dispatch() error = %v, want the HTTP 404 text", err)
	}
}

// TestDispatchUnknownTool proves a name no route claims keeps the historic
// unknown-tool refusal.
func TestDispatchUnknownTool(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: `{}`}
	_, err := dispatchAgainstStub(t, stub, "no_such_tool", map[string]any{}, "", DefaultOptions())
	if err == nil || err.Error() != "unknown tool: no_such_tool" {
		t.Fatalf("Dispatch() error = %v, want the unknown-tool refusal", err)
	}
}

// TestDispatchTimeoutTypesTheDeadline proves a slow core answers with the
// typed timeout error whose structured content carries the canonical
// mcp_dispatch_timeout code.
func TestDispatchTimeoutTypesTheDeadline(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: `{"data":{},"truth":null,"error":null}`, delay: 300 * time.Millisecond}
	_, err := dispatchAgainstStub(t, stub, "count_repositories_by_language",
		map[string]any{"language": "go"}, "", Options{Timeout: 50 * time.Millisecond, ResponseByteBudget: 256 * 1024})
	if err == nil {
		t.Fatal("Dispatch() succeeded against a hanging core, want the timeout error")
	}
	structured, ok := dispatchErrorStructuredContent(err)
	if !ok {
		t.Fatalf("dispatchErrorStructuredContent(%v) = false, want the timeout envelope", err)
	}
	envelope, ok := structured["error"].(map[string]any)
	if !ok {
		t.Fatalf("structured error block = %T, want map[string]any", structured["error"])
	}
	if envelope["code"] != "mcp_dispatch_timeout" {
		t.Errorf("timeout code = %v, want mcp_dispatch_timeout", envelope["code"])
	}
	if envelope["capability"] != "mcp.dispatch" {
		t.Errorf("timeout capability = %v, want mcp.dispatch", envelope["capability"])
	}
}

// TestDispatchOverBudgetReplaces proves a response that does not fit the
// budget is replaced by the bounded mcp_response_over_budget envelope.
func TestDispatchOverBudgetReplaces(t *testing.T) {
	t.Parallel()

	big := `{"data":{"results":[{"id":1,"text":"` + strings.Repeat("x", 4000) +
		`"},{"id":2,"text":"` + strings.Repeat("y", 4000) + `"}],"count":2},"truth":null,"error":null}`
	stub := &stubCore{status: 200, body: big}
	result, err := dispatchAgainstStub(t, stub, "find_code",
		map[string]any{"query": "sweep"}, "", Options{Timeout: 30 * time.Second, ResponseByteBudget: 512})
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}
	if !result.IsError {
		t.Error("Dispatch() IsError = false, want true for the over-budget replacement")
	}
	if result.Envelope == nil || result.Envelope.Error == nil ||
		result.Envelope.Error.Code != "mcp_response_over_budget" {
		t.Errorf("Dispatch() envelope = %+v, want the over-budget envelope", result.Envelope)
	}
}

// TestDispatchRefusesEmptyPathSegment proves a required path argument left
// empty is refused before any core call, naming the argument.
func TestDispatchRefusesEmptyPathSegment(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: `{}`}
	_, err := dispatchAgainstStub(t, stub, "get_workload_context", map[string]any{}, "", DefaultOptions())
	if err == nil || !strings.Contains(err.Error(), "workload_id is required") {
		t.Fatalf("Dispatch() error = %v, want the workload_id refusal", err)
	}
	if stub.seenPath != "" {
		t.Errorf("core saw path %q, want no core call on a refused route", stub.seenPath)
	}
}
