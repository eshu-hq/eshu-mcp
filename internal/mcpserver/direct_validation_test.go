package mcpserver

import (
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

// Direct tools/call validation (eshu-hq/eshu-mcp#86): the direct path now
// runs the same ValidateArguments the router applies to a call_tool target,
// so a schema-invalid direct call is refused with the structured
// mcp_router_invalid_arguments error naming the field, and a schema-valid
// call still dispatches.

// TestDirectCallValidatesArguments proves a direct call that violates the
// target's input schema (list_repository_files requires repo_id) is refused
// before dispatch, with the field_errors the call_tool path also produces.
func TestDirectCallValidatesArguments(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_repository_files","arguments":{}}}`,
		"Bearer k")
	result := mustMap(t, decoded["result"])
	if result["isError"] != true {
		t.Fatalf("direct tools/call isError = %v, want true for a schema-invalid call", result["isError"])
	}
	structured := mustMap(t, result["structuredContent"])
	failure := mustMap(t, structured["error"])
	if failure["code"] != "mcp_router_invalid_arguments" {
		t.Fatalf("direct tools/call code = %v, want mcp_router_invalid_arguments", failure["code"])
	}
	details := mustMap(t, failure["details"])
	if details["tool"] != "list_repository_files" {
		t.Errorf("direct tools/call details.tool = %v, want list_repository_files", details["tool"])
	}
	fieldErrors := mustSlice(t, details["field_errors"])
	found := false
	for _, raw := range fieldErrors {
		fe := mustMap(t, raw)
		if fe["path"] == "repo_id" && fe["keyword"] == "required" {
			found = true
		}
	}
	if !found {
		t.Errorf("direct tools/call field_errors = %v, want a repo_id required error", fieldErrors)
	}
	// The invalid-arguments result carries the same capped echo the call_tool
	// path serves.
	hint, ok := details["schema_hint"].(string)
	if !ok || !strings.Contains(hint, "find_tool") {
		t.Errorf("direct tools/call details.schema_hint = %v, want the find_tool pointer", details["schema_hint"])
	}
}

// TestDirectCallValidArgumentsStillDispatch proves a schema-valid direct call
// reaches core unchanged: validation admits it and the dispatch path runs.
func TestDirectCallValidArgumentsStillDispatch(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{"data":{},"truth":null,"error":null}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_repository_files","arguments":{"repo_id":"r1"}}}`,
		"Bearer k")
	result := mustMap(t, decoded["result"])
	if result["isError"] == true {
		t.Fatalf("direct tools/call isError = true, want the schema-valid call to dispatch: %v", result)
	}
}
