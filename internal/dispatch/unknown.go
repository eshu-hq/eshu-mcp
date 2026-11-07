package dispatch

import (
	"sort"

	registry "github.com/eshu-hq/eshu-mcp"
)

// unknownToolCode is the canonical machine-readable code for a tool name no
// route claims. It matches the router's call_tool refusal so a direct
// tools/call and a routed call_tool report the same code.
const unknownToolCode = "mcp_router_unknown_tool"

// maxUnknownSuggestions caps the closest registered names an unknown target
// gets, matching the router's cap.
const maxUnknownSuggestions = 3

// UnknownToolError is the refusal for a tool name no route claims. The text
// keeps the historic dispatch error a client has always seen; the typed form
// lets the renderer add the canonical code and the closest registered names
// without changing the fail-closed outcome (IsError, no core call).
type UnknownToolError struct {
	Tool string
	// Suggestions holds up to maxUnknownSuggestions registered names nearest
	// the requested tool, nearest first. It is never nil so it encodes as
	// [] rather than null when nothing is near.
	Suggestions []string
}

func (e *UnknownToolError) Error() string { return "unknown tool: " + e.Tool }

// Unwrap keeps errors.Is(err, errUnknownTool) true, so outcome classification
// and every existing refusal check behave exactly as before.
func (e *UnknownToolError) Unwrap() error { return errUnknownTool }

// newUnknownToolError builds the refusal for a name no route claims,
// attaching the closest registered names.
func newUnknownToolError(toolName string) *UnknownToolError {
	return &UnknownToolError{Tool: toolName, Suggestions: closestToolNames(toolName, registry.ToolNames())}
}

// closestToolNames returns up to maxUnknownSuggestions names nearest to name
// by edit distance, ties broken by name — the same order the router's
// call_tool refusal uses.
func closestToolNames(name string, names []string) []string {
	type scored struct {
		name     string
		distance int
	}
	scoredNames := make([]scored, 0, len(names))
	for _, candidate := range names {
		scoredNames = append(scoredNames, scored{candidate, toolNameDistance(name, candidate)})
	}
	sort.Slice(scoredNames, func(i, j int) bool {
		if scoredNames[i].distance != scoredNames[j].distance {
			return scoredNames[i].distance < scoredNames[j].distance
		}
		return scoredNames[i].name < scoredNames[j].name
	})
	out := make([]string, 0, maxUnknownSuggestions)
	for _, s := range scoredNames {
		if len(out) == maxUnknownSuggestions {
			break
		}
		out = append(out, s.name)
	}
	return out
}

// toolNameDistance is the Levenshtein distance between a and b over bytes;
// tool names are ASCII.
func toolNameDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
