package mcpserver

// Canonical forwarded principals (eshu-hq/eshu-mcp#50). The principal keys the
// per-principal SSE session cap, the session-owner check, and the admission
// cache, so one credential must map to one principal. RFC 7235 makes the
// scheme case-insensitive and Verify already matches the shared key that way,
// so hashing the raw header would give one opaque token a fresh principal for
// every spelling ("Bearer x", "bearer x", "Bearer  x") and let it walk past
// the cap. The scheme is lowercased and the secret's surrounding whitespace is
// dropped before hashing; the secret itself stays case-sensitive.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// forwardedPrincipal returns the opaque "fwd:" principal for a credential the
// edge cannot verify. It is stable across scheme case and spacing and never
// reveals the credential.
func forwardedPrincipal(credential string) string {
	canonical := strings.TrimSpace(credential)
	if scheme, rest, ok := strings.Cut(canonical, " "); ok {
		canonical = strings.ToLower(scheme) + " " + strings.TrimSpace(rest)
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
	scheme, rest, _ := strings.Cut(strings.TrimSpace(credential), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(rest)), []byte(sharedKey)) == 1
}
