package mcpserver

// Edge authentication, adapted from go/internal/mcp/transport_auth.go and
// go/internal/query/querycontract/unauthorized.go at 9922b49. The 401 body,
// headers, content type, envelope negotiation, and correlation-id behavior
// are byte-identical to core's WriteUnauthorized; the WWW-Authenticate
// challenge is the bare Bearer challenge augmented with the RFC 9728
// resource_metadata and scope directives when OAuth discovery is enabled
// (see DiscoveryConfig). The credential model differs by necessity and is recorded in
// docs/parity.md: the edge refuses a headerless request, binds SSE sessions
// to the presenting principal, and forwards the caller credential verbatim
// to core on every dispatch, where core remains the admission authority.
// When discovery is configured, Bearer JWTs are verified at the edge first
// (issuer, audience, lifetime, signature); see EdgeAuthMiddlewareWithVerifier.
// A constant-time shared-key comparison recognizes the deployment key as
// the "shared" principal for stable session binding. See docs/parity.md.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// principalContextKey carries the edge principal through the request.
type principalContextKey struct{}

// withPrincipal returns ctx carrying principal as the edge identity.
func withPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// principalKey returns the edge principal on ctx, or "" when the request
// entered without transport auth (escape-hatch mode) or failed it.
func principalKey(ctx context.Context) string {
	principal, ok := ctx.Value(principalContextKey{}).(string)
	if !ok {
		return ""
	}
	return principal
}

// edgeAuth checks one HTTP request's credential. It returns the principal to
// bind and whether the request may proceed. A missing Authorization header
// is refused: the edge never serves a headerless MCP request. A present
// credential is recognized as the "shared" principal when it constant-time
// matches the configured deployment key; any other present credential passes
// with a stable opaque principal, and core decides admission when the call
// dispatches. The credential itself is never logged and never leaves the
// process except forwarded to core on dispatch.
func edgeAuth(sharedKey, authHeader string) (string, bool) {
	credential := strings.TrimSpace(authHeader)
	if credential == "" {
		return "", false
	}
	if sharedKey != "" && subtle.ConstantTimeCompare([]byte(credential), []byte("Bearer "+sharedKey)) == 1 {
		return "shared", true
	}
	sum := sha256.Sum256([]byte(credential))
	return "fwd:" + hex.EncodeToString(sum[:]), true
}

// EdgeAuthMiddleware enforces the edge credential check on the MCP HTTP
// transport: without an Authorization header the request is refused with the
// core-identical 401 before the body is decoded. sharedKey is the deployment
// key ESHU_API_KEY; empty disables the shared fast path but a present
// credential still passes to core for admission. The challenge is the bare
// Bearer [REDACTED]; use EdgeAuthMiddlewareWithChallenge when OAuth discovery is
// enabled so the 401 steers discovery-capable clients at the metadata
// document.
func EdgeAuthMiddleware(sharedKey string, logger *slog.Logger) func(http.Handler) http.Handler {
	return EdgeAuthMiddlewareWithChallenge(sharedKey, BareChallenge, logger)
}

// EdgeAuthMiddlewareWithChallenge is EdgeAuthMiddleware with an explicit
// WWW-Authenticate challenge value: the DiscoveryConfig challenge when
// OAuth discovery is enabled, the bare challenge otherwise. The value is
// fixed at wiring time from static operator config.
func EdgeAuthMiddlewareWithChallenge(sharedKey, challenge string, logger *slog.Logger) func(http.Handler) http.Handler {
	return edgeAuthMiddleware(sharedKey, challenge, challenge, nil, logger)
}

// EdgeAuthMiddlewareWithVerifier is EdgeAuthMiddlewareWithChallenge plus
// the door check: when verifier is non-nil, a Bearer JWT is verified
// against the configured issuers before it may proceed. An unrecognized
// credential answers the discovery challenge so capable clients steer at
// the metadata document; a post-match failure (recognized issuer, wrong
// audience or bad crypto) answers the bare challenge and never steers.
// A nil verifier keeps the legacy behavior exactly: any present credential
// passes and core admits on dispatch.
func EdgeAuthMiddlewareWithVerifier(sharedKey string, discovery DiscoveryConfig, verifier *TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler {
	return edgeAuthMiddleware(sharedKey, discovery.ChallengeValue(), BareChallenge, verifier, logger)
}

func edgeAuthMiddleware(sharedKey, unrecognizedChallenge, postMatchChallenge string, verifier *TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if strings.TrimSpace(header) == "" {
				writeUnauthorizedWithChallenge(w, r, unrecognizedChallenge)
				if logger != nil {
					logger.Warn("mcp transport request without credential refused",
						"method", r.Method, "path", r.URL.Path)
				}
				return
			}
			principal := ""
			denial := DenyNone
			outcome, iss := "", ""
			if verifier == nil {
				var ok bool
				principal, ok = edgeAuth(sharedKey, header)
				if !ok {
					denial = DenyUnrecognized
				}
			} else {
				result := verifier.Verify(header)
				principal, denial, iss, outcome = result.Principal, result.Denial, result.Issuer, result.Outcome
			}
			if denial != DenyNone {
				challenge := unrecognizedChallenge
				if denial == DenyPostMatch {
					challenge = postMatchChallenge
				}
				writeUnauthorizedWithChallenge(w, r, challenge)
				if logger != nil {
					// Every denial reaching this block carries an outcome:
					// headerless returns earlier and edgeAuth denies only
					// empty credentials (also earlier), while Verify sets
					// Outcome on every denial return. Method/path stay on
					// the line: the E2E scraper reads named keys, and the
					// operator keeps the request context.
					logger.Warn("oidc Bearer [REDACTED]", "iss", iss, "outcome", outcome,
						"method", r.Method, "path", r.URL.Path)
				}
				return
			}
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))
		})
	}
}

// writeJSON writes one JSON response with the core content type and encoder
// settings: application/json with charset, HTML escaping off, trailing
// newline.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return
	}
}

// acceptsEnvelope reports whether the caller asked for the canonical eshu
// envelope, mirroring core's content negotiation.
func acceptsEnvelope(r *http.Request) bool {
	return r != nil && strings.Contains(r.Header.Get("Accept"), "application/eshu.envelope+json")
}

// correlationID returns the caller's correlation id when it passed one, or a
// fresh random one. It mirrors core's DocumentationCorrelationID.
func correlationID(r *http.Request) string {
	for _, header := range []string{"X-Correlation-ID", "X-Request-ID"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	return hex.EncodeToString(raw[:])
}

// writeUnauthorized writes the 401 refusal byte-identical in shape to
// core's: the bare Bearer challenge, the same status, content type, and JSON
// body with a correlation id, in envelope form when the caller asked for the
// envelope and in the plain error_code form otherwise.
func writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	writeUnauthorizedWithChallenge(w, r, BareChallenge)
}

// writeUnauthorizedWithChallenge is writeUnauthorized with an explicit
// WWW-Authenticate challenge value: the DiscoveryConfig challenge when
// OAuth discovery is enabled, the bare challenge otherwise.
func writeUnauthorizedWithChallenge(w http.ResponseWriter, r *http.Request, challenge string) {
	w.Header().Set("WWW-Authenticate", challenge)
	if acceptsEnvelope(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"data":  nil,
			"truth": nil,
			"error": map[string]any{
				"code":           "unauthenticated",
				"message":        "authentication is required",
				"correlation_id": correlationID(r),
			},
		})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error_code":     "unauthenticated",
		"message":        "authentication is required",
		"correlation_id": correlationID(r),
	})
}

// writeForbiddenSessionMismatch writes the 403 a POST /mcp/message carrying
// a valid credential into an SSE session another principal opened receives.
// The body matches core's session-hijack refusal exactly.
func writeForbiddenSessionMismatch(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		Error:   &jsonrpcError{Code: -32001, Message: "credential does not match the session owner"},
	}); err != nil {
		return
	}
}

// RequireCredentialSource is the destination's "no silent open mode" gate,
// mirroring requireMCPHTTPCredentialSource: an HTTP transport with no
// credential posture refuses to start unless the operator explicitly sets
// the loopback escape hatch. credentialConfigured folds the three explicit
// operator knobs the monolith counts (see CredentialSourceConfigured): the
// shared key, the scoped-token file, and the OIDC bearer [REDACTED] resource URI.
func RequireCredentialSource(credentialConfigured, allowUnauthenticated bool) error {
	if credentialConfigured || allowUnauthenticated {
		return nil
	}
	return ErrNoCredentialSource
}

// errNoCredentialSource is the actionable boot refusal when the server would
// otherwise start with no credential posture.
var ErrNoCredentialSource = errors.New("MCP HTTP transport has no credential posture: set ESHU_API_KEY, ESHU_SCOPED_TOKENS_FILE, or ESHU_AUTH_RESOURCE_URI, or explicitly set ESHU_MCP_ALLOW_UNAUTHENTICATED=true for loopback/dev use only (never expose this port publicly with the escape hatch set)")
