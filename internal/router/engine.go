package router

// Engine state and shared result shapes, adapted from
// go/internal/mcp/router/engine.go at 9922b49. The registry, names,
// examples, and refused-target set are unchanged. Route classification
// (routeClass), the catalog entries, the recent-finds rank histogram, and the
// OTEL instruments stay in core: the destination forwards the caller
// credential so core enforces admission per call, and the seeded contract
// carries the examples the source computed with its resolver. See
// docs/parity.md.

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// refusedTargets are the names call_tool never dispatches and find_tool never
// offers: the router tools themselves (recursion) and ask, which re-enters
// the Ask engine and could call back into the router.
var refusedTargets = map[string]bool{FindToolName: true, CallToolName: true, "ask": true}

// ResolveFunc maps a tool call to the HTTP route dispatch would send.
type ResolveFunc func(name string, arguments map[string]any) (*routecontract.Request, error)

// Config wires an Engine to one MCP server.
type Config struct {
	// Registry is every registered tool. call_tool looks targets up here and
	// validates against their schemas, so a tool the query profile hides
	// from tools/list stays callable, exactly like a direct tools/call.
	Registry []registry.ToolDefinition
	// Resolve maps a tool call to the HTTP route dispatch would send.
	Resolve ResolveFunc
	// Ranker orders candidates for find_tool. Nil makes find_tool answer
	// mcp_router_ranker_unavailable instead of inventing an order.
	Ranker Ranker
	// Logger receives router decisions. Nil uses slog.Default.
	Logger *slog.Logger
}

// Engine serves find_tool and call_tool for one MCP server. It is safe for
// concurrent use; per-request state travels in Request.
type Engine struct {
	registry map[string]registry.ToolDefinition
	names    []string
	examples map[string]map[string]any
	resolve  ResolveFunc
	ranker   Ranker
	logger   *slog.Logger
}

// Request carries the per-call inputs the MCP root owns.
type Request struct {
	// Visible is the tool list the server's query profile exposes. find_tool
	// ranks only these tools.
	Visible []registry.ToolDefinition
	// Session keys the caller's last find_tool result. The root passes the
	// credential's principal key.
	Session string
	// Dispatch runs a registered tool through the same path as tools/call.
	Dispatch Dispatcher
}

// Dispatcher runs one registered tool and returns its result. The MCP root
// injects it, closed over the per-request auth and logger. It must route
// through the same dispatch as a direct tools/call, so auth, timeouts,
// response budgets, and envelopes are unchanged. The value is opaque to this
// package: the root returns the tool result it renders for a direct
// tools/call, so the envelope and truth labels pass through untouched.
type Dispatcher func(ctx context.Context, name string, arguments map[string]any) (any, error)

// NewEngine builds an Engine and indexes every registered tool's example
// once, so per-call validation and error details are map lookups.
func NewEngine(cfg Config) *Engine {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	e := &Engine{
		registry: make(map[string]registry.ToolDefinition, len(cfg.Registry)),
		examples: make(map[string]map[string]any, len(cfg.Registry)),
		resolve:  cfg.Resolve,
		ranker:   cfg.Ranker,
		logger:   logger,
	}
	for _, tool := range cfg.Registry {
		e.registry[tool.Name] = tool
		e.names = append(e.names, tool.Name)
		if example, ok := registry.Example(tool.Name); ok {
			e.examples[tool.Name] = example
		}
	}
	return e
}

// Example returns a copy of the example arguments handed back for a
// registered tool.
func (e *Engine) Example(name string) (map[string]any, bool) {
	example, ok := e.examples[name]
	if !ok {
		return nil, false
	}
	return maps.Clone(example), true
}

// ToolResult is the MCP tools/call result the router builds itself. Its JSON
// matches the root's tool-result shape field for field.
type ToolResult struct {
	// Content holds the text rendering of the result.
	Content []Content `json:"content"`
	// StructuredContent holds the machine-readable result.
	StructuredContent any `json:"structuredContent,omitempty"`
	// IsError marks a tool-level failure.
	IsError bool `json:"isError,omitempty"`
}

// Content is one MCP content block.
type Content struct {
	// Type is the MCP content type; the router only emits "text".
	Type string `json:"type"`
	// Text is the rendered content.
	Text string `json:"text,omitempty"`
}

// CallOutcome is the result of one call_tool request. When Dispatched is
// true the root renders Value or Err exactly as it renders a direct
// tools/call to Target, so the two are byte-identical. Otherwise Result holds
// the router-built error result.
type CallOutcome struct {
	// Target is the requested tool name, empty when the request had none.
	Target string
	// Dispatched reports whether the Dispatcher ran.
	Dispatched bool
	// Value is the Dispatcher's result, untouched.
	Value any
	// Err is the Dispatcher's error, untouched.
	Err error
	// Result is the router-built error result when Dispatched is false.
	Result ToolResult
}

// jsonResult renders body as a tool result whose text is the compact JSON of
// the structured content, for clients that read only text.
func jsonResult(body any, isError bool) ToolResult {
	text, err := json.Marshal(body)
	if err != nil {
		text = []byte(`{"error":"unrenderable router result"}`)
	}
	return ToolResult{
		Content:           []Content{{Type: "text", Text: string(text)}},
		StructuredContent: body,
		IsError:           isError,
	}
}

// errorResult builds the router's {data, truth, error} error envelope, the
// same shape the dispatch timeout error uses.
func errorResult(code, message string, details map[string]any) ToolResult {
	return jsonResult(map[string]any{
		"data":  nil,
		"truth": nil,
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"capability": "mcp.router",
			"details":    details,
		},
	}, true)
}

// invalidArgumentsResult reports field errors for a router tool's own
// arguments or a call_tool target's arguments.
func invalidArgumentsResult(tool string, errs []FieldError, truncated bool) ToolResult {
	return errorResult("mcp_router_invalid_arguments", "arguments for "+tool+" do not match its input schema",
		map[string]any{"tool": tool, "field_errors": errs, "field_errors_truncated": truncated})
}
