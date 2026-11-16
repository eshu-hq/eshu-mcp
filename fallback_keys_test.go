// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import "testing"

// TestIncidentAndRunFallbackKeysDeclared pins the eshu-hq/eshu-mcp#77
// declare decision: the route fallback aliases incident_id (for
// provider_incident_id) and run_id (for provider_run_id) are declared
// input-schema properties, so a future closed-world call_tool validation
// keeps accepting the working calls the routes honor. The canonical keys
// stay the required ones; the aliases are optional.
func TestIncidentAndRunFallbackKeysDeclared(t *testing.T) {
	t.Parallel()

	incident, ok := Definition("get_incident_context")
	if !ok {
		t.Fatal("Definition(get_incident_context) missing")
	}
	assertDeclaresOptional(t, incident, "incident_id")
	assertRequired(t, "get_incident_context", []string{"provider_incident_id"})

	runs, ok := Definition("list_ci_cd_run_correlations")
	if !ok {
		t.Fatal("Definition(list_ci_cd_run_correlations) missing")
	}
	assertDeclaresOptional(t, runs, "run_id")
	assertRequired(t, "list_ci_cd_run_correlations", nil)
}

// assertDeclaresOptional requires the named property to exist without
// becoming a required argument.
func assertDeclaresOptional(t *testing.T, tool ToolDefinition, property string) {
	t.Helper()

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("Definition(%s) InputSchema is %T, want map[string]any", tool.Name, tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Definition(%s) properties is %T, want map[string]any", tool.Name, schema["properties"])
	}
	if _, ok := properties[property]; !ok {
		t.Errorf("Definition(%s) does not declare %q, want the route fallback alias declared", tool.Name, property)
	}
	for _, name := range RequiredArguments(tool.Name) {
		if name == property {
			t.Errorf("Definition(%s) requires %q, want the fallback alias optional", tool.Name, property)
		}
	}
}

// assertRequired requires the tool's required-argument set to match exactly.
func assertRequired(t *testing.T, tool string, want []string) {
	t.Helper()

	got := RequiredArguments(tool)
	if len(got) != len(want) {
		t.Fatalf("RequiredArguments(%s) = %v, want %v", tool, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RequiredArguments(%s) = %v, want %v", tool, got, want)
		}
	}
}
