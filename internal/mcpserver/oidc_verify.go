package mcpserver

// Edge OIDC verification: the door check. When discovery is configured,
// a Bearer JWT must carry a recognized issuer, an audience matching the
// resource, a live expiry, and a signature from the issuer's JWKS. Anything
// else is refused at the edge before dispatch, because core admission on a
// forwarded call cannot protect the locally served surface (tools/list,
// discovery) that never dispatches. Deployments without discovery keep the
// legacy behavior: any present credential passes and core admits.

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
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
const jwksCacheTTL = 10 * time.Minute

// TokenVerifier checks Bearer JWTs against the configured issuers. The zero
// value is unusable; build one with NewTokenVerifier. It is safe for
// concurrent use.
type TokenVerifier struct {
	resource  string
	issuers   map[string]bool
	sharedKey string

	client *http.Client

	mu   sync.Mutex
	keys map[string]*cachedKeys
}

type cachedKeys struct {
	byKid   map[string]*keyEntry
	expires time.Time
}

type keyEntry struct {
	key any
}

// NewTokenVerifier returns a verifier for resource trusting issuers. An
// empty sharedKey disables only the shared fast path, never verification.
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

// Verify checks one Authorization header value. DenyNone carries the
// principal to bind; any other kind refuses the request. A credential that
// is not a Bearer token keeps the legacy pass-through: the edge cannot
// verify what has no verifiable shape, and core still admits on dispatch.
func (v *TokenVerifier) Verify(authHeader string) (string, DenialKind) {
	credential := strings.TrimSpace(authHeader)
	const prefix = "Bearer "
	if !strings.HasPrefix(credential, prefix) {
		if credential == "Bearer" {
			return "", DenyUnrecognized
		}
		sum := sha256.Sum256([]byte(credential))
		return "fwd:" + hex.EncodeToString(sum[:]), DenyNone
	}
	if v.sharedKey != "" && subtle.ConstantTimeCompare([]byte(credential), []byte(prefix+v.sharedKey)) == 1 {
		return "shared", DenyNone
	}
	raw := strings.TrimSpace(strings.TrimPrefix(credential, prefix))
	if kind := classifyToken(raw, v.resource, v.issuers); kind != DenyNone {
		return "", kind
	}
	claims, err := v.parseVerified(raw)
	if err != nil {
		return "", DenyPostMatch
	}
	sum := sha256.Sum256([]byte(claimString(claims, "iss") + "\x00" + claimString(claims, "sub")))
	return "jwt:" + hex.EncodeToString(sum[:]), DenyNone
}

// claimString reads one optional string claim, returning "" when absent or
// mistyped. Callers treat "" as unmatched, never as verified identity.
func claimString(claims jwt.MapClaims, key string) string {
	value, ok := claims[key].(string)
	if !ok {
		return ""
	}
	return value
}

// classifyToken reads the unverified claims only to choose the denial
// shape: unknown issuer or unparsable input steers to discovery, while a
// recognized token that fails audience or lifetime answers bare. It trusts
// nothing; the signature is checked separately by parseVerified.
func classifyToken(raw, resource string, issuers map[string]bool) DenialKind {
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(raw, claims); err != nil {
		return DenyUnrecognized
	}
	if iss := claimString(claims, "iss"); !issuers[iss] {
		return DenyUnrecognized
	}
	if !audienceContains(claims["aud"], resource) {
		return DenyPostMatch
	}
	now := time.Now()
	if exp, ok := claims["exp"].(float64); ok && now.After(time.Unix(int64(exp), 0).Add(verifyLeeway)) {
		return DenyPostMatch
	}
	if nbf, ok := claims["nbf"].(float64); ok && now.Add(verifyLeeway).Before(time.Unix(int64(nbf), 0)) {
		return DenyPostMatch
	}
	return DenyNone
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

// signingKey resolves the verification key for iss, refetching once when a
// cached set lacks kid so a rotation converges on the next request.
func (v *TokenVerifier) signingKey(iss, kid string) (any, error) {
	if !v.issuers[iss] {
		return nil, fmt.Errorf("unknown issuer")
	}
	if entry, ok := v.cachedKey(iss, kid); ok {
		return entry.key, nil
	}
	if err := v.refetch(iss); err != nil {
		return nil, err
	}
	if entry, ok := v.cachedKey(iss, kid); ok {
		return entry.key, nil
	}
	return nil, fmt.Errorf("no key for kid %q", truncateForLog(kid))
}

// cachedKey returns the key for kid when the cached set is fresh. An empty
// kid matches only a single-key set, so a kidless token cannot smuggle
// itself past a multi-key issuer.
func (v *TokenVerifier) cachedKey(iss, kid string) (*keyEntry, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	set, ok := v.keys[iss]
	if !ok || time.Now().After(set.expires) {
		return nil, false
	}
	if kid == "" {
		if len(set.byKid) == 1 {
			for _, entry := range set.byKid {
				return entry, true
			}
		}
		return nil, false
	}
	entry, ok := set.byKid[kid]
	return entry, ok
}

// refetch reloads iss from its OIDC discovery document. The issuer comes
// from operator config, so plain HTTP is accepted: the allowlist is the
// trust root, not the transport.
func (v *TokenVerifier) refetch(iss string) error {
	docURL := strings.TrimSuffix(iss, "/") + "/.well-known/openid-configuration"
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := v.getJSON(docURL, &doc); err != nil {
		return err
	}
	if strings.TrimSpace(doc.JWKSURI) == "" {
		return fmt.Errorf("issuer has no jwks_uri")
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := v.getJSON(doc.JWKSURI, &set); err != nil {
		return err
	}
	fresh := &cachedKeys{byKid: make(map[string]*keyEntry), expires: time.Now().Add(jwksCacheTTL)}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") || k.Kid == "" {
			continue
		}
		pub, err := rsaKey(k.N, k.E)
		if err != nil {
			continue
		}
		fresh.byKid[k.Kid] = &keyEntry{key: pub}
	}
	if len(fresh.byKid) == 0 {
		return fmt.Errorf("no usable RSA keys")
	}
	v.mu.Lock()
	v.keys[iss] = fresh
	v.mu.Unlock()
	return nil
}

// getJSON fetches one JSON document with a bounded client. Error text never
// includes response bodies, which could carry attacker content into logs.
func (v *TokenVerifier) getJSON(url string, value any) error {
	resp, err := v.client.Get(url) //nolint:gosec,noctx // bounded client, operator-configured URL
	if err != nil {
		return err
	}
	const maxJWKSBytes = 1 << 20
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return err
	}
	return nil
}

// rsaKey decodes one RSA JWK. Only public material is ever handled; E must
// fit an int so oversized exponents cannot overflow the verifier.
func rsaKey(nB64, eB64 string) (any, error) {
	nRaw, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eRaw, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nRaw)
	e := new(big.Int).SetBytes(eRaw)
	if !e.IsInt64() || n.Sign() <= 0 {
		return nil, fmt.Errorf("invalid RSA parameters")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
