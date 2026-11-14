package router

// Find_tool request bounds and ranker contract, adapted from
// go/internal/mcp/router/find.go and contract.go at 9922b49. The catalog-free
// lexical ranker (internal/router/rank) builds from the versioned registry
// tools alone: a nil Ranker still answers the exact
// mcp_router_ranker_unavailable error core answers on a ranker-less server.
// The validation order (schema, request bounds, blank intent) and every bound
// are unchanged, so a malformed find_tool fails identically with or without
// a ranker. Admission checks stay in core: the destination forwards the
// caller credential so core enforces admission per call, and allowed carries
// only the profile-visibility filter. See docs/parity.md.

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
// catalog-free lexical ranker (internal/router/rank) implements this; a nil
// Ranker makes find_tool answer ranker-unavailable.
type Ranker interface {
	// Rank returns at most q.Limit candidates, best first, drawn from tools.
	// allowed reports whether the caller may see and call a tool by name.
	Rank(ctx context.Context, q Query, tools []registry.ToolDefinition, allowed func(name string) bool) ([]Candidate, error)
}

// RankerBuilder builds a Ranker over the tools find_tool ranks. It runs once
// per server, at startup.
type RankerBuilder func(tools []registry.ToolDefinition) Ranker

// candidateView is the wire form of one find_tool candidate.
type candidateView struct {
	Name        string         `json:"name"`
	Why         string         `json:"why"`
	Cost        string         `json:"cost"`
	InputSchema any            `json:"inputSchema"`
	Example     map[string]any `json:"example"`
	Score       float64        `json:"score"`
}

// findResponse is the find_tool structured result: the ranked candidates,
// best first. Error outcomes use the {data, truth, error} envelope instead.
type findResponse struct {
	Candidates []candidateView `json:"candidates"`
}

// Find serves find_tool: it validates the request, ranks the tools the query
// profile allows, and returns the ranked candidates. It never offers a
// profile-hidden tool, find_tool, call_tool, or ask. Without a ranker it
// answers mcp_router_ranker_unavailable, exactly as core answers on a
// ranker-less server.
func (e *Engine) Find(ctx context.Context, req Request, args map[string]any) ToolResult {
	if errs, truncated := ValidateArgumentsDetail(toolSchema(FindToolName), args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs, truncated)
	}
	if errs, truncated := boundFindRequest(args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs, truncated)
	}
	q := parseQuery(args)
	if q.Intent == "" {
		return invalidArgumentsResult(FindToolName, []FieldError{{Path: "intent", Keyword: "minLength", Message: "must not be blank"}}, false)
	}
	if e.ranker == nil {
		return errorResult("mcp_router_ranker_unavailable", "find_tool has no ranker configured on this server", nil)
	}

	allowed := e.allowedFunc(e.rankable(req.Visible))
	// Hand the ranker the caller's visible slice unchanged on every call, so
	// the ranker can key its index on it; allowed carries the per-call filter.
	ranked, err := e.ranker.Rank(ctx, q, req.Visible, allowed)
	if err != nil {
		e.logger.Warn("mcp router: ranker failed", "error", err)
		return errorResult("mcp_router_rank_failed", "find_tool could not rank tools: "+err.Error(), nil)
	}

	resp := findResponse{Candidates: make([]candidateView, 0, len(ranked))}
	for _, c := range ranked {
		// Re-check the ranker's output: a ranker that ignores allowed must
		// still not leak a tool, and the limit holds whatever it returns.
		if !allowed(c.Name) || len(resp.Candidates) >= q.Limit {
			continue
		}
		resp.Candidates = append(resp.Candidates, e.view(c))
	}
	return jsonResult(resp, false)
}

// rankable is the visible set as a name lookup.
func (e *Engine) rankable(visible []registry.ToolDefinition) map[string]bool {
	names := make(map[string]bool, len(visible))
	for _, tool := range visible {
		names[tool.Name] = true
	}
	return names
}

// allowedFunc builds the per-call filter: visible under the query profile,
// registered, and not a refused target. Admission stays in core, which
// enforces it per call at dispatch; this filter only decides what find_tool
// may offer.
func (e *Engine) allowedFunc(visible map[string]bool) func(string) bool {
	return func(name string) bool {
		if refusedTargets[name] || !visible[name] {
			return false
		}
		_, registered := e.registry[name]
		return registered
	}
}

func (e *Engine) view(c Candidate) candidateView {
	schema := c.InputSchema
	if schema == nil {
		schema = e.registry[c.Name].InputSchema
	}
	example := c.Example
	if example == nil {
		example = e.examples[c.Name]
	}
	return candidateView{Name: c.Name, Why: c.Why, Cost: c.Cost, InputSchema: schema, Example: example, Score: c.Score}
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

// parseQuery reads a find_tool request that already passed validation.
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
