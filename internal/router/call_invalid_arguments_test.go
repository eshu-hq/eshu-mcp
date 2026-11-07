package router

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

// Required-argument wire shape (eshu-hq/eshu-mcp#61): a call_tool target
// call that omits an argument the handler unconditionally requires must
// answer mcp_router_invalid_arguments from router-side schema validation,
// never the core HTTP 400 passthrough text or a route-level refusal. These
// tests pin the canonical envelope for the tools whose declared schemas now
// carry the requirement, plus representatives of the already-canonical
// classes, and prove the served examples for the fixed tools validate.

func callMissingArgs(t *testing.T, engine *Engine, name string, args map[string]any) CallOutcome {
	t.Helper()

	called := false
	out := engine.Call(context.Background(), Request{Dispatch: func(context.Context, string, map[string]any) (any, error) {
		called = true
		return nil, nil
	}}, map[string]any{"name": name, "arguments": args})
	if out.Dispatched || called {
		t.Fatalf("Call(%s, %v) dispatched, want the invalid-arguments error", name, args)
	}
	return out
}

func invalidArgumentsBody(t *testing.T, out CallOutcome) string {
	t.Helper()

	if !out.Result.IsError {
		t.Fatal("Call() result isError = false, want true")
	}
	body, err := json.Marshal(out.Result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "mcp_router_invalid_arguments") {
		t.Fatalf("Call() result lacks the invalid-arguments code: %s", text)
	}
	return text
}

// TestCallMissingBlastRadiusTargetType proves the #61 schema fix: without
// target_type the call never reaches dispatch, and the field errors name
// the missing argument.
func TestCallMissingBlastRadiusTargetType(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	for label, args := range map[string]map[string]any{
		"empty":          {},
		"target only":    {"target": "repo:eshu"},
		"unknown target": {"target": "repo:eshu", "target_type": "starship"},
	} {
		text := invalidArgumentsBody(t, callMissingArgs(t, engine, "find_blast_radius", args))
		if !strings.Contains(text, "target_type") {
			t.Errorf("Call(%s) field errors lack target_type: %s", label, text)
		}
	}
}

// TestCallMissingScopeKind proves the #61 schema fix: without scope_kind
// compose_replatforming_plan never reaches dispatch.
func TestCallMissingScopeKind(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	text := invalidArgumentsBody(t, callMissingArgs(t, engine, "compose_replatforming_plan", map[string]any{}))
	if !strings.Contains(text, "scope_kind") {
		t.Errorf("Call() field errors lack scope_kind: %s", text)
	}
	text = invalidArgumentsBody(t, callMissingArgs(t, engine, "compose_replatforming_plan",
		map[string]any{"scope_id": "aws:123456789012:us-east-1:lambda"}))
	if !strings.Contains(text, "scope_kind") {
		t.Errorf("Call(anchor only) field errors lack scope_kind: %s", text)
	}
}

// TestCallFixedExamplesValidate proves the served examples for the two
// #61-fixed tools satisfy their own schemas, so the example handed back
// in the invalid-arguments details is itself a valid call.
func TestCallFixedExamplesValidate(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	for _, name := range []string{"find_blast_radius", "compose_replatforming_plan"} {
		example, ok := engine.Example(name)
		if !ok {
			t.Fatalf("Example(%s) missing", name)
		}
		definition, ok := registry.Definition(name)
		if !ok {
			t.Fatalf("Definition(%s) missing", name)
		}
		if errs := ValidateArguments(definition.InputSchema, example); len(errs) != 0 {
			t.Errorf("Example(%s) = %v, want schema-valid: %v", name, example, errs)
		}
	}
}

// TestCallMissingRequiredArgsIsInvalidArguments pins the already-canonical
// representatives: route-refusal tools (status family, repository files)
// and core-400 tools whose schemas already require the argument all answer
// the envelope without dispatching.
func TestCallMissingRequiredArgsIsInvalidArguments(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	for _, name := range []string{
		"list_dead_letter_work_items",
		"list_reducer_input_invalid_facts",
		"list_changed_since_poisoned_links",
		"list_repository_files",
		"count_repositories_by_language",
		"get_repo_context",
		"get_service_context",
		"get_fact_schema_version",
		"find_change_surface",
		"trace_deployment_chain",
	} {
		invalidArgumentsBody(t, callMissingArgs(t, engine, name, map[string]any{}))
	}
}

// TestCallSelectorAlternativesDispatch proves the deliberate edge contract:
// tools whose requirement is an anyOf anchor group the schema cannot
// express must NOT be rejected by router validation. The missing selector
// fails later at the handler (or the route refusal for path selectors),
// exactly as the amended tool descriptions state.
func TestCallSelectorAlternativesDispatch(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	cases := map[string]map[string]any{
		"get_repo_summary":                                {},
		"get_service_story":                               {},
		"get_service_intelligence_report":                 {},
		"investigate_service":                             {},
		"search_registry_bundles":                         {},
		"search_php_configuration":                        {},
		"get_code_relationship_story":                     {},
		"analyze_pre_change_impact":                       {},
		"plan_developer_change":                           {},
		"find_unmanaged_resources":                        {},
		"list_cloud_runtime_drift_findings":               {},
		"export_cloud_runtime_drift_packet":               {},
		"list_kubernetes_correlations":                    {},
		"find_function_call_chain":                        {},
		"find_dead_iac":                                   {},
		"investigate_contract_impact":                     {},
		"investigate_change_surface":                      {},
		"investigate_deployment_config":                   {},
		"investigate_resource":                            {},
		"list_supply_chain_impact_findings":               {},
		"explain_supply_chain_impact":                     {},
		"list_container_image_identities":                 {},
		"list_observability_coverage_correlations":        {},
		"list_package_registry_correlations":              {},
		"list_package_registry_dependencies":              {},
		"list_ci_cd_run_correlations":                     {},
		"list_service_catalog_correlations":               {},
		"list_advisory_evidence":                          {},
		"get_sbom_attestation_attachment_inventory":       {},
		"list_sbom_attestation_attachments":               {},
		"list_security_alert_reconciliations":             {},
		"list_secrets_iam_identity_trust_chains":          {},
		"list_secrets_iam_privilege_posture_observations": {},
		"list_secrets_iam_secret_access_paths":            {},
		"list_secrets_iam_posture_gaps":                   {},
		"list_work_item_evidence":                         {},
		"list_semantic_code_hints":                        {},
		"list_semantic_documentation_observations":        {},
		"trace_exposure_path":                             {},
		"analyze_code_relationships":                      {"query_type": "find_callers"},
		"list_package_registry_packages":                  {"limit": 50},
	}
	for name, args := range cases {
		out := engine.Call(context.Background(), Request{Dispatch: testDispatch(nil)},
			map[string]any{"name": name, "arguments": args})
		if !out.Dispatched {
			body, marshalErr := json.Marshal(out.Result.StructuredContent)
			if marshalErr != nil {
				t.Fatalf("marshal structured content: %v", marshalErr)
			}
			t.Errorf("Call(%s, %v) refused by validation, want dispatch to the handler: %s", name, args, body)
		}
	}
}
