package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/mcpserver"
	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// TestBuildServerAdmitsForwardedCredentialsThroughCore proves the production
// wiring, not just the middleware: with transport auth enforced, a credential
// the edge cannot verify reaches initialize, tools/list, and ping only when
// core admits it (eshu-hq/eshu-mcp#50), the shared key never costs a core
// round trip, and an unreachable core answers 503.
func TestBuildServerAdmitsForwardedCredentialsThroughCore(t *testing.T) {
	var probes atomic.Int64
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/capabilities" {
			t.Errorf("core saw %s %s, want only the admission probe", r.Method, r.URL.Path)
		}
		probes.Add(1)
		if r.Header.Get("Authorization") == "Bearer personal-ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="core"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	client, err := readcontract.NewClient(readcontract.DefaultConfig(core.URL, ""))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	server, extra, _ := buildServer(quietLogger(), client, core.URL, "deploy-key",
		mcpserver.DiscoveryConfig{}, true, router.Surface(""), resultshape.Mode(""), "")
	mux := server.Handler(extra)

	call := func(method, auth string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp/message",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{}}`))
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}

	for _, method := range []string{"initialize", "tools/list", "ping"} {
		for _, bad := range []string{"Bearer garbage", "Basic x"} {
			if got := call(method, bad); got != http.StatusUnauthorized {
				t.Errorf("%s with %q = %d, want 401", method, bad, got)
			}
		}
		if got := call(method, "Bearer personal-ok"); got != http.StatusOK {
			t.Errorf("%s with an admitted personal token = %d, want 200", method, got)
		}
	}

	before := probes.Load()
	if got := call("tools/list", "Bearer deploy-key"); got != http.StatusOK {
		t.Errorf("tools/list with the shared key = %d, want 200", got)
	}
	if probes.Load() != before {
		t.Error("the shared key cost a core probe, want none")
	}

	health := httptest.NewRecorder()
	mux.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body struct {
		Admission map[string]int64 `json:"admission"`
	}
	if err := json.Unmarshal(health.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /healthz: %v", err)
	}
	if body.Admission["admitted"] != 1 || body.Admission["denied"] != 2 || body.Admission["cache_hit"] == 0 {
		t.Errorf("/healthz admission = %v, want admitted 1, denied 2, and cache hits", body.Admission)
	}

	core.Close()
	if got := call("ping", "Bearer another-token"); got != http.StatusServiceUnavailable {
		t.Errorf("ping with core down = %d, want 503 fail-closed", got)
	}
}
