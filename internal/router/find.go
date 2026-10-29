package router

// Find_tool request bounds and ranker contract, adapted from
// go/internal/mcp/router/find.go and contract.go at 9922b49. This increment
// wires no ranker — the lexical ranker and its catalog entries ride the next
// increment — so every find_tool request that passes validation answers the
// exact mcp_router_ranker_unavailable error core answers on a ranker-less
// server, and core refuses to start the router surfaces that way. The
// validation order (schema, request bounds, blank intent) and every bound
// are unchanged, so a malformed find_tool fails identically with or without
// a ranker. See docs/parity.md.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	registry "github.com/eshu-hq/eshu-mcp"
)

// Request bounds for find_tool. The MCP transport has no body cap and a
// ranker may echo anchors into every candidate's example, so the handler is
// the boundary that keeps one request from inflating its response.
const (
	// MaxIntentBytes is the longest intent find_tool accepts, in bytes.
	MaxIntentBytes = 4096
	// MaxAnchorValueBytes is the longest anchor value find_tool accepts, in
	// bytes. Real repository, service, and workload ids are far shorter.
	MaxAnchorValueBytes = 256
)

// anchorKeys are the only anchor names find_tool accepts.
var anchorKeys = map[string]bool{"repo_id": true, "service_id": true, "workload_id": true, "environment": true}

// Anchors narrows a find_tool request to the entities the caller already
// knows. Rankers use a non-empty anchor to prefer tools whose input schema
// accepts the matching property.
type Anchors struct {
	// RepoID is a repository identifier.
	RepoID string
	// ServiceID is a service identifier.
	ServiceID string
	// WorkloadID is a workload identifier.
	WorkloadID string
	// Environment is a deployment environment name.
	Environment string
}

// Query is one parsed find_tool request.
type Query struct {
	// Intent is the caller's free-text description of what it wants to do.
	Intent string
	// Anchors holds optional entity identifiers the caller already has.
	Anchors Anchors
	// Limit is the maximum number of candidates to return, 1 to MaxLimit.
	Limit int
}

// Candidate is one ranked tool offered to the caller by find_tool.
type Candidate struct {
	// Name is the registered MCP tool name to pass to call_tool.
	Name string
	// Why is a short, deterministic explanation of the match.
	Why string
	// Cost is the catalog cost class of the tool.
	Cost string
	// InputSchema is the target tool's input schema, so the caller can build
	// arguments without a second lookup.
	InputSchema any
	// Example is a sample arguments object for the tool, or nil when none is
	// curated.
	Example map[string]any
	// Score is the relative score of the candidate; higher is a better match.
	// It is comparable only within one result.
	Score float64
}

// Ranker orders registered tools against a Query. Implementations must be
// deterministic: the same inputs produce the same ordered result. The
// lexical ranker port lands with the next increment; until then the field
// stays nil and find_tool answers ranker-unavailable.
type Ranker interface {
	// Rank returns at most q.Limit candidates, best first, drawn from tools.
	// allowed reports whether the caller may see and call a tool by name.
	Rank(ctx context.Context, q Query, tools []registry.ToolDefinition, allowed func(name string) bool) ([]Candidate, error)
}

// RankerBuilder builds a Ranker over the tools find_tool ranks. It runs once
// per server, at startup.
type RankerBuilder func(tools []registry.ToolDefinition) Ranker

// Find serves find_tool: it validates the request, then answers
// mcp_router_ranker_unavailable, exactly as core answers on a ranker-less
// server. The ranker increment extends this past the gate.
func (e *Engine) Find(_ context.Context, _ Request, args map[string]any) ToolResult {
	if errs, truncated := ValidateArgumentsDetail(toolSchema(FindToolName), args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs, truncated)
	}
	if errs, truncated := boundFindRequest(args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs, truncated)
	}
	if parseQuery(args).Intent == "" {
		return invalidArgumentsResult(FindToolName, []FieldError{{Path: "intent", Keyword: "minLength", Message: "must not be blank"}}, false)
	}
	return errorResult("mcp_router_ranker_unavailable", "find_tool has no ranker configured on this server", nil)
}

// boundFindRequest enforces MaxIntentBytes, MaxAnchorValueBytes, and the
// closed anchor key set on a request that already passed schema validation.
func boundFindRequest(args map[string]any) ([]FieldError, bool) {
	errs := &errorSink{}
	if intent, ok := args["intent"].(string); ok && len(intent) > MaxIntentBytes {
		errs.add(FieldError{Path: "intent", Keyword: "maxLength", Message: fmt.Sprintf("must be at most %d bytes", MaxIntentBytes)})
	}
	anchors, ok := asObject(args["anchors"])
	if !ok {
		anchors = nil
	}
	keys := make([]string, 0, len(anchors))
	for key := range anchors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if errs.full() {
			break
		}
		if !anchorKeys[key] {
			errs.add(FieldError{Path: "anchors." + key, Keyword: "additionalProperties", Message: "is not an anchor; use repo_id, service_id, workload_id, or environment"})
			continue
		}
		if value, ok := anchors[key].(string); ok && len(value) > MaxAnchorValueBytes {
			errs.add(FieldError{Path: "anchors." + key, Keyword: "maxLength", Message: fmt.Sprintf("must be at most %d bytes", MaxAnchorValueBytes)})
		}
	}
	return errs.result()
}

// anchorString returns the named string anchor or "" when it is absent or
// has another type.
func anchorString(anchors map[string]any, key string) string {
	value, ok := anchors[key].(string)
	if !ok {
		return ""
	}
	return value
}

// parseQuery reads a find_tool request that already passed validation. It
// stays for the ranker increment; the ranker-less gate above answers first.
func parseQuery(args map[string]any) Query {
	q := Query{Limit: DefaultLimit}
	if intent, ok := args["intent"].(string); ok {
		q.Intent = intent
	}
	q.Intent = strings.TrimSpace(q.Intent)
	if limit, ok := asNumber(args["limit"]); ok {
		q.Limit = int(limit)
	}
	if anchors, ok := asObject(args["anchors"]); ok {
		q.Anchors.RepoID = anchorString(anchors, "repo_id")
		q.Anchors.ServiceID = anchorString(anchors, "service_id")
		q.Anchors.WorkloadID = anchorString(anchors, "workload_id")
		q.Anchors.Environment = anchorString(anchors, "environment")
	}
	return q
}
