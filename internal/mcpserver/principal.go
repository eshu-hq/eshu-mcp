package mcpserver

// Canonical forwarded principals (eshu-hq/eshu-mcp#50). The principal keys the
// per-principal SSE session cap, the session-owner check, and the admission
// cache, so one credential must map to one principal. RFC 7235 makes the
// scheme case-insensitive and Verify already matches the shared key that way,
// so hashing the raw header would give one opaque token a fresh principal for
// every spelling ("Bearer x", "bearer x", "Bearer  x") and let it walk past
// the cap. The scheme is lowercased and the secret's surrounding whitespace is
// dropped before hashing, by core's own rule (splitCredential); the secret
// itself stays case-sensitive.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// splitCredential cuts a credential into its scheme and secret exactly as
// core's bearer parsing does (go/internal/query/auth.go): cut at the FIRST
// space, then trim the scheme and the secret with strings.TrimSpace. Matching
// core, not the RFC, is the point: whitespace glued to the scheme ("Bearer\t
// x", "Bearer\u00a0 x") still reaches the same token in core, so the edge must
// treat those spellings as one credential. found is false when there is no
// space, which core refuses; such a credential never passes admission.
func splitCredential(credential string) (scheme, secret string, found bool) {
	rawScheme, rest, found := strings.Cut(strings.TrimSpace(credential), " ")
	return strings.TrimSpace(rawScheme), strings.TrimSpace(rest), found
}

// forwardedPrincipal returns the opaque "fwd:" principal for a credential the
// edge cannot verify. It is stable across scheme case and spacing and never
// reveals the credential.
func forwardedPrincipal(credential string) string {
	canonical := strings.TrimSpace(credential)
	if scheme, secret, found := splitCredential(canonical); found {
		canonical = strings.ToLower(scheme) + " " + secret
	}
	sum := sha256.Sum256([]byte(canonical))
	return forwardedPrincipalPrefix + hex.EncodeToString(sum[:])
}

// matchesSharedKey reports whether credential is a Bearer credential carrying
// the deployment key, with the scheme matched case-insensitively exactly as
// TokenVerifier.Verify does. An empty sharedKey never matches.
func matchesSharedKey(sharedKey, credential string) bool {
	if sharedKey == "" {
		return false
	}
	scheme, secret, _ := splitCredential(credential)
	if !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(secret), []byte(sharedKey)) == 1
}
