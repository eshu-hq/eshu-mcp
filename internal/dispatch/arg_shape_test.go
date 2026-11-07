package dispatch

import (
	"strings"
	"testing"
)

// Required-argument wire shape (eshu-hq/eshu-mcp#61), dispatch side: the
// direct tools/call path has no schema gate, so a call that omits an
// argument the route cannot build without is refused here with a named
// error before any core call. This table pins the exact refusal for every
// such tool, including the anyOf-selector tools whose schemas deliberately
// stay open (get_repo_summary, the service trio, investigate_service): for
// those the refusal text IS the edge contract.

// TestResolveRouteRefusesMissingSelector proves each route-level refusal
// names the missing selector and never reaches core.
func TestResolveRouteRefusesMissingSelector(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"check_documentation_evidence_packet_freshness": "packet_id is required",
		"get_collector_extraction_readiness":            "family is required",
		"get_component_extension_diagnostics":           "component_id is required",
		"get_documentation_evidence_packet":             "finding_id is required",
		"get_entity_context":                            "entity_id is required",
		"get_fact_schema_version":                       "fact_kind is required",
		"get_incident_context":                          "provider_incident_id is required",
		"get_repo_context":                              "repo_id is required",
		"get_repo_story":                                "repo_id is required",
		"get_repo_summary":                              "repo_id or repo_name is required",
		"get_repository_coverage":                       "repo_id is required",
		"get_repository_freshness":                      "repo_id is required",
		"get_service_context":                           "get_service_context requires workload_id",
		"get_service_intelligence_report":               "get_service_intelligence_report requires service_id, workload_id, or service_name",
		"get_service_story":                             "get_service_story requires service_id, workload_id, or service_name",
		"get_workload_context":                          "workload_id is required",
		"get_workload_story":                            "workload_id is required",
		"investigate_service":                           "investigate_service requires service_id or service_name",
		"list_changed_since_poisoned_links":             "limit is required",
		"list_dead_letter_work_items":                   "limit is required",
		"list_reducer_input_invalid_facts":              "scope_id is required",
		"list_repository_files":                         "repo_id is required",
	}
	if len(cases) != 22 {
		t.Fatalf("refusal table has %d tools, want the 22 honest-error refusals", len(cases))
	}
	for tool, want := range cases {
		route, err := ResolveRoute(tool, map[string]any{})
		if err == nil {
			t.Errorf("ResolveRoute(%s, {}) = %+v, want the %q refusal", tool, route, want)
			continue
		}
		if err.Error() != want {
			t.Errorf("ResolveRoute(%s, {}) error = %q, want %q", tool, err.Error(), want)
		}
	}
}

// TestResolveCallChainRequiresTarget proves the conditional selector: a
// call_chain analysis without a start->end target or start/end entity ids
// is refused with the format error instead of reaching the handler with
// empty selectors.
func TestResolveCallChainRequiresTarget(t *testing.T) {
	t.Parallel()

	_, err := ResolveRoute("analyze_code_relationships",
		map[string]any{"query_type": "call_chain"})
	if err == nil || !strings.Contains(err.Error(), "call_chain target must use start->end format") {
		t.Fatalf("ResolveRoute(call_chain, no target) error = %v, want the target-format refusal", err)
	}
	route, err := ResolveRoute("analyze_code_relationships",
		map[string]any{"query_type": "call_chain", "target": "a->b"})
	if err != nil {
		t.Fatalf("ResolveRoute(call_chain, a->b) error = %v, want a route", err)
	}
	if route == nil || route.Path != "/api/v0/code/call-chain" {
		t.Fatalf("ResolveRoute(call_chain, a->b) = %+v, want the call-chain route", route)
	}
}

// TestDispatchAnchor400Passthrough proves the deliberate edge contract for
// the anchor-group tools: with no selector the dispatcher forwards the
// request and the core plain-text 400 renders as the historic "HTTP 400"
// error (not the invalid-arguments envelope, which the schema cannot
// express for an anyOf group).
func TestDispatchAnchor400Passthrough(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 400, body: `{"error":"Bad Request","detail":"scope_id, cluster_id, workload_object_id, namespace, image_ref, or source_digest is required"}`}
	_, err := dispatchAgainstStub(t, stub, "list_kubernetes_correlations", map[string]any{}, "", DefaultOptions())
	if err == nil || !strings.HasPrefix(err.Error(), "HTTP 400:") {
		t.Fatalf("Dispatch() error = %v, want the HTTP 400 passthrough text", err)
	}
	if !strings.Contains(err.Error(), "source_digest is required") {
		t.Errorf("Dispatch() error = %v, want the core anchor text", err)
	}
}
