package mcpserver

import (
	"strings"
	"testing"
)

// Edge opaque-credential pass-through: dotless Bearer credentials
// (personal and scoped tokens) forward to core, where core remains the
// admission authority; only dotted JWT attempts verify at the door.

func TestVerifyOpaqueBearerPassesThrough(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	// Eshu-issued personal tokens are opaque strings with no dots: the
	// edge cannot verify what carries no claims, so they keep the legacy
	// pass-through and core admits on dispatch (mirrors core's
	// isJWTShaped gate in go/internal/oidcbearer/resolver.go). Live
	// proof: the #9751 E2E wave 401'd these at the door before the gate.
	for _, header := range []string{
		"Bearer " + strings.Repeat("a", 43),
		"Bearer " + strings.Repeat("x", 40),
		"Bearer eshu_personal_token_value",
		"Bearer not-a-jwt",
	} {
		result := v.Verify(header)
		if result.Denial != DenyNone {
			t.Fatalf("%q: result.Denial = %v, want none (legacy forward)", header, result.Denial)
		}
		if !strings.HasPrefix(result.Principal, "fwd:") {
			t.Fatalf("%q: result.Principal = %q, want fwd: prefix", header, result.Principal)
		}
		if result.Outcome != "" {
			t.Fatalf("%q: result.Outcome = %q, want empty on admit", header, result.Outcome)
		}
	}
}

func TestIsOpaqueCredential(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		credential string
		want       bool
	}{
		{strings.Repeat("a", 43), true},
		{"eshu_personal_token_value", true},
		{"not-a-jwt", true},
		{"a.b.c", false},
		{"aaaa.bbbb.cccc", false},
		{"a.b", false},
		{"a.b.c.d", false},
		{".b.c", false},
		{"unsigned-header.payload.", false},
	} {
		if got := isOpaqueCredential(tc.credential); got != tc.want {
			t.Errorf("isOpaqueCredential(%q) = %v, want %v", tc.credential, got, tc.want)
		}
	}
}
