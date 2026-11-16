package mcpserver

import (
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

// TestDirectSelectorRefusalIsInvalidArguments proves the #73 missing-selector
// fix on the direct tools/call path: a call that passes schema validation but
// names no usable selector still answers the structured
// mcp_router_invalid_arguments error naming the selector fields, through the
// same ToolErrorResultForCall rendering the routed call_tool path shares.
// get_service_story is used because its schema requires no single selector,
// so the direct-path validation (eshu-hq/eshu-mcp#86) admits it and the route
// refusal still fires; list_repository_files now fails validation first
// (repo_id is required), covered by TestDirectCallValidatesArguments.
func TestDirectSelectorRefusalIsInvalidArguments(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_service_story","arguments":{}}}`,
		"Bearer k")
	result := mustMap(t, decoded["result"])
	if result["isError"] != true {
		t.Fatalf("direct tools/call isError = %v, want true", result["isError"])
	}
	content := mustSlice(t, result["content"])
	if len(content) == 0 {
		t.Fatal("direct tools/call content is empty, want the refusal text")
	}
	block := mustMap(t, content[0])
	text, ok := block["text"].(string)
	if !ok || !strings.Contains(text, "get_service_story requires service_id, workload_id, or service_name") {
		t.Errorf("direct tools/call text = %q, want the historic refusal", text)
	}
	structured := mustMap(t, result["structuredContent"])
	failure := mustMap(t, structured["error"])
	if failure["code"] != "mcp_router_invalid_arguments" {
		t.Errorf("direct tools/call code = %v, want mcp_router_invalid_arguments", failure["code"])
	}
	if failure["message"] != "get_service_story requires service_id, workload_id, or service_name" {
		t.Errorf("direct tools/call message = %v, want the historic refusal", failure["message"])
	}
	details := mustMap(t, failure["details"])
	if details["tool"] != "get_service_story" {
		t.Errorf("direct tools/call details.tool = %v, want get_service_story", details["tool"])
	}
	fields, ok := details["fields"].([]any)
	if !ok || len(fields) != 3 || fields[0] != "service_id" || fields[1] != "workload_id" || fields[2] != "service_name" {
		t.Errorf("direct tools/call details.fields = %v, want [service_id workload_id service_name]", details["fields"])
	}
}
