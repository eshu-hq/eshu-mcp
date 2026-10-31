package mcpserver

// Edge OIDC verification, proving the door check the owner requires:
// when discovery is configured, a Bearer JWT must carry a known issuer,
// an audience matching the resource, a live expiry, and a signature from
// the issuer's JWKS. Anything else is refused at the edge before dispatch.

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

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

	principal, denial := v.Verify(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(time.Hour)))
	if denial != DenyNone {
		t.Fatalf("denial = %v, want none", denial)
	}
	if principal == "" {
		t.Fatal("empty principal for a valid token")
	}
	again, _ := v.Verify(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(time.Hour)))
	if again != principal {
		t.Fatalf("principal unstable: %q vs %q", principal, again)
	}
}

func TestVerifyWrongAudiencePostMatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	_, denial := v.Verify(mintToken(t, key, testKid, srv.URL, "http://someone-else/", time.Now().Add(time.Hour)))
	if denial != DenyPostMatch {
		t.Fatalf("denial = %v, want post-match", denial)
	}
}

func TestVerifyUnknownIssuerUnrecognized(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	other := testIssuerServer(t, testRSAKey(t), "other-key")
	v := testVerifier(t, srv)

	_, denial := v.Verify(mintToken(t, testRSAKey(t), "other-key", other.URL, testResource, time.Now().Add(time.Hour)))
	if denial != DenyUnrecognized {
		t.Fatalf("denial = %v, want unrecognized", denial)
	}
}

func TestVerifyMalformedUnrecognized(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	for _, header := range []string{"Bearer not.a.jwt", "Bearer ", "Bearer " + strings.Repeat("x", 40)} {
		if _, denial := v.Verify(header); denial != DenyUnrecognized {
			t.Fatalf("%q: denial = %v, want unrecognized", header, denial)
		}
	}
}

func TestVerifyNonBearerPassesThrough(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	if _, denial := v.Verify("Token opaque-value"); denial != DenyNone {
		t.Fatalf("denial = %v, want none (legacy forward)", denial)
	}
}

func TestVerifyExpiredPostMatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	_, denial := v.Verify(mintToken(t, key, testKid, srv.URL, testResource, time.Now().Add(-time.Hour)))
	if denial != DenyPostMatch {
		t.Fatalf("denial = %v, want post-match", denial)
	}
}

func TestVerifyWrongKeySignaturePostMatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	_, denial := v.Verify(mintToken(t, testRSAKey(t), testKid, srv.URL, testResource, time.Now().Add(time.Hour)))
	if denial != DenyPostMatch {
		t.Fatalf("denial = %v, want post-match", denial)
	}
}

func TestVerifyNoneAlgRefused(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + srv.URL + `"}`))
	if _, denial := v.Verify("Bearer " + header + "." + payload + "."); denial == DenyNone {
		t.Fatal("unsigned token accepted")
	}
}

func TestVerifyUnknownKidRefetchThenDeny(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	_, denial := v.Verify(mintToken(t, key, "rotated-key", srv.URL, testResource, time.Now().Add(time.Hour)))
	if denial != DenyPostMatch {
		t.Fatalf("denial = %v, want post-match", denial)
	}
}

func TestVerifyIssuerDownFailClosed(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	iss := srv.URL
	srv.Close()

	v := NewTokenVerifier(testResource, []string{iss}, "")
	_, denial := v.Verify(mintToken(t, key, testKid, iss, testResource, time.Now().Add(time.Hour)))
	if denial == DenyNone {
		t.Fatal("unverifiable token accepted when the issuer is down")
	}
}

func TestVerifySharedKeyFastPath(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := NewTokenVerifier(testResource, []string{srv.URL}, "deployment-key")

	principal, denial := v.Verify("Bearer deployment-key")
	if denial != DenyNone || principal != "shared" {
		t.Fatalf("got (%q, %v), want (shared, none)", principal, denial)
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
