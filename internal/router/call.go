package router

// Call path for call_tool, adapted from go/internal/mcp/router/call.go at
// 9922b49. The request validation, refusal, registry lookup, unknown-tool
// suggestions, argument validation, and verbatim-argument dispatch are
// unchanged. Two source checks have no destination equivalent and are
// documented deltas (see docs/parity.md): the credential admission re-checks
// (acc.admits on the registry-time and argument-selected route classes) and
// the OTEL spans/metrics. Core remains the admission authority: the
// destination forwards the caller credential on every dispatch, so a scoped
// credential is refused by core at HTTP time and the refusal envelope passes
// through untouched. For the standard shared-key credential every omitted
// check admits, so behavior is identical.

import (
	"context"
	"fmt"
	"sort"

	registry "github.com/eshu-hq/eshu-mcp"
)

// maxSuggestions caps the closest registered names an unknown target gets.
const maxSuggestions = 3

// MaxToolNameBytes is the longest call_tool name the router reads. A longer
// name is rejected with a fixed error that does not echo it, before any
// lookup or edit-distance work. Registered names are far shorter.
const MaxToolNameBytes = 128

// Call serves call_tool. It refuses find_tool, call_tool, and ask; answers an
// unknown name with the closest registered names; validates arguments against
// the target's input schema; and otherwise hands the caller's arguments,
// unchanged, to the Dispatcher. A dispatched outcome carries the
// Dispatcher's value and error untouched so the root renders it exactly like
// a direct tools/call.
func (e *Engine) Call(ctx context.Context, req Request, args map[string]any) CallOutcome {
	if errs, truncated := ValidateArgumentsDetail(toolSchema(CallToolName), args); len(errs) != 0 {
		return CallOutcome{Result: invalidArgumentsResult(CallToolName, errs, truncated)}
	}
	name, ok := args["name"].(string)
	if !ok {
		name = ""
	}
	if len(name) > MaxToolNameBytes {
		return CallOutcome{Result: invalidArgumentsResult(CallToolName, []FieldError{{
			Path: "name", Keyword: "maxLength", Message: fmt.Sprintf("must be at most %d bytes", MaxToolNameBytes),
		}}, false)}
	}
	arguments, _ := asObject(args["arguments"])
	out := CallOutcome{Target: name}

	if refusedTargets[name] {
		out.Result = errorResult("mcp_router_refused", "call_tool cannot run "+name,
			map[string]any{"tool": name, "reason": "recursive_target"})
		return out
	}
	tool, registered := e.registry[name]
	if !registered {
		out.Result = errorResult("mcp_router_unknown_tool", "no registered tool is named "+name,
			map[string]any{"tool": name, "suggestions": e.closestNames(name)})
		return out
	}
	example, ok := e.examples[name]
	if !ok {
		example = nil
	}
	if errs, truncated := ValidateArgumentsDetail(tool.InputSchema, arguments); len(errs) != 0 {
		out.Result = errorResult("mcp_router_invalid_arguments", "arguments for "+name+" do not match its input schema",
			map[string]any{
				"tool":                   name,
				"field_errors":           errs,
				"field_errors_truncated": truncated,
				"input_schema":           tool.InputSchema,
				"example":                map[string]any{"name": name, "arguments": example},
			})
		return out
	}

	// Pass the caller's own map: a direct tools/call hands dispatch the same
	// decoded object, so the routed call is byte-identical.
	if decoded, ok := args["arguments"].(map[string]any); ok {
		arguments = decoded
	}
	out.Dispatched = true
	out.Value, out.Err = req.Dispatch(ctx, name, arguments)
	return out
}

// toolSchema returns the input schema of the router tool named name, found
// by name so request validation never depends on the order of the pair.
func toolSchema(name string) any {
	for _, tool := range registry.RouterTools() {
		if tool.Name == name {
			return tool.InputSchema
		}
	}
	return nil
}

// closestNames returns up to maxSuggestions registered names nearest to name
// by edit distance, ties broken by name. The caller bounds name by
// MaxToolNameBytes, so the work is at most that times the registry size.
func (e *Engine) closestNames(name string) []string {
	type scored struct {
		name     string
		distance int
	}
	scoredNames := make([]scored, 0, len(e.names))
	for _, candidate := range e.names {
		scoredNames = append(scoredNames, scored{candidate, editDistance(name, candidate)})
	}
	sort.Slice(scoredNames, func(i, j int) bool {
		if scoredNames[i].distance != scoredNames[j].distance {
			return scoredNames[i].distance < scoredNames[j].distance
		}
		return scoredNames[i].name < scoredNames[j].name
	})
	out := make([]string, 0, maxSuggestions)
	for _, s := range scoredNames {
		if len(out) == maxSuggestions {
			break
		}
		out = append(out, s.name)
	}
	return out
}

// editDistance is the Levenshtein distance between a and b over bytes; tool
// names are ASCII.
func editDistance(a, b string) int {
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
