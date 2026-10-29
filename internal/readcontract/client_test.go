package readcontract

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// writeResponse writes a test-server body and reports failures with Errorf:
// handlers run on server goroutines, where Fatalf would be wrong.
func writeResponse(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Errorf("write test response: %v", err)
	}
}

func TestDefaultPins(t *testing.T) {
	t.Parallel()

	if DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", DefaultTimeout)
	}
	if DefaultResponseByteBudget != 256*1024 {
		t.Errorf("DefaultResponseByteBudget = %d, want %d", DefaultResponseByteBudget, 256*1024)
	}
	if EnvelopeMIME != "application/eshu.envelope+json" {
		t.Errorf("EnvelopeMIME = %q, want the canonical envelope", EnvelopeMIME)
	}
}

func TestDoSendsContractHeaders(t *testing.T) {
	t.Parallel()

	var seenAccept, seenAuth, seenPath, seenQuery string
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAccept = r.Header.Get("Accept")
		seenAuth = r.Header.Get("Authorization")
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		writeResponse(t, w, `{"data":{}}`)
	}))
	t.Cleanup(srv2.Close)

	c := DefaultConfig(srv2.URL, "secret-key")
	client := NewClient(c)
	body, err := client.Do(context.Background(), "repositories", url.Values{"limit": {"5"}})
	if err != nil {
		t.Fatalf("Do() failed: %v", err)
	}
	if string(body) != `{"data":{}}` {
		t.Errorf("Do() body = %q, want the served payload", body)
	}
	if seenAccept != "application/eshu.envelope+json" {
		t.Errorf("Accept = %q, want the canonical envelope MIME", seenAccept)
	}
	if seenAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want the bearer credential", seenAuth)
	}
	if seenPath != "/api/v0/repositories" {
		t.Errorf("path = %q, want /api/v0/repositories", seenPath)
	}
	if seenQuery != "limit=5" {
		t.Errorf("query = %q, want limit=5", seenQuery)
	}
}

func TestDoToleratesTrailingSlashBaseURL(t *testing.T) {
	t.Parallel()

	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		writeResponse(t, w, `{}`)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(DefaultConfig(srv.URL+"/", "k"))
	if _, err := client.Do(context.Background(), "status", nil); err != nil {
		t.Fatalf("Do() failed: %v", err)
	}
	if seenPath != "/api/v0/status" {
		t.Errorf("path = %q, want /api/v0/status (no double slash)", seenPath)
	}
}

func TestDoTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig(srv.URL, "k")
	cfg.Timeout = 50 * time.Millisecond
	client := NewClient(cfg)
	_, err := client.Do(context.Background(), "slow", nil)
	if err == nil {
		t.Fatal("Do() succeeded against a hanging route, want a timeout error")
	}
}

func TestDoRejectsOverBudgetBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, strings.Repeat("x", DefaultResponseByteBudget+100))
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig(srv.URL, "k")
	client := NewClient(cfg)
	_, err := client.Do(context.Background(), "big", nil)
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Do() error = %v, want ErrOverBudget", err)
	}
}

func TestDoMapsNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeResponse(t, w, `{"error_code":"forbidden"}`)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(DefaultConfig(srv.URL, "k"))
	_, err := client.Do(context.Background(), "denied", nil)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("Do() error type = %T, want *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusError.StatusCode = %d, want 403", statusErr.StatusCode)
	}
	if got := statusErr.Error(); !strings.Contains(got, "403") || !strings.Contains(got, "denied") {
		t.Errorf("StatusError.Error() = %q, want status and route inside", got)
	}
}

func TestDoClipsLongErrorSnippet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		writeResponse(t, w, strings.Repeat("e", errorSnippetBytes+100))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(DefaultConfig(srv.URL, "k"))
	_, err := client.Do(context.Background(), "gateway", nil)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("Do() error type = %T, want *StatusError", err)
	}
	if len(statusErr.Snippet) != errorSnippetBytes {
		t.Errorf("len(Snippet) = %d, want the %d-byte clip", len(statusErr.Snippet), errorSnippetBytes)
	}
}

func TestDoReportsStatusWhenBodyBreaks(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server does not support hijacking")
			return
		}
		conn, buf, hijackErr := hijacker.Hijack()
		if hijackErr != nil {
			t.Errorf("hijack failed: %v", hijackErr)
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Errorf("close hijacked conn: %v", err)
			}
		}()
		writeResponse(t, buf, "HTTP/1.1 500 Internal Server Error\r\nContent-Length: 100\r\n\r\nshort")
		if err := buf.Flush(); err != nil {
			t.Errorf("flush hijacked response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(DefaultConfig(srv.URL, "k"))
	_, err := client.Do(context.Background(), "broken", nil)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("Do() error type = %T, want *StatusError (status wins over short body)", err)
	}
	if statusErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusError.StatusCode = %d, want 500", statusErr.StatusCode)
	}
}

func TestDoReadsFullBody(t *testing.T) {
	t.Parallel()

	const payload = `{"data":{"items":[]},"truth":{},"error":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, payload)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(DefaultConfig(srv.URL, "k"))
	body, err := client.Do(context.Background(), "packet", nil)
	if err != nil {
		t.Fatalf("Do() failed: %v", err)
	}
	if string(body) != payload {
		t.Errorf("Do() body = %q, want %q", body, payload)
	}
}
