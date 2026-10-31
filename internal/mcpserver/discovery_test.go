package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func envGetter(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestCredentialSourceConfiguredKnobs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                            string
		apiKey, scopedFile, resourceURI string
		want                            bool
	}{
		{"none", "", "", "", false},
		{"shared key", "deploy-key", "", "", true},
		{"scoped tokens file", "", "/run/secrets/tokens.json", "", true},
		{"resource URI", "", "", "https://mcp.example.test", true},
		{"loopback resource URI", "", "", "http://127.0.0.1:8080", true},
		{"all three", "k", "/f", "https://mcp.example.test", true},
		{"whitespace is empty", "  ", "\t", " ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CredentialSourceConfigured(tc.apiKey, tc.scopedFile, tc.resourceURI); got != tc.want {
				t.Errorf("CredentialSourceConfigured(%q, %q, %q) = %v, want %v",
					tc.apiKey, tc.scopedFile, tc.resourceURI, got, tc.want)
			}
		})
	}
}

func TestMetadataURLForResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		resource string
		wantURL  string
		wantOK   bool
	}{
		{"empty", "", "", false},
		{"https root", "https://mcp.example.test", "https://mcp.example.test/.well-known/oauth-protected-resource", true},
		{"https path", "https://mcp.example.test/mcp", "https://mcp.example.test/.well-known/oauth-protected-resource/mcp", true},
		{"loopback http", "http://127.0.0.1:8080", "http://127.0.0.1:8080/.well-known/oauth-protected-resource", true},
		{"localhost http", "http://localhost:8080/mcp", "http://localhost:8080/.well-known/oauth-protected-resource/mcp", true},
		{"public http rejected", "http://mcp.example.test", "", false},
		{"ftp rejected", "ftp://mcp.example.test", "", false},
		{"gopher rejected", "gopher://mcp.example.test/x", "", false},
		{"no scheme", "mcp.example.test", "", false},
		{"query rejected", "https://mcp.example.test/?a=b", "", false},
		{"fragment rejected", "https://mcp.example.test/#f", "", false},
		{"quote rejected", "https://mcp.example.test/mcp\"x", "", false},
		{"control rejected", "https://mcp.example.test/mcp\x01", "", false},
		{"encoded quote rejected", "https://mcp.example.test/mcp%22x", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := MetadataURLForResource(tc.resource)
			if ok != tc.wantOK || got != tc.wantURL {
				t.Errorf("MetadataURLForResource(%q) = (%q, %v), want (%q, %v)",
					tc.resource, got, ok, tc.wantURL, tc.wantOK)
			}
		})
	}
}

func enabledConfig() DiscoveryConfig {
	return DiscoveryConfigFromEnv(envGetter(map[string]string{
		ResourceURIEnv:          "https://mcp.example.test",
		AuthorizationServersEnv: "https://idp.example.test",
	}))
}

func TestDiscoveryEnabledGate(t *testing.T) {
	t.Parallel()

	if !enabledConfig().Enabled() {
		t.Error("valid URI plus one issuer reports disabled")
	}
	// No issuer: the challenge must never point a client at a 404.
	noIssuer := DiscoveryConfigFromEnv(envGetter(map[string]string{
		ResourceURIEnv: "https://mcp.example.test",
	}))
	if noIssuer.Enabled() {
		t.Error("zero issuers reports enabled")
	}
	// Invalid URI: discovery stays off while Bearer [REDACTED] still enforces.
	badURI := DiscoveryConfigFromEnv(envGetter(map[string]string{
		ResourceURIEnv:          "http://mcp.example.test",
		AuthorizationServersEnv: "https://idp.example.test",
	}))
	if badURI.Enabled() {
		t.Error("non-loopback http URI reports enabled")
	}
}

func TestChallengeValue(t *testing.T) {
	t.Parallel()

	if got := (DiscoveryConfig{}).ChallengeValue(); got != BareChallenge {
		t.Errorf("disabled challenge = %q, want bare %q", got, BareChallenge)
	}
	got := enabledConfig().ChallengeValue()
	want := `Bearer resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource", scope="openid profile email groups"`
	if got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}
	// Defense in depth: a metadata URL carrying a delimiter degrades to
	// the bare challenge rather than emitting a broken header value.
	poisoned := DiscoveryConfig{
		Resource:    "https://mcp.example.test",
		Issuers:     []string{"https://idp.example.test"},
		MetadataURL: "https://mcp.example.test/\x7f",
	}
	if !poisoned.Enabled() {
		t.Fatal("poisoned config reports disabled, want enabled so the degrade path runs")
	}
	if got := poisoned.ChallengeValue(); got != BareChallenge {
		t.Errorf("poisoned challenge = %q, want bare %q", got, BareChallenge)
	}
}

func TestMetadataResourceSuffix(t *testing.T) {
	t.Parallel()

	if got := metadataResourceSuffix(""); got != "" {
		t.Errorf("empty resource suffix = %q, want empty", got)
	}
	if got := metadataResourceSuffix("https://mcp.example.test/mcp"); got != "mcp" {
		t.Errorf("pathed suffix = %q, want mcp", got)
	}
	if got := metadataResourceSuffix("https://mcp.example.test"); got != "" {
		t.Errorf("root suffix = %q, want empty", got)
	}
	if got := metadataResourceSuffix("://unparseable"); got != "" {
		t.Errorf("unparseable suffix = %q, want empty", got)
	}
}

func TestHasHeaderDelimiter(t *testing.T) {
	t.Parallel()

	for _, s := range []string{`"`, "\x01", "\x7f", "a\"b"} {
		if !hasHeaderDelimiter(s) {
			t.Errorf("hasHeaderDelimiter(%q) = false, want true", s)
		}
	}
	if hasHeaderDelimiter("Bearer") {
		t.Error("hasHeaderDelimiter(Bearer) = true, want false")
	}
}

// failWriter is an http.ResponseWriter whose body Write always fails, to
// prove the discovery handler surfaces a write error instead of a silent
// 200 with no body.
type failWriter struct {
	header http.Header
	status int
}

func (f *failWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}

func (f *failWriter) Write([]byte) (int, error) { return 0, errFailWrite }

func (f *failWriter) WriteHeader(status int) { f.status = status }

type failWriteError string

func (e failWriteError) Error() string { return string(e) }

var errFailWrite error = failWriteError("test write failure")

func TestDiscoveryWriteErrorSurfaces(t *testing.T) {
	t.Parallel()

	fw := &failWriter{}
	enabledConfig().serveMetadata(fw, nil)
	// The handler attempted the 200 then hit the failing body write: the
	// status must be the success attempt, not a silent unreported failure.
	if fw.status != http.StatusOK {
		t.Errorf("status = %d, want the attempted 200 before the write failed", fw.status)
	}
}

func TestDiscoveryDocumentShape(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	enabledConfig().Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET discovery root = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want the 60s public pin", cc)
	}
	var doc OAuthProtectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode discovery document: %v", err)
	}
	if doc.Resource != "https://mcp.example.test" {
		t.Errorf("resource = %q", doc.Resource)
	}
	if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://idp.example.test" {
		t.Errorf("authorization_servers = %v", doc.AuthorizationServers)
	}
	if len(doc.BearerMethodsSupported) != 1 || doc.BearerMethodsSupported[0] != "header" {
		t.Errorf("bearer_methods_supported = %v", doc.BearerMethodsSupported)
	}
	if strings.Join(doc.ScopesSupported, " ") != DefaultOAuthChallengeScope {
		t.Errorf("scopes_supported = %v", doc.ScopesSupported)
	}
	if doc.ResourceName != "Eshu MCP Server" {
		t.Errorf("resource_name = %q", doc.ResourceName)
	}
	// Field order matches core's struct: resource first, extension last.
	raw := rec.Body.String()
	order := []string{`"resource"`, `"authorization_servers"`, `"bearer_methods_supported"`, `"scopes_supported"`, `"resource_name"`}
	last := -1
	for _, key := range order {
		at := strings.Index(raw, key)
		if at < 0 || at < last {
			t.Fatalf("document field order breaks at %s in %s", key, raw)
		}
		last = at
	}
}

func TestDiscoveryRoutes404(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	enabledConfig().Mount(mux)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource/other",
		"/.well-known/oauth-protected-resource/MCP",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	// A pathed resource serves its exact suffix and nothing else.
	pathed := DiscoveryConfigFromEnv(envGetter(map[string]string{
		ResourceURIEnv:          "https://mcp.example.test/mcp",
		AuthorizationServersEnv: "https://idp.example.test",
	}))
	pathedMux := http.NewServeMux()
	pathed.Mount(pathedMux)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil)
	rec := httptest.NewRecorder()
	pathedMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET exact suffix = %d, want 200", rec.Code)
	}

	// Disabled config: every discovery route 404s.
	off := http.NewServeMux()
	(DiscoveryConfig{}).Mount(off)
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s disabled = %d, want 404", path, rec.Code)
		}
	}
}
