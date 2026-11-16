// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdtools

import (
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// TestRouteKeepsRunIDFallback pins the eshu-hq/eshu-mcp#77 kept read: the
// listing route still resolves the older run_id spelling when
// provider_run_id is absent, empty, or wrong-typed, and still echoes run_id
// under its own key. The schema declares the alias; this test proves the
// route keeps honoring it, mirroring the source contract.
func TestRouteKeepsRunIDFallback(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		args          routecontract.Arguments
		wantProvider  string
		wantRunIDEcho string
	}{
		{name: "both present", args: routecontract.Arguments{"provider_run_id": "run-9", "run_id": "run-1"}, wantProvider: "run-9", wantRunIDEcho: "run-1"},
		{name: "only run_id", args: routecontract.Arguments{"run_id": "run-1"}, wantProvider: "run-1", wantRunIDEcho: "run-1"},
		{name: "empty provider_run_id", args: routecontract.Arguments{"provider_run_id": "", "run_id": "run-1"}, wantProvider: "run-1", wantRunIDEcho: "run-1"},
		{name: "wrong-typed provider_run_id", args: routecontract.Arguments{"provider_run_id": 9, "run_id": "run-1"}, wantProvider: "run-1", wantRunIDEcho: "run-1"},
	} {
		request, handled := Route("list_ci_cd_run_correlations", tt.args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		if got := request.Query["provider_run_id"]; got != tt.wantProvider {
			t.Errorf("%s: provider_run_id = %q, want %q", tt.name, got, tt.wantProvider)
		}
		if got := request.Query["run_id"]; got != tt.wantRunIDEcho {
			t.Errorf("%s: run_id = %q, want %q", tt.name, got, tt.wantRunIDEcho)
		}
	}
}
