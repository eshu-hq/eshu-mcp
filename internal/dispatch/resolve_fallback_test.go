// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dispatch

import "testing"

// TestResolveRouteKeepsIncidentIDFallback pins the eshu-hq/eshu-mcp#77
// kept read: when provider_incident_id is absent, ResolveRoute still
// resolves the older incident_id spelling into the incident path, exactly
// as the source does. The schema declares the alias; this test proves the
// route keeps honoring it.
func TestResolveRouteKeepsIncidentIDFallback(t *testing.T) {
	t.Parallel()

	route, err := ResolveRoute("get_incident_context", map[string]any{
		"incident_id": "inc-9",
	})
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Path, "/api/v0/incidents/inc-9/context"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
}
