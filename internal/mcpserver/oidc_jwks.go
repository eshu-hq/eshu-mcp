package mcpserver

// Issuer key transport: OIDC discovery, JWKS fetch with origin pin, and
// the singleflight plus negative-kid cache that keeps crafted tokens from
// triggering egress per request.

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const jwksCacheTTL = 10 * time.Minute

// TokenVerifier checks Bearer JWTs against the configured issuers. The zero
// value is unusable; build one with NewTokenVerifier. It is safe for
// concurrent use.
type cachedKeys struct {
	byKid map[string]*keyEntry
	// negKid records kids proven absent so one crafted token cannot
	// refetch per request; entries expire fast so rotations converge.
	negKid  map[string]time.Time
	expires time.Time
}

// fetchCall is one in-flight refetch; followers wait on done instead of
// fetching, so N concurrent unknown-kid requests cause one egress fetch.
type fetchCall struct {
	done chan struct{}
	err  error
}

// negKidTTL bounds a negative kid entry so a rotation lands promptly.
const negKidTTL = 60 * time.Second

type keyEntry struct {
	key any
}

// NewTokenVerifier returns a verifier for resource trusting issuers. An
// empty sharedKey disables only the shared fast path, never verification.
func (v *TokenVerifier) freshSetLocked(iss string) *cachedKeys {
	set, ok := v.keys[iss]
	if !ok || time.Now().After(set.expires) {
		return nil
	}
	return set
}

// lookupKey returns the key for kid in a fresh set. An empty kid matches
// only a single-key set, so a kidless token cannot smuggle itself past a
// multi-key issuer.
func lookupKey(set *cachedKeys, kid string) (*keyEntry, bool) {
	if set == nil {
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

// lookupNeg reports the negative-cache expiry for kid; the caller must
// hold v.mu.
func lookupNeg(set *cachedKeys, kid string) (time.Time, bool) {
	if set == nil {
		return time.Time{}, false
	}
	deniedAt, ok := set.negKid[kid]
	return deniedAt, ok
}

// joinFetchLocked joins the in-flight refetch for iss, or starts one.
// Callers must hold v.mu; leadership is reported.
func (v *TokenVerifier) joinFetchLocked(iss string) (*fetchCall, bool) {
	if call, ok := v.inflight[iss]; ok {
		return call, false
	}
	call := &fetchCall{done: make(chan struct{})}
	if v.inflight == nil {
		v.inflight = make(map[string]*fetchCall)
	}
	v.inflight[iss] = call
	return call, true
}

// finishFetchLocked publishes a finished refetch and wakes the followers.
// Callers must hold v.mu.
func (v *TokenVerifier) finishFetchLocked(iss string, call *fetchCall, err error) {
	call.err = err
	delete(v.inflight, iss)
	close(call.done)
}

// recordNegLocked remembers that kid is absent after a successful fetch.
// Callers must hold v.mu.
func (v *TokenVerifier) recordNegLocked(iss, kid string) {
	set := v.freshSetLocked(iss)
	if set == nil {
		return
	}
	if set.negKid == nil {
		set.negKid = make(map[string]time.Time)
	}
	set.negKid[kid] = time.Now().Add(negKidTTL)
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
	// The keys must come from the issuer's own origin: a discovery
	// document pointing elsewhere hands signature trust to a stranger.
	if !sameOrigin(iss, doc.JWKSURI) {
		return fmt.Errorf("jwks_uri outside issuer origin")
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
	v.mu.Lock()
	// Carry unexpired negative entries across the replacement: a refetch
	// must not resurrect per-request egress for kids already proven absent.
	if old, ok := v.keys[iss]; ok {
		now := time.Now()
		for kid, until := range old.negKid {
			if now.Before(until) {
				if fresh.negKid == nil {
					fresh.negKid = make(map[string]time.Time)
				}
				fresh.negKid[kid] = until
			}
		}
	}
	v.mu.Unlock()
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

// sameOrigin reports whether raw shares the issuer's scheme and host,
// compared exactly (port included) with no normalization or fallback.
func sameOrigin(iss, raw string) bool {
	iu, iuErr := url.Parse(strings.TrimSpace(iss))
	ju, juErr := url.Parse(strings.TrimSpace(raw))
	return iuErr == nil && juErr == nil &&
		strings.EqualFold(iu.Scheme, ju.Scheme) && strings.EqualFold(iu.Host, ju.Host)
}

// HTTPNonLoopbackIssuer reports whether iss is a plain-HTTP issuer on a
// non-loopback host. Such issuers work — the E2E mock IdP is one — but
// their JWKS travels unauthenticated, so the operator is warned loudly at
// wiring time. Prefer https issuers in any network you do not control.
func HTTPNonLoopbackIssuer(iss string) bool {
	u, err := url.Parse(strings.TrimSpace(iss))
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") && !isLoopbackHost(u.Hostname())
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
