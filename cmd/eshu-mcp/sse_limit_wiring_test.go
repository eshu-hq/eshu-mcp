package main

import (
	"context"
	"encoding/json"
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
