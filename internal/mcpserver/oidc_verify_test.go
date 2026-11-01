package mcpserver

// Edge OIDC verification, proving the door check the owner requires:
// when discovery is configured, a Bearer JWT must carry a known issuer,
// an audience matching the resource, a live expiry, and a signature from
// the issuer's JWKS. Anything else is refused at the edge before dispatch.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// lockedBuffer is a goroutine-safe slog sink for asserting log shape.
func hmacSHA256(secret, unsigned string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		panic(err)
	}
	return mac.Sum(nil)
}

const (
	testResource = "http://127.0.0.1:29081"
	testKid      = "test-key-1"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func jwksDoc(t *testing.T, pub *rsa.PublicKey, kid string) string {
	t.Helper()

	raw, err := json.Marshal(map[string]any{"keys": []any{map[string]any{
		"kty": "RSA",
		"use": "sig",
		"kid": kid,
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	return string(raw)
}

// testIssuerServer serves the OIDC discovery + JWKS surface the verifier
// consumes, mirroring the mock IdP shape (issuer doc points at /jwks).
func testIssuerServer(t *testing.T, key *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issuer":   srv.URL,
			"jwks_uri": srv.URL + "/jwks",
		}); err != nil {
			t.Errorf("encode discovery doc: %v", err)
		}
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(jwksDoc(t, &key.PublicKey, kid))); err != nil {
			t.Errorf("write jwks doc: %v", err)
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func mintToken(t *testing.T, key *rsa.PrivateKey, kid, iss, aud string, exp time.Time) string {
	t.Helper()

	claims := jwt.MapClaims{"iss": iss, "sub": "e2e-member", "iat": time.Now().Add(-time.Minute).Unix()}
	if aud != "" {
		claims["aud"] = aud
	}
	if !exp.IsZero() {
		claims["exp"] = exp.Unix()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return "Bearer " + raw
}

func testVerifier(t *testing.T, srv *httptest.Server) *TokenVerifier {
	t.Helper()

	return NewTokenVerifier(testResource, []string{srv.URL}, "")
}

func TestVerifyValidTokenPasses(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	result := v.Verify(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(time.Hour)))
	if result.Denial != DenyNone {
		t.Fatalf("result.Denial = %v, want none", result.Denial)
	}
	if result.Principal == "" {
		t.Fatal("empty result.Principal for a valid token")
	}
	againResult := v.Verify(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(time.Hour)))
	if againResult.Principal != result.Principal {
		t.Fatalf("result.Principal unstable: %q vs %q", result.Principal, againResult.Principal)
	}
}

func TestVerifyWrongAudiencePostMatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	result := v.Verify(mintToken(t, key, testKid, srv.URL, "http://someone-else/", time.Now().Add(time.Hour)))
	if result.Denial != DenyPostMatch {
		t.Fatalf("result.Denial = %v, want post-match", result.Denial)
	}
}

func TestVerifyUnknownIssuerUnrecognized(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	other := testIssuerServer(t, testRSAKey(t), "other-key")
	v := testVerifier(t, srv)

	result := v.Verify(mintToken(t, testRSAKey(t), "other-key", other.URL, testResource, time.Now().Add(time.Hour)))
	if result.Denial != DenyUnrecognized {
		t.Fatalf("result.Denial = %v, want unrecognized", result.Denial)
	}
}

func TestVerifyMalformedUnrecognized(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	// Only JWT-shaped-but-unparsable input denies as malformed here: an
	// empty Bearer [REDACTED] a present scheme with no credential, and
	// "aaaa.bbbb.cccc" (the E2E malformed probe's exact input). Opaque
	// non-shaped credentials pass through (see
	// TestVerifyOpaqueBearerPassesThrough).
	for _, header := range []string{"Bearer ", "Bearer aaaa.bbbb.cccc", "Bearer x.y.z"} {
		result := v.Verify(header)
		if result.Denial != DenyUnrecognized {
			t.Fatalf("%q: result.Denial = %v, want unrecognized", header, result.Denial)
		}
		if result.Outcome != outcomeMalformed {
			t.Fatalf("%q: result.Outcome = %q, want %q", header, result.Outcome, outcomeMalformed)
		}
	}
}

func TestVerifyNonBearerPassesThrough(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	if result := v.Verify("Token opaque-value"); result.Denial != DenyNone {
		t.Fatalf("result.Denial = %v, want none (legacy forward)", result.Denial)
	}
}

func TestVerifyExpiredPostMatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	result := v.Verify(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(-time.Hour)))
	if result.Denial != DenyPostMatch {
		t.Fatalf("result.Denial = %v, want post-match", result.Denial)
	}
}

func TestVerifyWrongKeySignaturePostMatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	result := v.Verify(mintToken(t, testRSAKey(t), testKid, srv.URL, testResource, time.Now().Add(time.Hour)))
	if result.Denial != DenyPostMatch {
		t.Fatalf("result.Denial = %v, want post-match", result.Denial)
	}
}

func TestVerifyNoneAlgRefused(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + srv.URL + `"}`))
	if result := v.Verify("Bearer " + header + "." + payload + "."); result.Denial == DenyNone {
		t.Fatal("unsigned token accepted")
	}
}

func TestVerifyIssuerDownFailClosed(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	iss := srv.URL
	srv.Close()

	v := NewTokenVerifier(testResource, []string{iss}, "")
	result := v.Verify(mintToken(t, key, testKid, iss, testResource, time.Now().Add(time.Hour)))
	if result.Denial == DenyNone {
		t.Fatal("unverifiable token accepted when the issuer is down")
	}
}

// TestVerifySingleflightFollowerFetchFailure pins the follower branch of
// the fetch/signature split: when the test goroutine holds fetch leadership
// for a down issuer, Verify must join as a follower and report
// jwks_fetch_failure (never bad_signature) from the shared call's error.
// The server stays closed throughout, so even a Verify that arrives after
// the shared call completes refetches against the dead issuer and lands in
// the same bucket — the assertion holds regardless of goroutine timing.
func TestVerifySingleflightFollowerFetchFailure(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	iss := srv.URL
	srv.Close()

	v := NewTokenVerifier(testResource, []string{iss}, "")

	v.mu.Lock()
	call, leader := v.joinFetchLocked(iss)
	v.mu.Unlock()
	if !leader {
		t.Fatal("expected the test to seize fetch leadership")
	}

	// mintToken already prefixes "Bearer " (unlike a raw SignedString).
	raw := mintToken(t, key, "ghost-kid", iss, testResource, time.Now().Add(time.Hour))
	done := make(chan VerifyResult, 1)
	go func() { done <- v.Verify(raw) }()

	time.Sleep(500 * time.Millisecond)
	v.mu.Lock()
	v.finishFetchLocked(iss, call, errors.New("boom"))
	v.mu.Unlock()

	select {
	case result := <-done:
		if result.Denial != DenyPostMatch || result.Outcome != "jwks_fetch_failure" || result.Issuer != iss {
			t.Fatalf("got (%v, %q, %q), want (post-match, jwks_fetch_failure, %q)",
				result.Denial, result.Outcome, result.Issuer, iss)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Verify did not return after the shared fetch failed")
	}
}

func TestVerifySharedKeyFastPath(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := NewTokenVerifier(testResource, []string{srv.URL}, "deployment-key")

	result := v.Verify("Bearer deployment-key")
	if result.Denial != DenyNone || result.Principal != "shared" {
		t.Fatalf("got (%q, %v), want (shared, none)", result.Principal, result.Denial)
	}
}

func TestMiddlewareChallengeSelection(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)
	discovery := DiscoveryConfig{Resource: testResource, Issuers: []string{srv.URL}, MetadataURL: testResource + "/.well-known/oauth-protected-resource"}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := EdgeAuthMiddlewareWithVerifier("", discovery, v, nil)(next)

	serve := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(mintToken(t, key, testKid, srv.URL, "http://someone-else/", time.Now().Add(time.Hour))); rec.Code != http.StatusUnauthorized ||
		rec.Header().Get("WWW-Authenticate") != BareChallenge {
		t.Fatalf("wrong-audience: code=%d challenge=%q, want 401 + bare", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	if rec := serve(mintToken(t, testRSAKey(t), "other", "http://unknown-issuer/", testResource, time.Now().Add(time.Hour))); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("unknown-issuer: code=%d challenge=%q, want 401 + discovery", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	if rec := serve(""); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("headerless: code=%d challenge=%q, want 401 + discovery", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	if rec := serve(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(time.Hour))); rec.Code != http.StatusOK {
		t.Fatalf("valid: code=%d, want 200", rec.Code)
	}
}

func TestVerifySchemeCaseInsensitive(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	expired := mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(-time.Hour))
	if result := v.Verify("bearer " + strings.TrimPrefix(expired, "Bearer ")); result.Denial == DenyNone {
		t.Fatal("lowercase bearer expired token admitted")
	}
	wrongAud := mintToken(t, key, testKid, srv.URL, "http://someone-else/", time.Now().Add(time.Hour))
	if result := v.Verify("BEARER " + strings.TrimPrefix(wrongAud, "Bearer ")); result.Denial != DenyPostMatch {
		t.Fatalf("uppercase bearer wrong-audience: result.Denial = %v, want post-match", result.Denial)
	}
	// Dotted malformed input keeps the verification path whatever the
	// scheme case; a dotless opaque credential forwards under a lowercase
	// scheme exactly like its canonical-case twin.
	if result := v.Verify("bearer aaaa.bbbb.cccc"); result.Denial != DenyUnrecognized {
		t.Fatalf("lowercase bearer malformed JWT: result.Denial = %v, want unrecognized", result.Denial)
	}
	if result := v.Verify("bearer garbage"); result.Denial != DenyNone {
		t.Fatalf("lowercase bearer opaque credential denied: %v", result.Denial)
	}
}

func TestVerifyNoneAlgFullClaimsRefused(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	fullClaims := map[string]any{
		"iss": srv.URL, "aud": testResource, "sub": "e2e-member",
		"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	encode := func(header map[string]any, claims map[string]any, secret string) string {
		rawHeader, err := json.Marshal(header)
		if err != nil {
			t.Fatalf("marshal header: %v", err)
		}
		rawClaims, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal claims: %v", err)
		}
		unsigned := base64.RawURLEncoding.EncodeToString(rawHeader) + "." + base64.RawURLEncoding.EncodeToString(rawClaims)
		if secret == "" {
			return "Bearer " + unsigned + "."
		}
		mac := hmacSHA256(secret, unsigned)
		return "Bearer " + unsigned + "." + base64.RawURLEncoding.EncodeToString(mac)
	}
	// Unsigned, but otherwise fully valid: must die on crypto, not on shape.
	if result := v.Verify(encode(map[string]any{"alg": "none", "typ": "JWT"}, fullClaims, "")); result.Denial == DenyNone {
		t.Fatal("unsigned full-claims token admitted")
	}
	// Symmetric signature under an unknown secret: only RS256 is trusted.
	if result := v.Verify(encode(map[string]any{"alg": "HS256", "typ": "JWT", "kid": testKid}, fullClaims, "attacker-secret")); result.Denial == DenyNone {
		t.Fatal("HS256 token admitted")
	}
}

func TestVerifyEmptySubDenied(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	claims := jwt.MapClaims{
		"iss": srv.URL, "aud": testResource,
		"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = testKid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	result := v.Verify("Bearer " + raw)
	if result.Denial != DenyPostMatch || result.Outcome != "malformed" || result.Issuer != srv.URL {
		t.Fatalf("result = (%v, %q, %q), want (post-match, malformed, %q)",
			result.Denial, result.Outcome, result.Issuer, srv.URL)
	}
}

func TestSameOrigin(t *testing.T) {
	t.Parallel()

	if !sameOrigin("http://mock-oidc-idp:8080", "http://mock-oidc-idp:8080/jwks") {
		t.Fatal("same origin rejected")
	}
	if sameOrigin("http://mock-oidc-idp:8080", "http://attacker:8080/jwks") {
		t.Fatal("foreign origin accepted")
	}
	if sameOrigin("http://mock-oidc-idp:8080", "https://mock-oidc-idp:8080/jwks") {
		t.Fatal("scheme change accepted")
	}
}

func TestHTTPNonLoopbackIssuer(t *testing.T) {
	t.Parallel()

	if !HTTPNonLoopbackIssuer("http://mock-oidc-idp:8080") {
		t.Fatal("http non-loopback issuer not flagged")
	}
	for _, ok := range []string{"https://idp.example.com", "http://127.0.0.1:8080", "http://localhost:8080", ""} {
		if HTTPNonLoopbackIssuer(ok) {
			t.Fatalf("%q flagged", ok)
		}
	}
}

// TestVerifyOutcomeVocabulary pins the denial outcome strings the E2E
// leakage matrix scrapes from the edge log: byte-identical to the
// monolith resolver's vocabulary (go/internal/oidcbearer/telemetry.go).
func TestMiddlewareNilVerifierKeepsLegacy(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := EdgeAuthMiddlewareWithVerifier("", DiscoveryConfig{}, nil, nil)(next)

	req := httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
	req.Header.Set("Authorization", "Bearer anything-at-all")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy pass-through broken: code=%d", rec.Code)
	}
}
