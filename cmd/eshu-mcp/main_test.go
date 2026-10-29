package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

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
	err := run([]string{"--core-base-url", "https://core:8443"}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "credential posture") {
		t.Fatalf("run() error = %v, want the no-credential-posture refusal", err)
	}
}

func TestRunRefusesRouterSurfaceWithoutRanker(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "test-key")
	t.Setenv("ESHU_MCP_TOOL_SURFACE", "router")
	t.Setenv("ESHU_MCP_ALLOW_UNAUTHENTICATED", "")
	err := run([]string{"--core-base-url", "https://core:8443"}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "ranker") {
		t.Fatalf("run() error = %v, want the ranker-gate refusal", err)
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
	server, extra, summary := buildServer(quietLogger(), "https://core:8443", "deploy-key", router.Surface(""), resultshape.Mode(""), "")
	if summary.surface != router.SurfaceFull {
		t.Errorf("surface = %q, want the full default", summary.surface)
	}
	if summary.advertised != 169 {
		t.Errorf("advertised = %d, want the 169-tool registry", summary.advertised)
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
	_, _, summary := buildServer(quietLogger(), "https://core:8443", "deploy-key", router.Surface(""), resultshape.Mode(""), "production")
	if summary.advertised != 169-4 {
		t.Errorf("production advertised = %d, want 169 minus the 4 hidden tools", summary.advertised)
	}
}
