package mcpserver

import (
	"net/http"
	"strings"
	"testing"
)

// TestForwardedPrincipalIsCanonicalAcrossSpellings proves one opaque
// credential maps to one principal whatever its scheme case or inner
// spacing, in both the no-verifier path (edgeAuth) and the door-check path
// (Verify). Otherwise a single valid token could mint a fresh per-principal
// SSE allowance for each spelling and walk past the cap.
func TestForwardedPrincipalIsCanonicalAcrossSpellings(t *testing.T) {
	t.Parallel()

	verifier := NewTokenVerifier("", nil, "deploy-key")
	spellings := []string{
		"Bearer opaque-token",
		"bearer opaque-token",
		"BEARER opaque-token",
		"Bearer  opaque-token",
		"Bearer opaque-token ",
		"  Bearer opaque-token",
		// Core cuts at the first space and trims the scheme, so whitespace
		// glued to the scheme still reaches the same token there.
		"Bearer\t opaque-token",
		"Bearer\t\t opaque-token",
		"BEARER\u00a0 opaque-token",
		"bearer\v opaque-token",
	}
	want, ok := edgeAuth("deploy-key", spellings[0])
	if !ok || !strings.HasPrefix(want, "fwd:") {
		t.Fatalf("edgeAuth(%q) = (%q, %v), want a fwd principal", spellings[0], want, ok)
	}
	for _, header := range spellings {
		if got, _ := edgeAuth("deploy-key", header); got != want {
			t.Errorf("edgeAuth(%q) = %q, want %q", header, got, want)
		}
		if got := verifier.Verify(header).Principal; got != want {
			t.Errorf("Verify(%q) = %q, want %q", header, got, want)
		}
	}

	if other, _ := edgeAuth("deploy-key", "Bearer opaque-token2"); other == want {
		t.Error("distinct tokens share a principal")
	}
	// A different scheme with the same secret is a different credential.
	if basic, _ := edgeAuth("deploy-key", "Basic opaque-token"); basic == want {
		t.Error("Basic and Bearer share a principal")
	}
	lower, _ := edgeAuth("deploy-key", "basic Zm9vOmJhcg==")
	upper, _ := edgeAuth("deploy-key", "Basic Zm9vOmJhcg==")
	if lower != upper {
		t.Errorf("scheme case split a non-Bearer principal: %q vs %q", lower, upper)
	}
	// The secret itself stays case-sensitive.
	if a, _ := edgeAuth("deploy-key", "Bearer Token"); a == func() string { p, _ := edgeAuth("deploy-key", "Bearer token"); return p }() {
		t.Error("token case was folded; secrets are case-sensitive")
	}
}

// TestEdgeAuthSharedKeySchemeCaseMatchesVerify keeps the shared key's scheme
// matching identical on both auth paths.
func TestEdgeAuthSharedKeySchemeCaseMatchesVerify(t *testing.T) {
	t.Parallel()

	verifier := NewTokenVerifier("", nil, "deploy-key")
	for _, header := range []string{"Bearer deploy-key", "bearer deploy-key", "BEARER  deploy-key"} {
		edge, _ := edgeAuth("deploy-key", header)
		if edge != "shared" {
			t.Errorf("edgeAuth(%q) = %q, want shared", header, edge)
		}
		if got := verifier.Verify(header).Principal; got != "shared" {
			t.Errorf("Verify(%q) = %q, want shared", header, got)
		}
	}
}

// TestSSESessionCapHoldsAcrossCredentialSpellings is the end-to-end bypass
// check: four spellings of one valid token share one allowance.
func TestSSESessionCapHoldsAcrossCredentialSpellings(t *testing.T) {
	t.Parallel()

	_, h := cappedServer(t, 1)
	first := openSSE(t, h, "Bearer same-token")
	if !first.admitted() {
		t.Fatalf("first session refused: %d", first.status())
	}
	for _, header := range []string{"bearer same-token", "BEARER same-token", "Bearer  same-token"} {
		if got := openSSE(t, h, header); got.status() != http.StatusTooManyRequests {
			t.Errorf("session via %q = %d, want 429 (same credential)", header, got.status())
		}
	}
}

// TestSSESessionCeilingBoundsDistinctPrincipals proves the process-wide
// backstop: many distinct credentials, each under its own cap, still cannot
// hold more than the ceiling in total.
func TestSSESessionCeilingBoundsDistinctPrincipals(t *testing.T) {
	t.Parallel()

	mcp, h := cappedServer(t, 10, WithMaxSSESessions(3))
	var open []*sseStream
	for _, auth := range []string{"Bearer p-1", "Bearer p-2", "Bearer p-3"} {
		stream := openSSE(t, h, auth)
		if !stream.admitted() {
			t.Fatalf("session for %q under the ceiling refused: %d", auth, stream.status())
		}
		open = append(open, stream)
	}
	over := openSSE(t, h, "Bearer p-4")
	if over.status() != http.StatusTooManyRequests {
		t.Fatalf("session over the ceiling = %d, want 429", over.status())
	}
	if over.rec.Header().Get("Retry-After") == "" {
		t.Error("ceiling 429 has no Retry-After")
	}
	if got := mcp.SSEStats(); got.Active != 3 || got.Rejected != 1 {
		t.Errorf("SSEStats() = %+v, want Active 3 and Rejected 1", got)
	}

	open[0].close(t)
	if again := openSSE(t, h, "Bearer p-4"); !again.admitted() {
		t.Errorf("session after a slot freed refused: %d", again.status())
	}
}

// TestSSESessionCeilingDefaultsAndInvalidValues keeps a non-positive ceiling
// from disabling the backstop.
func TestSSESessionCeilingDefaultsAndInvalidValues(t *testing.T) {
	t.Parallel()

	if defaultMaxSSESessionsTotal < defaultMaxSSESessionsPerPrincipal {
		t.Fatalf("ceiling %d is below the per-principal default %d", defaultMaxSSESessionsTotal, defaultMaxSSESessionsPerPrincipal)
	}
	for name, opts := range map[string][]ServerOption{
		"unset": nil, "zero": {WithMaxSSESessions(0)}, "negative": {WithMaxSSESessions(-1)},
	} {
		s := NewServer(nil, nil, nil, "t", "t", opts...)
		if got := s.maxSSETotal(); got != defaultMaxSSESessionsTotal {
			t.Errorf("%s: ceiling = %d, want default %d", name, got, defaultMaxSSESessionsTotal)
		}
	}
}

// TestSchemeTrimmedLikeCore pins the shared-key match to core's rule for a
// scheme with whitespace glued to it, and documents that a separator-free
// tab form (which core refuses) stays its own principal.
func TestSchemeTrimmedLikeCore(t *testing.T) {
	t.Parallel()

	verifier := NewTokenVerifier("", nil, "deploy-key")
	for _, header := range []string{"Bearer\t deploy-key", "bearer\u00a0 deploy-key"} {
		if got, _ := edgeAuth("deploy-key", header); got != "shared" {
			t.Errorf("edgeAuth(%q) = %q, want shared", header, got)
		}
		if got := verifier.Verify(header).Principal; got != "shared" {
			t.Errorf("Verify(%q) = %q, want shared", header, got)
		}
	}
	canonical, _ := edgeAuth("deploy-key", "Bearer opaque-token")
	if tabOnly, _ := edgeAuth("deploy-key", "Bearer\topaque-token"); tabOnly == canonical {
		t.Error("a tab-only form, which core refuses, was folded into the Bearer principal")
	}
}
