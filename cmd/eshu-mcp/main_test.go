package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/mcpserver"
	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// testClient builds the core client buildServer takes, or fails the test.
func testClient(t *testing.T) *readcontract.Client {
	t.Helper()
	client, err := readcontract.NewClient(readcontract.DefaultConfig("https://core:8443", ""))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunRequiresCoreBaseURL(t *testing.T) {
	t.Parallel()

	err := run([]string{}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "--core-base-url") {
		t.Fatalf("run() error = %v, want a missing --core-base-url error", err)
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	t.Parallel()

	err := run([]string{"--no-such-flag"}, quietLogger())
	if err == nil {
		t.Fatal("run() succeeded with an unknown flag, want the error")
	}
}

func TestRunRefusesBootWithoutCredentialPosture(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_MCP_ALLOW_UNAUTHENTICATED", "")
	t.Setenv("ESHU_SCOPED_TOKENS_FILE", "")
	t.Setenv("ESHU_AUTH_RESOURCE_URI", "")
	err := run([]string{"--core-base-url", "https://core:8443"}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "credential posture") {
		t.Fatalf("run() error = %v, want the no-credential-posture refusal", err)
	}
}

// e2eEnvGetter builds the getenv wireServer reads, so the tests below
// drive the exact boot path run serves: a wiring break fails these tests
// rather than hiding behind the unit-tested predicate.
func e2eEnvGetter(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

// TestWireServerE2EEnvBootsAndServesDiscovery replays the auth E2E stacks
// through the real boot path (eshu-hq/eshu-mcp#27): empty shared key, no
// escape hatch, resource URI plus mock issuer set. The monolith booted
// there, so wireServer must wire — not refuse — and serve the discovery
// the scripted OAuth flow needs.
func TestWireServerE2EEnvBootsAndServesDiscovery(t *testing.T) {
	getenv := e2eEnvGetter(map[string]string{
		"ESHU_API_KEY":                    "",
		"ESHU_MCP_ALLOW_UNAUTHENTICATED":  "",
		"ESHU_SCOPED_TOKENS_FILE":         "",
		"ESHU_AUTH_RESOURCE_URI":          "http://127.0.0.1:8080",
		"ESHU_AUTH_AUTHORIZATION_SERVERS": "http://mock-oidc-idp:8080",
	})
	_, server, extra, err := wireServer([]string{"--core-base-url", "https://core:8443"}, getenv, quietLogger())
	if err != nil {
		t.Fatalf("wireServer() error = %v, want the E2E-env boot to wire", err)
	}
	mux := server.Handler(extra)

	// The scripted OAuth flow starts here: credential-less initialize must
	// 401 with the resource_metadata challenge, not the bare Bearer.
	anon := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`))
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("headerless initialize = %d, want 401", anonRec.Code)
	}
	challenge := anonRec.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `resource_metadata="http://127.0.0.1:8080/.well-known/oauth-protected-resource"`) {
		t.Errorf("challenge = %q, want the metadata directive", challenge)
	}

	// Step two of the flow: the metadata document serves the resource and
	// the mock issuer.
	docReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	docRec := httptest.NewRecorder()
	mux.ServeHTTP(docRec, docReq)
	if docRec.Code != http.StatusOK {
		t.Fatalf("GET discovery root = %d, want 200", docRec.Code)
	}
	body := docRec.Body.String()
	if !strings.Contains(body, `"resource":"http://127.0.0.1:8080"`) {
		t.Errorf("discovery document resource wrong: %s", body)
	}
	if !strings.Contains(body, `"authorization_servers":["http://mock-oidc-idp:8080"]`) {
		t.Errorf("discovery document issuers wrong: %s", body)
	}
}

// TestWireServerScopedFileOnlyDeniesHeaderless pins the fail-open fix:
// a scoped-token file alone is a credential source, so the wired edge
// must deny a headerless request (401, bare challenge, no discovery)
// rather than serve open.
func TestWireServerScopedFileOnlyDeniesHeaderless(t *testing.T) {
	getenv := e2eEnvGetter(map[string]string{
		"ESHU_SCOPED_TOKENS_FILE": "/run/secrets/tokens.json",
	})
	_, server, extra, err := wireServer([]string{"--core-base-url", "https://core:8443"}, getenv, quietLogger())
	if err != nil {
		t.Fatalf("wireServer() error = %v, want the scoped-file boot to wire", err)
	}
	mux := server.Handler(extra)
	anon := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("scoped-file-only headerless call = %d, want 401", anonRec.Code)
	}
	if challenge := anonRec.Header().Get("WWW-Authenticate"); challenge != mcpserver.BareChallenge {
		t.Errorf("challenge = %q, want the bare challenge with no discovery", challenge)
	}
	docRec := httptest.NewRecorder()
	mux.ServeHTTP(docRec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if docRec.Code != http.StatusNotFound {
		t.Errorf("GET discovery without URI = %d, want 404", docRec.Code)
	}
}

// TestWireServerRefusesBootWithoutPosture proves the refusal at the wire
// level too, so a gate regression fails here even if run() changes shape.
func TestWireServerRefusesBootWithoutPosture(t *testing.T) {
	getenv := e2eEnvGetter(map[string]string{})
	_, _, _, err := wireServer([]string{"--core-base-url", "https://core:8443"}, getenv, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "credential posture") {
		t.Fatalf("wireServer() error = %v, want the no-credential-posture refusal", err)
	}
}

// TestBuildServerAuthEnforcedFlag pins the flag semantics directly: false
// serves a headerless request open (escape-hatch-open with no source),
// true denies it.
func TestBuildServerAuthEnforcedFlag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enforced bool
		want     int
	}{
		{"open", false, http.StatusOK},
		{"enforced", true, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, extra, _ := buildServer(quietLogger(), testClient(t), "https://core:8443", "", mcpserver.DiscoveryConfig{}, tc.enforced, router.Surface(""), resultshape.Mode(""), "")
			rec := httptest.NewRecorder()
			server.Handler(extra).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
			if rec.Code != tc.want {
				t.Errorf("headerless call = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestRunRefusesBadSurfaceAndShape(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "test-key")
	t.Setenv("ESHU_MCP_TOOL_SURFACE", "bogus")
	err := run([]string{"--core-base-url", "https://core:8443"}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "ESHU_MCP_TOOL_SURFACE") {
		t.Fatalf("run() error = %v, want the surface error", err)
	}

	t.Setenv("ESHU_MCP_TOOL_SURFACE", "")
	t.Setenv("ESHU_MCP_RESULT_SHAPE", "bogus")
	err = run([]string{"--core-base-url", "https://core:8443"}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "ESHU_MCP_RESULT_SHAPE") {
		t.Fatalf("run() error = %v, want the shape error", err)
	}
}

func TestRunRefusesNonHTTPTransport(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "test-key")
	t.Setenv("ESHU_MCP_TRANSPORT", "stdio")
	err := run([]string{"--core-base-url", "https://core:8443"}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("run() error = %v, want the transport refusal", err)
	}
}

func TestIsTruthy(t *testing.T) {
	t.Parallel()

	for _, truthy := range []string{"1", "true", "t", "yes", "y", "on", " TRUE "} {
		if !isTruthy(truthy) {
			t.Errorf("isTruthy(%q) = false, want true", truthy)
		}
	}
	for _, falsy := range []string{"", "0", "false", "no", "dev"} {
		if isTruthy(falsy) {
			t.Errorf("isTruthy(%q) = true, want false", falsy)
		}
	}
}

func TestBuildServerWiring(t *testing.T) {
	server, extra, summary := buildServer(quietLogger(), testClient(t), "https://core:8443", "deploy-key", mcpserver.DiscoveryConfig{}, true, router.Surface(""), resultshape.Mode(""), "")
	if summary.surface != router.SurfaceFull {
		t.Errorf("surface = %q, want the full default", summary.surface)
	}
	if summary.advertised != 171 {
		t.Errorf("advertised = %d, want the 171-tool registry", summary.advertised)
	}
	if summary.listBytes <= 0 {
		t.Error("tools/list bytes is not positive")
	}
	mux := server.Handler(extra)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
	// The edge requires the credential even though construction succeeded.
	anon := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Errorf("headerless tools/call = %d, want 401", anonRec.Code)
	}
}

func TestBuildServerProductionProfile(t *testing.T) {
	_, _, summary := buildServer(quietLogger(), testClient(t), "https://core:8443", "deploy-key", mcpserver.DiscoveryConfig{}, true, router.Surface(""), resultshape.Mode(""), "production")
	if summary.advertised != 171-4 {
		t.Errorf("production advertised = %d, want 171 minus the 4 hidden tools", summary.advertised)
	}
}

// TestBuildServerReportsBuildVersion pins the release contract: the build
// version baked with -ldflags "-X main.version=..." (the Dockerfile VERSION
// arg the release workflow sets) is the version the initialize handshake
// advertises. A release that reports "dev" was not stamped.
func TestBuildServerReportsBuildVersion(t *testing.T) {
	const stamped = "v0.0.0-9521-test"
	previous := version
	version = stamped
	defer func() { version = previous }()

	server, extra, _ := buildServer(quietLogger(), testClient(t), "https://core:8443", "deploy-key", mcpserver.DiscoveryConfig{}, true, router.Surface(""), resultshape.Mode(""), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp/message",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`))
	req.Header.Set("Authorization", "Bearer deploy-key")
	rec := httptest.NewRecorder()
	server.Handler(extra).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /mcp/message initialize = %d, want 200", rec.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	result, ok := decoded["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize response has no result: %v", decoded)
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result has no serverInfo: %v", result)
	}
	if info["version"] != stamped {
		t.Errorf("serverInfo.version = %v, want the stamped build %q", info["version"], stamped)
	}
}

// repoRoot resolves the repository root from this test file so contract
// guards below can read the Dockerfile and release workflow hermetically.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// TestReleaseStampPlumbingGuardsVersionArg pins the release-stamping link
// the handshake test above cannot see: the Dockerfile must consume a
// VERSION build arg into -ldflags "-X main.version=...", and release.yml
// must pass that VERSION build-arg to the Docker build. Renaming the
// Dockerfile ARG without updating the workflow (or dropping the build-arg)
// silently ships "dev" binaries while TestBuildServerReportsBuildVersion
// stays green, so this guard fails that mutant.
func TestReleaseStampPlumbingGuardsVersionArg(t *testing.T) {
	t.Parallel()

	dockerfile, err := os.ReadFile(filepath.Join(repoRoot(t), "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	for _, want := range []string{"ARG VERSION=", "-X main.version=${VERSION}"} {
		if !strings.Contains(string(dockerfile), want) {
			t.Errorf("Dockerfile has no %q: the VERSION build-arg no longer reaches the binary", want)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	if !strings.Contains(string(workflow), "VERSION=${{") {
		t.Error("release.yml passes no VERSION=${{...}} build-arg: the Dockerfile stamp is unset")
	}
}

// TestReleaseWorkflowGatesPushOnTrivyScan pins the P1 gate order: Trivy
// must scan the local pre-push image, and the push must live in a job that
// needs the scan job, so a failed scan leaves nothing pullable in GHCR.
func TestReleaseWorkflowGatesPushOnTrivyScan(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	body := string(raw)

	if strings.Contains(body, "image-ref: ${{ env.REGISTRY }}") {
		t.Error("Trivy scans a registry image: the scan must run against the local pre-push image")
	}
	if !strings.Contains(body, "needs: [build-and-scan]") {
		t.Error("no job needs build-and-scan: the push must wait for the Trivy gate")
	}
	if strings.Contains(body, "push: ${{ github.ref_type") {
		t.Error("conditional push before the scan: the push must run only after the Trivy gate passes")
	}
}
