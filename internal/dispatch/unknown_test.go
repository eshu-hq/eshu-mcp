package dispatch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// TestUnknownToolErrorCarriesSuggestions proves an unclaimed name refuses
// with the typed unknown-tool error: errors.Is still matches errUnknownTool
// (fail-closed classification unchanged) and errors.As exposes the tool plus
// the closest registered names.
func TestUnknownToolErrorCarriesSuggestions(t *testing.T) {
	t.Parallel()

	_, err := ResolveRoute("find_cod", map[string]any{})
	if err == nil {
		t.Fatal("ResolveRoute(find_cod) succeeded, want the unknown-tool refusal")
	}
	if !errors.Is(err, errUnknownTool) {
		t.Fatalf("ResolveRoute(find_cod) error = %v, want errUnknownTool in the chain", err)
	}
	var unknown *UnknownToolError
	if !errors.As(err, &unknown) {
		t.Fatalf("ResolveRoute(find_cod) error type = %T, want *UnknownToolError", err)
	}
	if unknown.Tool != "find_cod" {
		t.Errorf("UnknownToolError.Tool = %q, want find_cod", unknown.Tool)
	}
	if len(unknown.Suggestions) == 0 || len(unknown.Suggestions) > 3 {
		t.Errorf("UnknownToolError.Suggestions = %v, want 1-3 names", unknown.Suggestions)
	}
	found := false
	for _, suggestion := range unknown.Suggestions {
		if suggestion == "find_code" {
			found = true
		}
	}
	if !found {
		t.Errorf("UnknownToolError.Suggestions = %v, want find_code among them", unknown.Suggestions)
	}
	if got := classifyOutcome("find_cod", nil, err); got != outcomeUnknownTool {
		t.Errorf("classifyOutcome() = %q, want %q", got, outcomeUnknownTool)
	}
}

// TestUnknownToolRendersCanonicalCode proves the unknown-tool refusal
// renders mcp_router_unknown_tool with suggestions in both shapes while
// staying fail-closed: IsError with no core call.
func TestUnknownToolRendersCanonicalCode(t *testing.T) {
	t.Parallel()

	for shapeName, shape := range map[string]resultshape.Mode{
		"dual":       resultshape.Dual,
		"structured": resultshape.Structured,
	} {
		stub := &stubCore{status: 200, body: `{}`}
		_, err := dispatchAgainstStub(t, stub, "find_cod", map[string]any{}, "", DefaultOptions())
		if err == nil {
			t.Fatalf("[%s] Dispatch() succeeded, want the unknown-tool refusal", shapeName)
		}
		if stub.seenPath != "" {
			t.Errorf("[%s] core saw path %q, want no core call on an unknown tool", shapeName, stub.seenPath)
		}
		rendered := ToolErrorResultForCall(shape, "find_cod", err, nil, DefaultOptions().ResponseByteBudget)
		if !rendered.IsError {
			t.Errorf("[%s] IsError = false, want true for the unknown-tool refusal", shapeName)
		}
		var text strings.Builder
		for _, block := range rendered.Content {
			text.WriteString(block.Text)
		}
		if !strings.Contains(text.String(), "unknown tool: find_cod") {
			t.Errorf("[%s] content = %q, want the historic refusal text", shapeName, text.String())
		}
		body, marshalErr := json.Marshal(rendered.StructuredContent)
		if marshalErr != nil {
			t.Fatalf("[%s] marshal structured content: %v", shapeName, marshalErr)
		}
		for _, want := range []string{"mcp_router_unknown_tool", "suggestions", "find_code"} {
			if !strings.Contains(string(body), want) {
				t.Errorf("[%s] structured content = %s, want %q", shapeName, body, want)
			}
		}
	}
}
