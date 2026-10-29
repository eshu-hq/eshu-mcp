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
	"testing"
	"time"

	registry "github.com/eshu-hq/eshu-mcp"
)

// TestSSESessionRoundTrip proves session establishment, SSE-linked 202, and
// the principal-mismatch 403.
func TestSSESessionRoundTrip(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{"data":{},"truth":null,"error":null}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()
	mux := mcp.Handler(nil)

	// Unauthenticated SSE establishment is refused before a session exists.
	anon := httptest.NewRequest(http.MethodGet, "/sse", nil)
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /sse without credential = %d, want 401", anonRec.Code)
	}

	// Establish a session as principal A. The handler streams, so drive it
	// in the background behind a mutex-guarded recorder: the handler
	// goroutine writes keepalives while this goroutine reads the endpoint
	// event, and httptest.ResponseRecorder is not goroutine-safe.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	establish := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	establish.Header.Set("Authorization", "Bearer principal-a")
	estRec := &syncRecorder{ResponseRecorder: httptest.NewRecorder()}
	go func() {
		mux.ServeHTTP(estRec, establish)
	}()
	body := ""
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		estRec.mu.Lock()
		body = estRec.Body.String()
		estRec.mu.Unlock()
		if strings.Contains(body, "data: ") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	endpoint := ""
	for _, line := range strings.Split(body, "\n") {
		if value, ok := strings.CutPrefix(line, "data: "); ok {
			endpoint = value
			break
		}
	}
	if !strings.HasPrefix(endpoint, "/mcp/message?sessionId=sess_") {
		t.Fatalf("SSE endpoint event = %q, want the session URL", endpoint)
	}
	// The SSE stream stays open for the linked POSTs below; the deferred
	// cancel closes it at test end.

	// A different principal posting into A's session is refused with 403
	// before the body is decoded.
	mismatch := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	mismatch.Header.Set("Authorization", "Bearer principal-b")
	mismatchRec := httptest.NewRecorder()
	mux.ServeHTTP(mismatchRec, mismatch)
	if mismatchRec.Code != http.StatusForbidden {
		t.Fatalf("session mismatch status = %d, want 403", mismatchRec.Code)
	}
	var denied map[string]any
	if err := json.Unmarshal(mismatchRec.Body.Bytes(), &denied); err != nil {
		t.Fatalf("decode 403 body: %v", err)
	}
	failure := mustMap(t, denied["error"])
	if failure["code"] != float64(-32001) {
		t.Errorf("session mismatch error = %v, want -32001", denied["error"])
	}

	// The session owner posting a ping gets 202 (answer via the SSE stream).
	owner := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	owner.Header.Set("Authorization", "Bearer principal-a")
	ownerRec := httptest.NewRecorder()
	mux.ServeHTTP(ownerRec, owner)
	if ownerRec.Code != http.StatusAccepted {
		t.Errorf("SSE-linked POST status = %d, want 202", ownerRec.Code)
	}
}

// syncRecorder guards an httptest.ResponseRecorder for the streaming test
// above, where the handler goroutine writes while the test goroutine reads.
type syncRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *syncRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func (r *syncRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.WriteHeader(status)
}

func (r *syncRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.Flush()
}

// TestEdgeAuthPrincipalBinding proves the edge recognizes the shared key as
// "shared" and binds any other present credential to a stable opaque
// principal, refusing only the headerless request.
func TestEdgeAuthPrincipalBinding(t *testing.T) {
	t.Parallel()

	principal, ok := edgeAuth("deploy-key", "Bearer deploy-key")
	if !ok || principal != "shared" {
		t.Errorf("edgeAuth(shared match) = (%q, %v), want (shared, true)", principal, ok)
	}
	first, ok := edgeAuth("deploy-key", "Bearer some-scoped-token")
	if !ok || !strings.HasPrefix(first, "fwd:") {
		t.Fatalf("edgeAuth(other bearer) = (%q, %v), want an opaque forward principal", first, ok)
	}
	second, ok := edgeAuth("deploy-key", "Bearer some-scoped-token")
	if !ok || second != first {
		t.Errorf("edgeAuth() is not stable across calls: %q vs %q", first, second)
	}
	if _, ok := edgeAuth("deploy-key", ""); ok {
		t.Error("edgeAuth(empty) = true, want refusal")
	}
	if _, ok := edgeAuth("deploy-key", "   "); ok {
		t.Error("edgeAuth(whitespace) = true, want refusal")
	}
}

// TestUnauthorizedCorrelationPassthrough proves the 401 carries the caller's
// correlation id when one is present.
func TestUnauthorizedCorrelationPassthrough(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
	req.Header.Set("X-Correlation-ID", "caller-trace-1")
	rec := httptest.NewRecorder()
	writeUnauthorized(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var decoded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode 401: %v", err)
	}
	if decoded["correlation_id"] != "caller-trace-1" {
		t.Errorf("correlation_id = %q, want the caller value", decoded["correlation_id"])
	}
}

// TestMessageMiscellany proves ping, notifications/initialized, and the
// structured shape override behave.
func TestMessageMiscellany(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{"data":{"ok":true},"truth":null,"error":null}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()
	mux := mcp.Handler(nil)

	_, decoded := postMessage(t, mux, `{"jsonrpc":"2.0","id":9,"method":"ping"}`, "Bearer k")
	result := mustMap(t, decoded["result"])
	if len(result) != 0 {
		t.Errorf("ping result = %v, want the empty object", result)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp/message",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Authorization", "Bearer k")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("notification status = %d, want 204", rec.Code)
	}

	structured := httptest.NewRequest(http.MethodPost, "/mcp/message?shape=structured",
		strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"count_repositories_by_language","arguments":{"language":"go"}}}`))
	structured.Header.Set("Authorization", "Bearer k")
	structuredRec := httptest.NewRecorder()
	mux.ServeHTTP(structuredRec, structured)
	var structuredDecoded map[string]any
	if err := json.Unmarshal(structuredRec.Body.Bytes(), &structuredDecoded); err != nil {
		t.Fatalf("decode structured tools/call: %v", err)
	}
	structuredResult := mustMap(t, structuredDecoded["result"])
	content := mustSlice(t, structuredResult["content"])
	if len(content) != 1 {
		t.Errorf("structured tools/call content blocks = %d, want text only", len(content))
	}

	_, decoded = postMessage(t, mux,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`,
		"Bearer k")
	direct := mustMap(t, decoded["result"])
	if direct["isError"] != true {
		t.Errorf("direct unknown tool isError = %v, want true", direct["isError"])
	}
	text := mustJSON(t, direct["content"])
	if !strings.Contains(string(text), "unknown tool: no_such_tool") {
		t.Errorf("direct unknown tool content = %s, want the refusal", text)
	}
}

// TestFailingDispatchRenders proves a transport-level dispatch failure
// renders the typed error result instead of escaping the handler.
func TestFailingDispatchRenders(t *testing.T) {
	t.Parallel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	failing := func(context.Context, string, map[string]any, string) (any, error) {
		return nil, context.DeadlineExceeded
	}
	mcp := NewServer(registry.Definitions(), failing, quiet, "eshu-mcp-test", "test",
		WithTransportAuth(EdgeAuthMiddleware("", quiet)))

	post := func(shape string) map[string]any {
		url := "/mcp/message"
		if shape != "" {
			url += "?shape=" + shape
		}
		req := httptest.NewRequest(http.MethodPost, url,
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_code","arguments":{"query":"x"}}}`))
		req.Header.Set("Authorization", "Bearer k")
		rec := httptest.NewRecorder()
		mcp.Handler(nil).ServeHTTP(rec, req)
		var decoded map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode failed dispatch: %v", err)
		}
		result := mustMap(t, decoded["result"])
		return result
	}
	dual := post("")
	if dual["isError"] != true {
		t.Errorf("failed dispatch isError = %v, want true", dual["isError"])
	}
	structured := post("structured")
	body := mustJSON(t, structured["structuredContent"])
	if !strings.Contains(string(body), "mcp_dispatch_error") {
		t.Errorf("failed structured dispatch result = %s, want the error envelope", body)
	}
}
