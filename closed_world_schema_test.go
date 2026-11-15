package registry

import (
	"testing"
)

// Closed-world schema markers (eshu-hq/eshu-mcp#86): the router pair declares
// additionalProperties: false so a call_tool/find_tool request with an
// undeclared key is refused by the validator naming the key. The target tools
// stay open until the declare-or-remove follow-ups (#76-#80) land, so only
// the router pair is pinned here.
func TestRouterPairDeclaresClosedWorld(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"find_tool", "call_tool"} {
		var found bool
		for _, tool := range RouterTools() {
			if tool.Name != name {
				continue
			}
			found = true
			schema, ok := tool.InputSchema.(map[string]any)
			if !ok {
				t.Fatalf("RouterTools(%s) schema = %T, want object map", name, tool.InputSchema)
			}
			extra, ok := schema["additionalProperties"]
			if !ok {
				t.Errorf("RouterTools(%s) omits additionalProperties, want false", name)
				continue
			}
			if extra != false {
				t.Errorf("RouterTools(%s) additionalProperties = %#v, want false", name, extra)
			}
		}
		if !found {
			t.Errorf("RouterTools(%s) missing", name)
		}
	}
}

// TestRouterPairCoversRouteReadKeys proves the router pair's closed schemas
// declare every key their handlers read, so closed-world rejection cannot
// refuse a key the route needs. call_tool reads name and arguments; find_tool
// reads intent, anchors, and limit.
func TestRouterPairCoversRouteReadKeys(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"find_tool": {"intent", "anchors", "limit"},
		"call_tool": {"name", "arguments"},
	}
	for _, tool := range RouterTools() {
		want, ok := cases[tool.Name]
		if !ok {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("RouterTools(%s) schema = %T, want object map", tool.Name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("RouterTools(%s) properties = %T, want object map", tool.Name, schema["properties"])
		}
		for _, key := range want {
			if _, ok := properties[key]; !ok {
				t.Errorf("RouterTools(%s) does not declare route-read key %q", tool.Name, key)
			}
		}
	}
}
