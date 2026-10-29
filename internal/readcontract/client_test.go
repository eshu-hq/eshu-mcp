package readcontract

import (
	"bytes"
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

// failCloseBody reads normally but reports a close error, proving DoRaw
// treats a body-close failure as best-effort: the bytes already read win.
type failCloseBody struct{ *bytes.Reader }

// Close reports a close failure for failCloseBody.
func (failCloseBody) Close() error { return errors.New("close boom") }

// roundTripFunc adapts a function to http.RoundTripper for transport stubs.
type roundTripFunc func(*http.Request) (*http.Response, error)

// mustClient builds a test client or fails: no test proceeds with a nil client.
func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

// RoundTrip runs the stubbed round trip.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestDoRawToleratesCloseError proves a body-close failure does not fail the
// call: the status and bytes already read are still returned.
func TestDoRawToleratesCloseError(t *testing.T) {
	t.Parallel()

	client := mustClient(t, DefaultConfig("https://core:8443", ""))
	client.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       failCloseBody{bytes.NewReader([]byte(`{"data":{}}`))},
		}, nil
	})}
	status, raw, err := client.DoRaw(context.Background(), RawRequest{Method: "GET", Path: "/api/v0/repos"}, "Bearer [REDACTED]")
	if err != nil {
		t.Fatalf("DoRaw() error = %v, want nil despite the close failure", err)
	}
	if status != http.StatusOK || string(raw) != `{"data":{}}` {
		t.Errorf("DoRaw() = (%d, %s), want (200, {\"data\":{}})", status, raw)
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
	client := mustClient(t, c)
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

	client := mustClient(t, DefaultConfig(srv.URL+"/", "k"))
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
	client := mustClient(t, cfg)
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
	client := mustClient(t, cfg)
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

	client := mustClient(t, DefaultConfig(srv.URL, "k"))
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

	client := mustClient(t, DefaultConfig(srv.URL, "k"))
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

	client := mustClient(t, DefaultConfig(srv.URL, "k"))
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

	client := mustClient(t, DefaultConfig(srv.URL, "k"))
	body, err := client.Do(context.Background(), "packet", nil)
	if err != nil {
		t.Fatalf("Do() failed: %v", err)
	}
	if string(body) != payload {
		t.Errorf("Do() body = %q, want %q", body, payload)
	}
}

func TestDoRawForwardsCredentialVerbatim(t *testing.T) {
	t.Parallel()

	var seenAuth, seenAccept, seenCT, seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenAccept = r.Header.Get("Accept")
		seenCT = r.Header.Get("Content-Type")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		seenBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"data":{},"truth":null,"error":null}`)); err != nil {
			return
		}
	}))
	defer srv.Close()

	client := mustClient(t, DefaultConfig(srv.URL, ""))
	status, body, err := client.DoRaw(context.Background(), RawRequest{
		Method: http.MethodPost,
		Path:   "/api/v0/repos/r/files/read",
		Query:  map[string]string{"limit": "10"},
		Body:   map[string]any{"path": "go.mod"},
	}, "Bearer scoped-token-abc")
	if err != nil {
		t.Fatalf("DoRaw() error = %v, want nil", err)
	}
	if status != http.StatusOK {
		t.Errorf("DoRaw() status = %d, want 200", status)
	}
	if !strings.Contains(string(body), `"data"`) {
		t.Errorf("DoRaw() body = %q, want the raw envelope bytes", body)
	}
	if seenAuth != "Bearer scoped-token-abc" {
		t.Errorf("Authorization = %q, want the caller credential verbatim", seenAuth)
	}
	if seenAccept != "application/eshu.envelope+json" {
		t.Errorf("Accept = %q, want the envelope MIME", seenAccept)
	}
	if seenCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", seenCT)
	}
	if seenBody != `{"path":"go.mod"}` {
		t.Errorf("body = %q, want the JSON request", seenBody)
	}
}

func TestDoRawDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	client := mustClient(t, DefaultConfig(srv.URL, ""))
	status, _, err := client.DoRaw(context.Background(), RawRequest{
		Method: http.MethodGet,
		Path:   "/api/v0/repos//summary",
	}, "Bearer k")
	if err != nil {
		t.Fatalf("DoRaw() error = %v, want nil", err)
	}
	if status != http.StatusTemporaryRedirect {
		t.Errorf("DoRaw() status = %d, want the un-followed 307", status)
	}
}

// TestNewClientRejectsEmptyBaseURL proves a client without an origin fails
// at construction instead of failing every request it attempts.
func TestNewClientRejectsEmptyBaseURL(t *testing.T) {
	t.Parallel()

	if _, err := NewClient(DefaultConfig("", "k")); err == nil {
		t.Fatal("NewClient(\"\") = nil, want the empty-BaseURL error")
	}
}

// TestNewClientDefaultsZeroGuards proves a zero Config cannot silently
// disable the call timeout or the body budget.
func TestNewClientDefaultsZeroGuards(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{BaseURL: "https://core:8443"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want DefaultTimeout %v", client.cfg.Timeout, DefaultTimeout)
	}
	if client.cfg.ResponseByteBudget != DefaultResponseByteBudget {
		t.Errorf("ResponseByteBudget = %d, want %d", client.cfg.ResponseByteBudget, DefaultResponseByteBudget)
	}
}

// TestDoRawRejectsOverBudgetBody proves the live dispatch path caps the
// wire read at the configured budget instead of buffering an unbounded body.
func TestDoRawRejectsOverBudgetBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponse(t, w, `{"data":"`+strings.Repeat("x", 1024)+`"}`)
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig(srv.URL, "")
	cfg.ResponseByteBudget = 16
	client := mustClient(t, cfg)
	if _, _, err := client.DoRaw(context.Background(), RawRequest{Method: "GET", Path: "/api/v0/repos"}, ""); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("DoRaw() error = %v, want ErrOverBudget", err)
	}
}
