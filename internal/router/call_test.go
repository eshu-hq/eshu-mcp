package router

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

// testEngine builds an Engine over the seeded contract.
func testEngine() *Engine {
	return NewEngine(Config{
		Registry: registry.Definitions(),
		Resolve:  nil,
	})
}

func testDispatch(value any) Dispatcher {
	return func(context.Context, string, map[string]any) (any, error) {
		return value, nil
	}
}

// TestCallDispatchesRegisteredTool proves a schema-valid call_tool reaches
// the Dispatcher with the caller's own argument map, untouched.
func TestCallDispatchesRegisteredTool(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	sent := map[string]any{"language": "go"}
	out := engine.Call(context.Background(), Request{Dispatch: testDispatch("rendered")},
		map[string]any{"name": "count_repositories_by_language", "arguments": sent})
	if !out.Dispatched {
		t.Fatalf("Call() dispatched = false, result = %+v", out.Result)
	}
	if out.Value != "rendered" {
		t.Errorf("Call() value = %v, want the Dispatcher value untouched", out.Value)
	}
	if out.Target != "count_repositories_by_language" {
		t.Errorf("Call() target = %q, want the requested tool", out.Target)
	}
}

// TestCallRefusesRouterTargets proves find_tool, call_tool, and ask cannot
// run through call_tool, with the recursive-target reason.
func TestCallRefusesRouterTargets(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	for _, name := range []string{"find_tool", "call_tool", "ask"} {
		out := engine.Call(context.Background(), Request{Dispatch: testDispatch(nil)},
			map[string]any{"name": name, "arguments": map[string]any{}})
		if out.Dispatched {
			t.Errorf("Call(%q) dispatched, want the recursive-target refusal", name)
			continue
		}
		if !out.Result.IsError {
			t.Errorf("Call(%q) isError = false, want true", name)
		}
		body, marshalErr := json.Marshal(out.Result.StructuredContent)
		if marshalErr != nil {
			t.Fatalf("marshal structured content: %v", marshalErr)
		}
		if !strings.Contains(string(body), "recursive_target") {
			t.Errorf("Call(%q) result lacks the recursive-target reason: %s", name, body)
		}
	}
}

// TestCallUnknownToolSuggests proves an unknown name answers
// mcp_router_unknown_tool with the closest registered names.
func TestCallUnknownToolSuggests(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	out := engine.Call(context.Background(), Request{Dispatch: testDispatch(nil)},
		map[string]any{"name": "find_cod", "arguments": map[string]any{}})
	if out.Dispatched {
		t.Fatal("Call() dispatched an unknown tool, want the unknown-tool error")
	}
	body, marshalErr := json.Marshal(out.Result.StructuredContent)
	if marshalErr != nil {
		t.Fatalf("marshal structured content: %v", marshalErr)
	}
	text := string(body)
	if !strings.Contains(text, "mcp_router_unknown_tool") {
		t.Errorf("Call() result lacks the unknown-tool code: %s", text)
	}
	if !strings.Contains(text, "find_code") {
		t.Errorf("Call() result lacks the find_code suggestion: %s", text)
	}
}

// TestCallValidatesTargetArguments proves a schema-violating target call
// answers mcp_router_invalid_arguments with field errors, the input schema,
// and an example, and never reaches the Dispatcher.
func TestCallValidatesTargetArguments(t *testing.T) {
	t.Parallel()

	called := false
	engine := testEngine()
	out := engine.Call(context.Background(), Request{Dispatch: func(context.Context, string, map[string]any) (any, error) {
		called = true
		return nil, nil
	}}, map[string]any{"name": "count_repositories_by_language", "arguments": map[string]any{}})
	if out.Dispatched || called {
		t.Fatal("Call() dispatched schema-invalid arguments, want the invalid-arguments error")
	}
	body, marshalErr := json.Marshal(out.Result.StructuredContent)
	if marshalErr != nil {
		t.Fatalf("marshal structured content: %v", marshalErr)
	}
	text := string(body)
	for _, want := range []string{"mcp_router_invalid_arguments", "field_errors", "input_schema", "example"} {
		if !strings.Contains(text, want) {
			t.Errorf("Call() result lacks %q: %s", want, text)
		}
	}
}

// TestCallRejectsLongName proves an over-long tool name is rejected with a
// fixed error that does not echo it.
func TestCallRejectsLongName(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	long := strings.Repeat("x", MaxToolNameBytes+1)
	out := engine.Call(context.Background(), Request{Dispatch: testDispatch(nil)},
		map[string]any{"name": long, "arguments": map[string]any{}})
	if out.Dispatched {
		t.Fatal("Call() dispatched an over-long name, want rejection")
	}
	body, marshalErr := json.Marshal(out.Result.StructuredContent)
	if marshalErr != nil {
		t.Fatalf("marshal structured content: %v", marshalErr)
	}
	if strings.Contains(string(body), long) {
		t.Error("Call() result echoes the over-long name, want the fixed error")
	}
}

// TestFindValidatesBeforeRankerGate proves schema, bound, and blank-intent
// failures answer invalid_arguments even though no ranker is configured.
func TestFindValidatesBeforeRankerGate(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	cases := map[string]map[string]any{
		"missing intent": {},
		"blank intent":   {"intent": "   "},
		"oversize":       {"intent": strings.Repeat("x", MaxIntentBytes+1)},
		"bad anchor":     {"intent": "find repos", "anchors": map[string]any{"bogus": "x"}},
	}
	for name, args := range cases {
		result := engine.Find(context.Background(), Request{}, args)
		if !result.IsError {
			t.Errorf("Find(%s) isError = false, want true", name)
			continue
		}
		body, marshalErr := json.Marshal(result.StructuredContent)
		if marshalErr != nil {
			t.Fatalf("marshal structured content: %v", marshalErr)
		}
		if !strings.Contains(string(body), "mcp_router_invalid_arguments") {
			t.Errorf("Find(%s) lacks the invalid-arguments code: %s", name, body)
		}
	}
}

// TestFindWithoutRankerIsUnavailable proves a valid find_tool request on a
// ranker-less server answers the exact ranker-unavailable error.
func TestFindWithoutRankerIsUnavailable(t *testing.T) {
	t.Parallel()

	engine := testEngine()
	result := engine.Find(context.Background(), Request{}, map[string]any{"intent": "find dead code"})
	body, marshalErr := json.Marshal(result.StructuredContent)
	if marshalErr != nil {
		t.Fatalf("marshal structured content: %v", marshalErr)
	}
	if !strings.Contains(string(body), "mcp_router_ranker_unavailable") {
		t.Errorf("Find() result lacks the ranker-unavailable code: %s", body)
	}
}

// TestAdvertiseSurfaces proves every tool surface advertises the pinned set:
// the full registry, the router pair, or the core set plus the pair.
func TestAdvertiseSurfaces(t *testing.T) {
	t.Parallel()

	all := registry.Definitions()
	if got := Advertise(all, SurfaceFull); len(got) != 171 {
		t.Errorf("Advertise(full) = %d tools, want 171", len(got))
	}
	pair := Advertise(all, SurfaceRouter)
	if len(pair) != 2 || pair[0].Name != "find_tool" || pair[1].Name != "call_tool" {
		t.Errorf("Advertise(router) = %v, want [find_tool call_tool]", toolNames(pair))
	}
	core := Advertise(all, SurfaceCoreRouter)
	if len(core) != 12 {
		t.Errorf("Advertise(core_router) = %d tools, want 10 core + pair", len(core))
	}
	if core[10].Name != "find_tool" || core[11].Name != "call_tool" {
		t.Errorf("Advertise(core_router) tail = %v, want the router pair", toolNames(core[10:]))
	}
	for _, tool := range core[:10] {
		if tool.Name == "find_tool" || tool.Name == "call_tool" {
			t.Errorf("Advertise(core_router) head contains router tool %q", tool.Name)
		}
	}
}

// TestInterceptsNamesRouterTools proves only the router pair routes through
// the Host.
func TestInterceptsNamesRouterTools(t *testing.T) {
	t.Parallel()

	if !Intercepts(FindToolName) || !Intercepts(CallToolName) {
		t.Error("Intercepts() misses a router tool, want both intercepted")
	}
	if Intercepts("find_code") || Intercepts("ask") {
		t.Error("Intercepts() claims a registry tool, want registry tools dispatched directly")
	}
}

func toolNames(tools []registry.ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
