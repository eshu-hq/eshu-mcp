package mcpserver

// RFC 9728 OAuth discovery for the satellite edge, adapted from
// go/internal/query/auth_oauth_discovery.go and
// go/cmd/mcp-server/oauth_discovery_wiring.go at 9922b49 (issue #5168,
// F-2; eshu-hq/eshu-mcp#27). The 401 challenge shape, the metadata
// document fields and order, the route set, and the metadata-URL
// derivation match the monolith exactly so scripted OAuth clients cannot
// tell the edge from the in-tree server.
//
// The honest difference is posture derivation. The monolith derives
// enablement per-request from Postgres identity stores (providers, sign-in
// policy, active issuers). The satellite holds no stores, so enablement is
// static operator config: a valid ESHU_AUTH_RESOURCE_URI plus at least one
// ESHU_AUTH_AUTHORIZATION_SERVERS issuer. No provider picker lives on the
// edge; core remains the admission authority and the edge forwards the
// caller credential verbatim on every dispatch.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// DefaultOAuthChallengeScope is the space-delimited OAuth scope set the
// 401 challenge advertises and the discovery document lists as
// scopes_supported. It matches core's DefaultOAuthChallengeScope value:
// the groups scope is load-bearing, because core's token resolver denies
// a grant when the verified token carries no groups claim.
const DefaultOAuthChallengeScope = "openid profile email groups"

// BareChallenge is the WWW-Authenticate value when OAuth discovery is
// disabled: byte-for-byte the value the transport has always sent.
const BareChallenge = "Bearer"

// ResourceDocumentationEnv and PreregisteredClientIDEnv are the optional
// operator knobs the discovery document copies verbatim, same names as
// the monolith reads.
const (
	ResourceDocumentationEnv = "ESHU_AUTH_RESOURCE_DOCUMENTATION"
	PreregisteredClientIDEnv = "ESHU_AUTH_PREREGISTERED_CLIENT_ID"
	ResourceURIEnv           = "ESHU_AUTH_RESOURCE_URI"
	AuthorizationServersEnv  = "ESHU_AUTH_AUTHORIZATION_SERVERS"
	// #nosec G101 -- the value is the operator env var name the monolith
	// also reads, not a credential: no secret material appears here.
	ScopedTokensFileEnv   = "ESHU_SCOPED_TOKENS_FILE"
	discoveryCacheControl = "public, max-age=60"
	discoveryRoute        = "GET /.well-known/oauth-protected-resource"
	discoveryRouteSuffix  = "GET /.well-known/oauth-protected-resource/{rest...}"
	discoveryContentType  = "application/json"
)

// OAuthProtectedResourceMetadata is the RFC 9728 section 2 document the
// edge serves. Field names, JSON shape, and field order match core's
// OAuthProtectedResourceMetadata: resource is the only REQUIRED field and
// every other field is included only when the deployment has a real value
// for it, so the edge never advertises a phantom capability.
type OAuthProtectedResourceMetadata struct {
	Resource                  string   `json:"resource"`
	AuthorizationServers      []string `json:"authorization_servers,omitempty"`
	BearerMethodsSupported    []string `json:"bearer_methods_supported,omitempty"`
	ScopesSupported           []string `json:"scopes_supported,omitempty"`
	ResourceName              string   `json:"resource_name,omitempty"`
	ResourceDocumentation     string   `json:"resource_documentation,omitempty"`
	EshuPreregisteredClientID string   `json:"eshu_preregistered_client_id,omitempty"`
}

// DiscoveryConfig is the static OAuth discovery posture for one process,
// built once from operator env at startup.
type DiscoveryConfig struct {
	Resource              string
	Issuers               []string
	ScopesSupported       []string
	ResourceName          string
	ResourceDocumentation string
	PreregisteredClientID string
	MetadataURL           string
}

// DiscoveryConfigFromEnv builds the discovery posture from operator env.
// ESHU_AUTH_AUTHORIZATION_SERVERS is whitespace-separated; empty entries
// are dropped. The config reports disabled unless the resource URI is
// valid and at least one issuer is configured.
func DiscoveryConfigFromEnv(getenv func(string) string) DiscoveryConfig {
	resource := strings.TrimSpace(getenv(ResourceURIEnv))
	var issuers []string
	for _, issuer := range strings.Fields(getenv(AuthorizationServersEnv)) {
		if trimmed := strings.TrimSpace(issuer); trimmed != "" {
			issuers = append(issuers, trimmed)
		}
	}
	cfg := DiscoveryConfig{
		Resource:              resource,
		Issuers:               issuers,
		ScopesSupported:       strings.Fields(DefaultOAuthChallengeScope),
		ResourceName:          "Eshu MCP Server",
		ResourceDocumentation: strings.TrimSpace(getenv(ResourceDocumentationEnv)),
		PreregisteredClientID: strings.TrimSpace(getenv(PreregisteredClientIDEnv)),
	}
	if metadataURL, ok := MetadataURLForResource(resource); ok && len(issuers) > 0 {
		cfg.MetadataURL = metadataURL
	}
	return cfg
}

// Enabled reports whether this config serves discovery: a valid resource
// URI and at least one issuer, mirroring the monolith's dual gate (valid
// URI plus a metadata route that would actually resolve).
func (d DiscoveryConfig) Enabled() bool {
	return d.MetadataURL != "" && len(d.Issuers) > 0
}

// ChallengeValue builds the WWW-Authenticate header value for a 401: the
// bare Bearer [REDACTED] unless discovery is enabled, in which case the RFC 9728
// resource_metadata directive plus the RFC 6750 scope directive, exactly
// the composition core's oauthWWWAuthenticateChallenge builds. A metadata
// URL or scope carrying a quote or control character degrades to the bare
// challenge rather than emitting a broken header value.
func (d DiscoveryConfig) ChallengeValue() string {
	if !d.Enabled() {
		return BareChallenge
	}
	if hasHeaderDelimiter(d.MetadataURL) || hasHeaderDelimiter(strings.Join(d.ScopesSupported, " ")) {
		return BareChallenge
	}
	challenge := BareChallenge + ` resource_metadata="` + d.MetadataURL + `"`
	if scope := strings.Join(d.ScopesSupported, " "); strings.TrimSpace(scope) != "" {
		challenge += `, scope="` + scope + `"`
	}
	return challenge
}

// Mount registers the discovery routes on mux: both the bare well-known
// root (the RFC 9728 root fallback) and the path-suffixed route, which
// only answers for the exact suffix derived from the resource's own path.
// Any other suffix 404s so a strict client falls back to the root rather
// than dead-ending. A disabled config registers routes that always 404,
// indistinguishable from the route never having been mounted.
func (d DiscoveryConfig) Mount(mux *http.ServeMux) {
	mux.HandleFunc(discoveryRoute, d.handleMetadataRoot)
	mux.HandleFunc(discoveryRouteSuffix, d.handleMetadataSuffix)
}

func (d DiscoveryConfig) handleMetadataRoot(w http.ResponseWriter, r *http.Request) {
	d.serveMetadata(w, r)
}

func (d DiscoveryConfig) handleMetadataSuffix(w http.ResponseWriter, r *http.Request) {
	if !d.Enabled() {
		http.NotFound(w, r)
		return
	}
	expected := metadataResourceSuffix(d.Resource)
	if expected == "" || r.PathValue("rest") != expected {
		http.NotFound(w, r)
		return
	}
	d.serveMetadata(w, r)
}

func (d DiscoveryConfig) serveMetadata(w http.ResponseWriter, r *http.Request) {
	if !d.Enabled() {
		// Uniform with handleMetadataSuffix: the standard NotFound body,
		// so every disabled discovery path refuses the same shape.
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", discoveryCacheControl)
	w.Header().Set("Content-Type", discoveryContentType)
	raw, err := json.Marshal(OAuthProtectedResourceMetadata{
		Resource:                  d.Resource,
		AuthorizationServers:      d.Issuers,
		BearerMethodsSupported:    []string{"header"},
		ScopesSupported:           d.ScopesSupported,
		ResourceName:              d.ResourceName,
		ResourceDocumentation:     d.ResourceDocumentation,
		EshuPreregisteredClientID: d.PreregisteredClientID,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(raw); err != nil {
		return
	}
}

// MetadataURLForResource validates the operator's ESHU_AUTH_RESOURCE_URI
// and derives the RFC 9728 section 3 protected-resource-metadata URL,
// mirroring the monolith's oauthMetadataURL exactly: the resource must be
// an absolute https URL — or an http URL whose host is loopback — with no
// query, no fragment, and no quote or control characters (raw or
// percent-decoded), because the value is embedded in a quoted-string
// WWW-Authenticate directive.
func MetadataURLForResource(resource string) (string, bool) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return "", false
	}
	for _, r := range resource {
		if r == '"' || r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	u, err := url.Parse(resource)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	for _, part := range []string{u.Host, u.Path} {
		for _, r := range part {
			if r == '"' || r < 0x20 || r == 0x7f {
				return "", false
			}
		}
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", false
		}
	default:
		return "", false
	}
	metadataURL := u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource"
	if suffix := strings.Trim(u.Path, "/"); suffix != "" {
		metadataURL += "/" + suffix
	}
	return metadataURL, true
}

// metadataResourceSuffix returns the RFC 9728 section 3 path suffix for a
// resource identifier: the resource URI's path with surrounding slashes
// trimmed. An unparseable or pathless resource yields no suffix, so every
// suffixed request 404s and only the root serves.
func metadataResourceSuffix(resource string) string {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return ""
	}
	u, err := url.Parse(resource)
	if err != nil {
		return ""
	}
	return strings.Trim(u.Path, "/")
}

// isLoopbackHost reports whether host is a loopback name or address, the
// only case where a plain-http resource URI is accepted.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// hasHeaderDelimiter reports whether s contains a double quote or an
// ASCII control character that would break out of a quoted HTTP header
// parameter value or split the header.
func hasHeaderDelimiter(s string) bool {
	for _, r := range s {
		if r == '"' || r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// CredentialSourceConfigured is the edge "no silent open mode" predicate,
// mirroring the monolith's authEnforcementConfigured: true when at least
// one of the three explicit operator credential knobs is set — the shared
// key, the scoped-token file, or the OIDC bearer [REDACTED] resource URI. The edge
// verifies no Bearer [REDACTED] itself (core admits every dispatch); the knobs
// declare that an identity-backed deployment stands behind this edge, so a
// headerless request must be denied rather than served open.
func CredentialSourceConfigured(apiKey, scopedTokensFile, resourceURI string) bool {
	return strings.TrimSpace(apiKey) != "" ||
		strings.TrimSpace(scopedTokensFile) != "" ||
		strings.TrimSpace(resourceURI) != ""
}
