package mcpserver

// Edge OIDC verification: the door check. When discovery is configured,
// every Bearer-shaped credential is verified — recognized issuer, matching
// audience, live expiry, present subject, signature from the issuer's own
// JWKS origin — before it may proceed. Anything else is refused at the
// edge, because core admission cannot protect the locally served surface
// (tools/list, discovery) that never dispatches. Other schemes keep the
// intentional legacy pass-through to core; without discovery, so do Bearer JWTs.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DenialKind separates the two 401 shapes the edge sends: an unrecognized
// credential steers a discovery-capable client at the metadata document,
// while a post-match failure (right issuer, wrong audience or bad crypto)
// answers the bare challenge and never steers.
type DenialKind int

const (
	// DenyNone admits the request with the returned principal.
	DenyNone DenialKind = iota
	// DenyUnrecognized refuses with the discovery challenge.
	DenyUnrecognized
	// DenyPostMatch refuses with the bare Bearer [REDACTED]
	DenyPostMatch
)

// verifyLeeway forgives clock skew between the edge and the issuer when
// checking exp/nbf, mirroring standard OIDC relying-party tolerance.
const verifyLeeway = 60 * time.Second

// jwksCacheTTL bounds how long one issuer's keys are trusted without a
// refetch. An unknown kid forces one early refetch before failing, so a
// rotation converges on the next request instead of waiting out the TTL.
type TokenVerifier struct {
	resource  string
	issuers   map[string]bool
	sharedKey string

	client *http.Client

	mu       sync.Mutex
	keys     map[string]*cachedKeys
	inflight map[string]*fetchCall
}

func NewTokenVerifier(resource string, issuers []string, sharedKey string) *TokenVerifier {
	set := make(map[string]bool, len(issuers))
	for _, iss := range issuers {
		if trimmed := strings.TrimSpace(iss); trimmed != "" {
			set[trimmed] = true
		}
	}
	return &TokenVerifier{
		resource:  resource,
		issuers:   set,
		sharedKey: sharedKey,
		client:    &http.Client{Timeout: 5 * time.Second},
		keys:      make(map[string]*cachedKeys),
	}
}

// Outcome vocabulary for edge denials, byte-identical to the monolith
// resolver's (go/internal/oidcbearer/telemetry.go): the E2E leakage matrix
// scrapes these strings from the edge log, and core's future denial audit
// will share them. no_grants never fires here — the edge does not evaluate
// group grants; core denies those on dispatch.
const (
	outcomeUnknownIssuer = "unknown_issuer"
	outcomeWrongAudience = "wrong_audience"
	outcomeExpired       = "expired"
	outcomeBadSignature  = "bad_signature"
	outcomeMalformed     = "malformed"
	// outcomeJWKSFetchFailure fires when the issuer is trusted but its
	// keys cannot be loaded (issuer down): the token is unverifiable,
	// which core distinguishes from a bad signature.
	outcomeJWKSFetchFailure = "jwks_fetch_failure"
)

// fetchError marks a verification failure caused by being unable to load
// the issuer's keys, as opposed to a token that fails against loaded keys.
// Error() delegates so the message is unchanged; callers match with
// errors.As, which traverses the jwt library's wrapping.
type fetchError struct{ err error }

func (e *fetchError) Error() string { return e.err.Error() }
func (e *fetchError) Unwrap() error { return e.err }

// VerifyResult is one door decision: DenyNone admits Principal, any other
// kind refuses with Outcome logged and Issuer attributed ("" when the token
// never parsed). Outcome is "" exactly when DenyNone admits.
type VerifyResult struct {
	Principal string
	Denial    DenialKind
	Issuer    string
	Outcome   string
}

// Verify checks one Authorization header value. A credential that is not
// a Bearer, and an opaque (dotless) Bearer [REDACTED] no claims to check, keeps the
// legacy pass-through: the edge cannot verify what has no verifiable
// shape, and core still admits on dispatch. Only a dotted Bearer - a
// JWT attempt - is verified against the configured issuers (issuer,
// audience, lifetime, signature).
func (v *TokenVerifier) Verify(authHeader string) VerifyResult {
	credential := strings.TrimSpace(authHeader)
	// RFC 7235 schemes are case-insensitive: every Bearer-shaped
	// credential is verified, whatever its case. Only a genuinely
	// different scheme keeps the legacy pass-through.
	scheme, rest, _ := strings.Cut(credential, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		sum := sha256.Sum256([]byte(credential))
		return VerifyResult{Principal: "fwd:" + hex.EncodeToString(sum[:])}
	}
	raw := strings.TrimSpace(rest)
	const prefix = "Bearer "
	if v.sharedKey != "" && subtle.ConstantTimeCompare([]byte(prefix+raw), []byte(prefix+v.sharedKey)) == 1 {
		return VerifyResult{Principal: "shared"}
	}
	// A Bearer [REDACTED] that never had the compact JWT shape carries no
	// claims to verify: opaque personal and scoped tokens keep the legacy
	// pass-through and core admits on dispatch. This mirrors core's
	// isJWTShaped gate (go/internal/oidcbearer/resolver.go), which falls
	// through to the next resolver for the same input. An empty Bearer
	// (present scheme, absent credential) stays malformed: it is broken,
	// not opaque.
	if raw != "" && isOpaqueCredential(raw) {
		sum := sha256.Sum256([]byte(credential))
		return VerifyResult{Principal: "fwd:" + hex.EncodeToString(sum[:])}
	}
	kind, outcome, iss := classifyToken(raw, v.resource, v.issuers)
	if kind != DenyNone {
		return VerifyResult{Denial: kind, Issuer: iss, Outcome: outcome}
	}
	// iss comes from classification: a failed parse leaves claims empty,
	// and attribution must survive crypto failure.
	claims, err := v.parseVerified(raw)
	if err != nil {
		// Recognized issuer, unverifiable token: fail closed. An issuer
		// whose keys cannot be loaded lands in the fetch bucket (core's
		// jwks_fetch_failure); a token that fails against loaded keys
		// lands in the signature bucket.
		var fetchErr *fetchError
		if errors.As(err, &fetchErr) {
			return VerifyResult{Denial: DenyPostMatch, Issuer: iss, Outcome: outcomeJWKSFetchFailure}
		}
		return VerifyResult{Denial: DenyPostMatch, Issuer: iss, Outcome: outcomeBadSignature}
	}
	// A principal is the edge identity bound to SSE sessions: two subjects
	// must never share one. Real issuers always set sub, so its absence
	// denies rather than minting an issuer-constant principal.
	if claimString(claims, "sub") == "" {
		return VerifyResult{Denial: DenyPostMatch, Issuer: iss, Outcome: outcomeMalformed}
	}
	sum := sha256.Sum256([]byte(iss + "\x00" + claimString(claims, "sub")))
	return VerifyResult{Principal: "jwt:" + hex.EncodeToString(sum[:])}
}

// claimString reads one optional string claim, returning "" when absent or
// mistyped. Verify denies an empty sub outright; classification treats a
// missing iss as unmatched. No caller admits on "".
func claimString(claims jwt.MapClaims, key string) string {
	value, ok := claims[key].(string)
	if !ok {
		return ""
	}
	return value
}

// isOpaqueCredential reports whether raw can never be a compact JWT:
// Eshu-issued personal and scoped tokens are opaque hex/base64 strings
// with no dots (see core go/internal/oidcbearer/claims.go), so a dotless
// Bearer [REDACTED] no claims to verify and keeps the legacy pass-through to
// core, where core remains the admission authority. Any dotted input is a
// JWT attempt and stays on the verification path: core's strict shape
// gate would fall an unsigned trailing-dot token through to a later
// resolver, but the edge refuses it at the door instead (bad_signature or
// malformed), which the outcome table pins and the E2E leakage matrix
// relies on.
func isOpaqueCredential(raw string) bool {
	return !strings.Contains(raw, ".")
}

// classifyToken reads the unverified claims only to choose the denial
// shape and outcome: unknown issuer or unparsable input steers to
// discovery, while a recognized token that fails audience or lifetime
// answers bare. It trusts nothing; the signature is checked separately by
// parseVerified.
func classifyToken(raw, resource string, issuers map[string]bool) (DenialKind, string, string) {
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(raw, claims); err != nil {
		return DenyUnrecognized, outcomeMalformed, ""
	}
	iss := claimString(claims, "iss")
	if !issuers[iss] {
		return DenyUnrecognized, outcomeUnknownIssuer, iss
	}
	if !audienceContains(claims["aud"], resource) {
		return DenyPostMatch, outcomeWrongAudience, iss
	}
	now := time.Now()
	if exp, ok := claims["exp"].(float64); ok && now.After(time.Unix(int64(exp), 0).Add(verifyLeeway)) {
		return DenyPostMatch, outcomeExpired, iss
	}
	if nbf, ok := claims["nbf"].(float64); ok && now.Add(verifyLeeway).Before(time.Unix(int64(nbf), 0)) {
		// Not yet valid is malformed, not expired: core maps only
		// *oidc.TokenExpiredError to expired and defaults the rest.
		return DenyPostMatch, outcomeMalformed, iss
	}
	return DenyNone, "", iss
}

// audienceContains reports whether the aud claim (string or list, per RFC
// 7519) carries resource.
func audienceContains(aud any, resource string) bool {
	switch value := aud.(type) {
	case string:
		return value == resource
	case []any:
		for _, entry := range value {
			if s, ok := entry.(string); ok && s == resource {
				return true
			}
		}
	case []string:
		for _, s := range value {
			if s == resource {
				return true
			}
		}
	}
	return false
}

// parseVerified enforces the cryptographic half: RS256 only (never none or
// symmetric confusion), issuer keys from the issuer's own discovery
// document, audience and lifetime per the standard claims. The issuer comes
// from the unverified claims, which is sound because the signature check
// binds it: keys are fetched per-issuer, so a token cannot borrow another
// issuer's audience.
func (v *TokenVerifier) parseVerified(raw string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodRS256 {
				return nil, fmt.Errorf("unexpected signing method %v", token.Header["alg"])
			}
			tokenClaims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return nil, fmt.Errorf("unexpected claims type %T", token.Claims)
			}
			return v.signingKey(claimString(tokenClaims, "iss"), kidOf(token))
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(v.resource),
		jwt.WithLeeway(verifyLeeway),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("token invalid")
	}
	return claims, nil
}

// kidOf reads the key id without trusting it; an absent kid is empty and
// matches only a single-key issuer.
func kidOf(token *jwt.Token) string {
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return ""
	}
	return kid
}

// signingKey resolves the verification key for iss. A fresh cache answers
// inline; otherwise exactly one goroutine refetches while the rest wait,
// and a kid proven absent denies from the negative cache without egress.
func (v *TokenVerifier) signingKey(iss, kid string) (any, error) {
	if !v.issuers[iss] {
		return nil, fmt.Errorf("unknown issuer")
	}
	v.mu.Lock()
	if entry, ok := lookupKey(v.freshSetLocked(iss), kid); ok {
		v.mu.Unlock()
		return entry.key, nil
	}
	if deniedAt, ok := lookupNeg(v.freshSetLocked(iss), kid); ok && time.Now().Before(deniedAt) {
		v.mu.Unlock()
		return nil, fmt.Errorf("no key for kid %q", truncateForLog(kid))
	}
	call, leader := v.joinFetchLocked(iss)
	v.mu.Unlock()
	if !leader {
		<-call.done
		v.mu.Lock()
		defer v.mu.Unlock()
		if entry, ok := lookupKey(v.freshSetLocked(iss), kid); ok {
			return entry.key, nil
		}
		if call.err != nil {
			return nil, &fetchError{call.err}
		}
		// A follower's kid stays unproven by the shared fetch: record it
		// so it denies from cache instead of fetching on its own turn.
		v.recordNegLocked(iss, kid)
		return nil, fmt.Errorf("no key for kid %q", truncateForLog(kid))
	}
	err := v.refetch(iss)
	v.mu.Lock()
	defer v.mu.Unlock()
	if err == nil {
		if entry, ok := lookupKey(v.freshSetLocked(iss), kid); ok {
			v.finishFetchLocked(iss, call, nil)
			return entry.key, nil
		}
		v.recordNegLocked(iss, kid)
	}
	v.finishFetchLocked(iss, call, err)
	if err != nil {
		return nil, &fetchError{err}
	}
	return nil, fmt.Errorf("no key for kid %q", truncateForLog(kid))
}

// freshSetLocked returns the cached set for iss, or nil when absent or
// expired. Callers must hold v.mu.
