package mcpserver

// Denial outcome vocabulary and log shape: the strings the E2E leakage
// matrix scrapes, byte-identical to the monolith resolver's.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

// TestOutcomeConstantsMatchMonolith pins byte-identity with the monolith
// resolver's vocabulary (go/internal/oidcbearer/telemetry.go): renaming a
// constant must break this test, not just the E2E scrape.
func TestOutcomeConstantsMatchMonolith(t *testing.T) {
	t.Parallel()

	for constant, literal := range map[string]string{
		outcomeUnknownIssuer:    "unknown_issuer",
		outcomeWrongAudience:    "wrong_audience",
		outcomeExpired:          "expired",
		outcomeBadSignature:     "bad_signature",
		outcomeMalformed:        "malformed",
		outcomeJWKSFetchFailure: "jwks_fetch_failure",
	} {
		if constant != literal {
			t.Fatalf("outcome constant %q != monolith literal %q", constant, literal)
		}
	}
}

// TestVerifyOutcomeVocabulary pins every denial-outcome mapping against
// literal strings, so a renamed constant breaks here and not in E2E.
func TestVerifyOutcomeVocabulary(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	live := time.Now()
	mintClaims := func(mut func(jwt.MapClaims)) string {
		claims := jwt.MapClaims{
			"iss": srv.URL, "aud": testResource, "sub": "e2e-member",
			"iat": live.Add(-time.Minute).Unix(), "exp": live.Add(time.Hour).Unix(),
		}
		mut(claims)
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = testKid
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return "Bearer " + raw
	}
	down := testIssuerServer(t, testRSAKey(t), "down-key")
	downIss := down.URL
	down.Close()
	// The down issuer stays trusted: classification passes it and the
	// failure surfaces at crypto/fetch time, not as unknown_issuer.
	v := NewTokenVerifier(testResource, []string{srv.URL, downIss}, "")
	downHeader := mintToken(t, key, testKid, downIss, testResource, live.Add(time.Hour))
	unsigned := func() string {
		rawHeader, err := json.Marshal(map[string]any{"alg": "none"})
		if err != nil {
			t.Fatalf("marshal header: %v", err)
		}
		rawClaims, err := json.Marshal(map[string]any{
			"iss": srv.URL, "aud": testResource, "sub": "s", "exp": live.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("marshal claims: %v", err)
		}
		return "Bearer " + base64.RawURLEncoding.EncodeToString(rawHeader) + "." +
			base64.RawURLEncoding.EncodeToString(rawClaims) + "."
	}
	cases := []struct {
		name    string
		header  string
		outcome string
		iss     string
		denial  DenialKind
	}{
		{"wrong audience", mintToken(t, key, testKid, srv.URL, "http://someone-else/", live.Add(time.Hour)), "wrong_audience", srv.URL, DenyPostMatch},
		{"unknown issuer", mintToken(t, testRSAKey(t), "other", "http://unknown/", testResource, live.Add(time.Hour)), "unknown_issuer", "http://unknown/", DenyUnrecognized},
		{"malformed", "Bearer not.a.jwt", "malformed", "", DenyUnrecognized},
		{"expired", mintToken(t, key, testKid, srv.URL, testResource, live.Add(-time.Hour)), "expired", srv.URL, DenyPostMatch},
		{"future nbf", mintClaims(func(c jwt.MapClaims) { c["nbf"] = live.Add(time.Hour).Unix() }), "malformed", srv.URL, DenyPostMatch},
		{"empty sub", mintClaims(func(c jwt.MapClaims) { delete(c, "sub") }), "malformed", srv.URL, DenyPostMatch},
		{"issuer down", downHeader, "jwks_fetch_failure", downIss, DenyPostMatch},
		{"bad signature", mintToken(t, testRSAKey(t), testKid, srv.URL, testResource, live.Add(time.Hour)), "bad_signature", srv.URL, DenyPostMatch},
		{"unsigned full claims", unsigned(), "bad_signature", srv.URL, DenyPostMatch},
	}
	for _, tc := range cases {
		result := v.Verify(tc.header)
		if result.Denial != tc.denial || result.Outcome != tc.outcome || result.Issuer != tc.iss {
			t.Fatalf("%s: got (%v, %q, %q), want (%v, %q, %q)",
				tc.name, result.Denial, result.Outcome, result.Issuer, tc.denial, tc.outcome, tc.iss)
		}
	}
}

// TestMiddlewareDenialLogShape proves the edge log line the E2E leakage
// matrix scrapes: message plus iss plus outcome as one JSON object, parsed
// the same way the matrix parser parses it.
func TestMiddlewareDenialLogShape(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)
	discovery := DiscoveryConfig{Resource: testResource, Issuers: []string{srv.URL}, MetadataURL: testResource + "/.well-known/oauth-protected-resource"}

	var buf lockedBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := EdgeAuthMiddlewareWithVerifier("", discovery, v, logger)(next)

	serve := func(header string) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	serve(mintToken(t, key, testKid, srv.URL, "http://someone-else/", time.Now().Add(time.Hour)))
	serve(mintToken(t, testRSAKey(t), "other", "http://unknown/", testResource, time.Now().Add(time.Hour)))
	serve("Bearer not.a.jwt")

	outcomes := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, "{")
		if idx == -1 || !strings.Contains(line, "oidc bearer token denied") {
			continue
		}
		var parsed struct {
			Message string `json:"message"`
			Msg     string `json:"msg"`
			Iss     string `json:"iss"`
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal([]byte(line[idx:]), &parsed); err != nil {
			t.Fatalf("denial line not JSON: %v", err)
		}
		msg := parsed.Message
		if msg == "" {
			msg = parsed.Msg
		}
		if msg != "oidc bearer token denied" {
			t.Fatalf("message = %q", msg)
		}
		outcomes[parsed.Outcome] = parsed.Iss
	}
	for outcome, iss := range map[string]string{
		outcomeWrongAudience: srv.URL, outcomeUnknownIssuer: "http://unknown/", outcomeMalformed: "",
	} {
		if got, ok := outcomes[outcome]; !ok || got != iss {
			t.Fatalf("outcome %q: got iss %q, outcomes %v", outcome, got, outcomes)
		}
	}
}
