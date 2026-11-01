package mcpserver

import (
	"context"
	"errors"
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

// fakeProber stands in for core's admission probe: it answers per
// Authorization header and counts every call, so a test can prove both the
// decision and how many core round trips it cost.
type fakeProber struct {
	mu        sync.Mutex
	calls     int
	seen      []string
	status    int
	challenge string
	err       error
}

func (f *fakeProber) ProbeAdmission(_ context.Context, authHeader string) (int, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.seen = append(f.seen, authHeader)
	return f.status, f.challenge, f.err
}

func (f *fakeProber) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// admissionServer builds the real MCP server behind the admission
// middleware so a test exercises every locally served method through the
// production wiring, not a stub handler.
func admissionServer(t testing.TB, sharedKey string, verifier *TokenVerifier, admission *Admission) http.Handler {
	t.Helper()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	noDispatch := func(context.Context, string, map[string]any, string) (any, error) {
		return nil, errors.New("dispatch must not run in an admission test")
	}
	mcp := NewServer(registry.Definitions(), noDispatch, quiet, "eshu-mcp-test", "test",
		WithTransportAuth(EdgeAuthMiddlewareWithAdmission(sharedKey, DiscoveryConfig{}, verifier, admission, quiet)))
	return mcp.Handler(nil)
}

func newTestAdmission(prober AdmissionProber) *Admission {
	return NewAdmission(prober, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func rpc(method string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{}}`
}

func do(h http.Handler, method, path, body, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAdmissionRefusesForwardedCredentialCoreRejects is the #50 regression:
// a credential core rejects, in any scheme, must not reach initialize,
// tools/list, ping, or the SSE session mint.
func TestAdmissionRefusesForwardedCredentialCoreRejects(t *testing.T) {
	t.Parallel()

	for _, auth := range []string{"Bearer x", "Bearer not-a-jwt", "Basic x", "Token opaque"} {
		t.Run(auth, func(t *testing.T) {
			t.Parallel()

			prober := &fakeProber{status: http.StatusUnauthorized, challenge: `Bearer realm="core"`}
			h := admissionServer(t, "", nil, newTestAdmission(prober))

			for _, method := range []string{"initialize", "tools/list", "ping"} {
				rec := do(h, http.MethodPost, "/mcp/message", rpc(method), auth)
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("%s with %q: status = %d, want 401; body=%s", method, auth, rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="core"` {
					t.Errorf("%s: WWW-Authenticate = %q, want core's challenge relayed", method, got)
				}
			}
			// A bounded request context: if admission were skipped the SSE
			// stream would open and never end, so the deadline turns that
			// failure into a clean assertion instead of a hang.
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
			req.Header.Set("Authorization", auth)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("GET /sse with %q: status = %d, want 401 (no session minted)", auth, rec.Code)
			}
		})
	}
}

// TestAdmissionAdmitsCredentialCoreAccepts proves the fix for #48 survives:
// a credential core admits is served, and the probe carried it verbatim.
func TestAdmissionAdmitsCredentialCoreAccepts(t *testing.T) {
	t.Parallel()

	prober := &fakeProber{status: http.StatusOK}
	h := admissionServer(t, "", nil, newTestAdmission(prober))
	auth := "Bearer " + strings.Repeat("a", 43)

	for _, method := range []string{"initialize", "tools/list", "ping"} {
		rec := do(h, http.MethodPost, "/mcp/message", rpc(method), auth)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200; body=%s", method, rec.Code, rec.Body.String())
		}
	}
	for _, seen := range prober.seen {
		if seen != auth {
			t.Errorf("probe carried %q, want the caller credential verbatim %q", seen, auth)
		}
	}
}

func TestAdmissionRelaysCoreForbidden(t *testing.T) {
	t.Parallel()

	prober := &fakeProber{status: http.StatusForbidden}
	h := admissionServer(t, "", nil, newTestAdmission(prober))

	rec := do(h, http.MethodPost, "/mcp/message", rpc("tools/list"), "Bearer scoped-but-revoked")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want core's 403 relayed", rec.Code)
	}
}

// TestAdmissionFailsClosedWhenCoreUnavailable proves an unanswered or
// unusable probe never admits: a transport error and every non-admit,
// non-deny status answer a retryable 503.
func TestAdmissionFailsClosedWhenCoreUnavailable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		err    error
	}{
		{"transport error", 0, errors.New("dial tcp: connection refused")},
		{"core 500", http.StatusInternalServerError, nil},
		{"core 503", http.StatusServiceUnavailable, nil},
		{"core 429", http.StatusTooManyRequests, nil},
		{"route missing", http.StatusNotFound, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prober := &fakeProber{status: tc.status, err: tc.err}
			h := admissionServer(t, "", nil, newTestAdmission(prober))

			rec := do(h, http.MethodPost, "/mcp/message", rpc("tools/list"), "Bearer valid-looking")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 fail-closed; body=%s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Error("Retry-After missing on a retryable 503")
			}
			if strings.Contains(rec.Body.String(), `"tools"`) {
				t.Errorf("response served the catalog while core was unavailable: %s", rec.Body.String())
			}
		})
	}
}

// TestAdmissionCachesDecisions proves the cache bounds core load: a repeated
// credential costs one probe per TTL, a denial is cached for a shorter time
// than an admit, and an unavailable answer is never cached.
func TestAdmissionCachesDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	prober := &fakeProber{status: http.StatusUnauthorized}
	admission := newTestAdmission(prober)
	admission.now = func() time.Time { return now }
	h := admissionServer(t, "", nil, admission)

	for range 5 {
		do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer garbage")
	}
	if got := prober.callCount(); got != 1 {
		t.Fatalf("5 identical garbage requests cost %d probes, want 1 (negative cache)", got)
	}

	now = now.Add(admission.negativeTTL + time.Second)
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer garbage")
	if got := prober.callCount(); got != 2 {
		t.Fatalf("after the negative TTL probes = %d, want 2", got)
	}

	prober.mu.Lock()
	prober.status = http.StatusOK
	prober.mu.Unlock()
	for range 3 {
		do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer good")
	}
	if got := prober.callCount(); got != 3 {
		t.Fatalf("3 identical admitted requests brought probes to %d, want 3 (positive cache)", got)
	}
	now = now.Add(admission.positiveTTL + time.Second)
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer good")
	if got := prober.callCount(); got != 4 {
		t.Fatalf("after the positive TTL probes = %d, want 4", got)
	}

	prober.mu.Lock()
	prober.status = http.StatusInternalServerError
	prober.mu.Unlock()
	before := prober.callCount()
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer flaky")
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer flaky")
	if got := prober.callCount() - before; got != 2 {
		t.Fatalf("2 requests while core was unavailable cost %d probes, want 2 (unavailable is never cached)", got)
	}
}

// TestAdmissionCacheStaysBounded proves a flood of distinct credentials
// cannot grow the cache without limit.
func TestAdmissionCacheStaysBounded(t *testing.T) {
	t.Parallel()

	prober := &fakeProber{status: http.StatusUnauthorized}
	admission := newTestAdmission(prober)
	admission.maxEntries = 8
	h := admissionServer(t, "", nil, admission)

	for i := range 200 {
		do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer garbage-"+strings.Repeat("z", i%7)+string(rune('a'+i%26)))
		do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Basic distinct-"+strings.Repeat("q", i))
	}
	if got := admission.cacheLen(); got > 8 {
		t.Fatalf("cache holds %d entries, want at most 8", got)
	}
}

// TestAdmissionSkipsPrincipalsTheEdgeAlreadyProved proves the shared key and
// an edge-verified JWT never cost a core round trip.
func TestAdmissionSkipsPrincipalsTheEdgeAlreadyProved(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	verifier := NewTokenVerifier(testResource, []string{srv.URL}, "deployment-key")
	prober := &fakeProber{status: http.StatusUnauthorized}
	h := admissionServer(t, "deployment-key", verifier, newTestAdmission(prober))

	jwt := mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(time.Hour))
	for name, auth := range map[string]string{"shared key": "Bearer deployment-key", "verified JWT": jwt} {
		rec := do(h, http.MethodPost, "/mcp/message", rpc("tools/list"), auth)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200; body=%s", name, rec.Code, rec.Body.String())
		}
	}
	if got := prober.callCount(); got != 0 {
		t.Fatalf("shared key and verified JWT cost %d probes, want 0", got)
	}
}

// TestAdmissionStillRefusesHeaderlessAndBadJWT proves the earlier door
// checks are untouched: headerless and a dotted JWT attempt are refused
// before any probe.
func TestAdmissionStillRefusesHeaderlessAndBadJWT(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	verifier := NewTokenVerifier(testResource, []string{srv.URL}, "")
	prober := &fakeProber{status: http.StatusOK}
	h := admissionServer(t, "", verifier, newTestAdmission(prober))

	if rec := do(h, http.MethodPost, "/mcp/message", rpc("ping"), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("headerless status = %d, want 401", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer aaaa.bbbb.cccc"); rec.Code != http.StatusUnauthorized {
		t.Errorf("malformed JWT status = %d, want 401", rec.Code)
	}
	if got := prober.callCount(); got != 0 {
		t.Fatalf("door denials cost %d probes, want 0", got)
	}
}

// TestAdmissionStatsCountEveryOutcome proves the operator-facing counters
// separate "core denied" from "core unavailable" and count cache hits.
func TestAdmissionStatsCountEveryOutcome(t *testing.T) {
	t.Parallel()

	prober := &fakeProber{status: http.StatusOK}
	admission := newTestAdmission(prober)
	h := admissionServer(t, "", nil, admission)

	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer a") // admitted
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer a") // cache hit
	prober.mu.Lock()
	prober.status = http.StatusUnauthorized
	prober.mu.Unlock()
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer b") // denied
	prober.mu.Lock()
	prober.status = http.StatusBadGateway
	prober.mu.Unlock()
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer c") // unavailable

	got := admission.Stats()
	want := AdmissionStats{Admitted: 1, Denied: 1, Unavailable: 1, CacheHit: 1}
	if got != want {
		t.Fatalf("Stats() = %+v, want %+v", got, want)
	}
}

// TestAdmissionFullCacheStillBoundsProbeCost proves a full cache keeps the
// one-probe-per-TTL bound: a new credential evicts the entry closest to
// expiry instead of going uncached, so a repeated credential never probes on
// every request just because the cache filled up.
func TestAdmissionFullCacheStillBoundsProbeCost(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	prober := &fakeProber{status: http.StatusUnauthorized}
	admission := newTestAdmission(prober)
	admission.now = func() time.Time { return now }
	admission.maxEntries = 3
	h := admissionServer(t, "", nil, admission)

	for _, cred := range []string{"Bearer a", "Bearer b", "Bearer c"} {
		do(h, http.MethodPost, "/mcp/message", rpc("ping"), cred)
		now = now.Add(time.Second)
	}
	before := prober.callCount()
	for range 5 {
		do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer d")
	}
	if got := prober.callCount() - before; got != 1 {
		t.Fatalf("5 repeats of a credential arriving at a full cache cost %d probes, want 1", got)
	}
	if got := admission.cacheLen(); got > 3 {
		t.Fatalf("cache holds %d entries, want at most 3", got)
	}
	// The oldest entry was the one evicted; the newest two are still served
	// from the cache.
	before = prober.callCount()
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer c")
	do(h, http.MethodPost, "/mcp/message", rpc("ping"), "Bearer a")
	if got := prober.callCount() - before; got != 1 {
		t.Fatalf("revisiting c (cached) and a (evicted) cost %d probes, want exactly 1 for the evicted one", got)
	}
}
