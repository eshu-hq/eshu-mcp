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

func TestVerifyOutcomeVocabulary(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)
	full := func(claims map[string]any, header map[string]any) string {
		rawHeader, err := json.Marshal(header)
		if err != nil {
			t.Fatalf("marshal header: %v", err)
		}
		rawClaims, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal claims: %v", err)
		}
		return "Bearer " + base64.RawURLEncoding.EncodeToString(rawHeader) + "." +
			base64.RawURLEncoding.EncodeToString(rawClaims) + "."
	}
	live := time.Now().Add(time.Hour).Unix()
	cases := []struct {
		name    string
		header  string
		outcome string
		iss     string
		denial  DenialKind
	}{
		{"wrong audience", mintToken(t, key, testKid, srv.URL, "http://someone-else/", time.Now().Add(time.Hour)), outcomeWrongAudience, srv.URL, DenyPostMatch},
		{"unknown issuer", mintToken(t, testRSAKey(t), "other", "http://unknown/", testResource, time.Now().Add(time.Hour)), outcomeUnknownIssuer, "http://unknown/", DenyUnrecognized},
		{"malformed", "Bearer not.a.jwt", outcomeMalformed, "", DenyUnrecognized},
		{"expired", mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(-time.Hour)), outcomeExpired, srv.URL, DenyPostMatch},
		{"bad signature", mintToken(t, testRSAKey(t), testKid, srv.URL, testResource, time.Now().Add(time.Hour)), outcomeBadSignature, srv.URL, DenyPostMatch},
		{"unsigned full claims", full(map[string]any{"iss": srv.URL, "aud": testResource, "sub": "s", "exp": live}, map[string]any{"alg": "none"}), outcomeBadSignature, srv.URL, DenyPostMatch},
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
		if idx == -1 || !strings.Contains(line, "oidc Bearer [REDACTED]") {
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
		if msg != "oidc Bearer [REDACTED]" {
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
