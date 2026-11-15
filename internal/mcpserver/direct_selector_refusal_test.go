package mcpserver

import (
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

// TestDirectSelectorRefusalIsInvalidArguments proves the #73 missing-selector
// fix on the direct tools/call path, which never validates: a selectorless
// list_repository_files call still answers the structured
// mcp_router_invalid_arguments error naming repo_id, through the same
// ToolErrorResultForCall rendering the routed call_tool path shares.
func TestDirectSelectorRefusalIsInvalidArguments(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_repository_files","arguments":{}}}`,
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
	if !ok || !strings.Contains(text, "repo_id is required") {
		t.Errorf("direct tools/call text = %q, want the historic refusal", text)
	}
	structured := mustMap(t, result["structuredContent"])
	failure := mustMap(t, structured["error"])
	if failure["code"] != "mcp_router_invalid_arguments" {
		t.Errorf("direct tools/call code = %v, want mcp_router_invalid_arguments", failure["code"])
	}
	if failure["message"] != "repo_id is required" {
		t.Errorf("direct tools/call message = %v, want the historic refusal", failure["message"])
	}
	details := mustMap(t, failure["details"])
	if details["tool"] != "list_repository_files" {
		t.Errorf("direct tools/call details.tool = %v, want list_repository_files", details["tool"])
	}
	fields, ok := details["fields"].([]any)
	if !ok || len(fields) != 1 || fields[0] != "repo_id" {
		t.Errorf("direct tools/call details.fields = %v, want [repo_id]", details["fields"])
	}
}
