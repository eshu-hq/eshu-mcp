package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedRecorder lets the test read a streaming SSE response while the
// handler goroutine is still writing it.
type lockedRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *lockedRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func (r *lockedRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.WriteHeader(status)
}

func (r *lockedRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.Flush()
}

func (r *lockedRecorder) snapshot() (int, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Code, r.Body.String()
}

// TestWireServerBoundsSSESessionsFromEnv proves the operator knob reaches the
// transport through the real boot path and that /healthz surfaces the bound:
// with a cap of 1, the second GET /sse for the same credential is a 429 and
// /healthz reports one active session and one rejection.
func TestWireServerBoundsSSESessionsFromEnv(t *testing.T) {
	getenv := e2eEnvGetter(map[string]string{
		"ESHU_API_KEY": "deploy-key",
		"ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL": "1",
	})
	_, server, extra, err := wireServer([]string{"--core-base-url", "https://core:8443"}, getenv, quietLogger())
	if err != nil {
		t.Fatalf("wireServer() error = %v", err)
	}
	mux := server.Handler(extra)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	first.Header.Set("Authorization", "Bearer deploy-key")
	firstRec := &lockedRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(firstRec, first)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, body := firstRec.snapshot(); strings.Contains(body, "event: endpoint") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, body := firstRec.snapshot(); !strings.Contains(body, "event: endpoint") {
		t.Fatalf("first session was not established: %q", body)
	}

	second := httptest.NewRequest(http.MethodGet, "/sse", nil)
	second.Header.Set("Authorization", "Bearer deploy-key")
	secondRec := httptest.NewRecorder()
	mux.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second session = %d, want 429 at a cap of 1", secondRec.Code)
	}
	if secondRec.Header().Get("Retry-After") == "" {
		t.Error("429 has no Retry-After")
	}

	health := httptest.NewRecorder()
	mux.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body struct {
		SSE map[string]int64 `json:"sse"`
	}
	if err := json.Unmarshal(health.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /healthz: %v", err)
	}
	if body.SSE["active"] != 1 || body.SSE["rejected"] != 1 {
		t.Errorf("/healthz sse = %v, want active 1 and rejected 1", body.SSE)
	}

	cancel()
	<-done
}

// TestWireServerRejectsInvalidSSESessionCap proves a malformed cap fails the
// boot instead of silently falling back to the default.
func TestWireServerRejectsInvalidSSESessionCap(t *testing.T) {
	for _, value := range []string{"abc", "0", "-4", "1.5"} {
		getenv := e2eEnvGetter(map[string]string{
			"ESHU_API_KEY": "deploy-key",
			"ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL": value,
		})
		_, _, _, err := wireServer([]string{"--core-base-url", "https://core:8443"}, getenv, quietLogger())
		if err == nil || !strings.Contains(err.Error(), "ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL") {
			t.Errorf("value %q: wireServer() error = %v, want a refusal naming the variable", value, err)
		}
	}
}

// TestWireServerBoundsSSESessionsProcessWideFromEnv proves the ceiling knob
// reaches the transport: with a ceiling of 1 and a roomy per-principal cap, a
// second principal is refused, and a malformed ceiling fails the boot.
func TestWireServerBoundsSSESessionsProcessWideFromEnv(t *testing.T) {
	getenv := e2eEnvGetter(map[string]string{
		"ESHU_API_KEY":                    "deploy-key",
		"ESHU_MCP_MAX_SSE_SESSIONS_TOTAL": "1",
	})
	logs := &logSink{}
	_, server, extra, err := wireServer([]string{"--core-base-url", "https://core:8443"}, getenv,
		slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatalf("wireServer() error = %v", err)
	}
	mux := server.Handler(extra)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	first.Header.Set("Authorization", "Bearer deploy-key")
	rec := &lockedRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rec, first)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, body := rec.snapshot(); strings.Contains(body, "event: endpoint") {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// A second session through the same shared key: the default per-principal
	// cap (32) is nowhere near, so only the ceiling of 1 can refuse it. The
	// deadline keeps a missing ceiling a failed assertion, not a hang.
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer secondCancel()
	second := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(secondCtx)
	second.Header.Set("Authorization", "bearer deploy-key")
	secondRec := httptest.NewRecorder()
	mux.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("session over the ceiling = %d, want 429", secondRec.Code)
	}
	var refusal struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("decode ceiling 429 body %q: %v", secondRec.Body.String(), err)
	}
	if refusal.Error.Code != -32002 || refusal.Error.Message != "too many open sessions; close one or retry shortly" {
		t.Errorf("ceiling 429 error = %+v, want code -32002 and the neutral message", refusal.Error)
	}
	if out := logs.String(); !strings.Contains(out, "sse_session_ceiling_reached") || strings.Contains(out, "sse_session_cap_reached") {
		t.Errorf("log = %s\nwant the ceiling outcome and not the per-principal one", out)
	}
	cancel()
	<-done

	for _, value := range []string{"abc", "0", "-1"} {
		bad := e2eEnvGetter(map[string]string{
			"ESHU_API_KEY":                    "deploy-key",
			"ESHU_MCP_MAX_SSE_SESSIONS_TOTAL": value,
		})
		_, _, _, err := wireServer([]string{"--core-base-url", "https://core:8443"}, bad, quietLogger())
		if err == nil || !strings.Contains(err.Error(), "ESHU_MCP_MAX_SSE_SESSIONS_TOTAL") {
			t.Errorf("value %q: wireServer() error = %v, want a refusal naming the variable", value, err)
		}
	}
}

// logSink is a goroutine-safe log destination for asserting log outcomes.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}
