package router

// Host state and request serving, adapted from
// go/internal/mcp/router/host.go at 9922b49. The engine builds lazily on the
// first router request; a nil ranker stays nil, so find_tool answers
// ranker-unavailable while call_tool dispatches. A dispatched call_tool
// returns the Dispatcher's value untouched, so a routed result is
// byte-identical to the direct one. Every other outcome is a ToolResult. The
// mcp_router_dispatch_failed branch is defensive: the root's Dispatcher
// renders dispatch errors itself and returns a nil error, exactly like core.

import (
	"context"
	"log/slog"
	"sync"

	registry "github.com/eshu-hq/eshu-mcp"
)

// Host is the router state one MCP server embeds. It builds the Engine on
// the first router request and serves find_tool and call_tool through it.
// The zero value is ready to use.
type Host struct {
	// Ranker orders find_tool candidates. Set it before the first request,
	// directly or through NewRanker and Prepare; while it is nil find_tool
	// answers mcp_router_ranker_unavailable.
	Ranker Ranker
	// NewRanker, when set, builds Ranker once in Prepare. The command wires
	// the catalog-free lexical ranker (internal/router/rank) here.
	NewRanker RankerBuilder

	// logger is the Prepare-supplied logger the ranker increment will use.
	// It stays nil until the first Prepare call.
	logger *slog.Logger

	once   sync.Once
	engine *Engine
}

// Prepare builds Ranker with NewRanker at server start, before any request.
// tools must be the slice the root passes as Request.Visible on every call,
// so a future ranker's index matches what Rank is handed. Prepare does
// nothing when NewRanker is nil or Ranker is already set, and is not safe to
// call concurrently with Serve. A nil logger uses slog.Default.
func (h *Host) Prepare(tools []registry.ToolDefinition, logger *slog.Logger) {
	if h.NewRanker == nil || h.Ranker != nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	h.logger = logger
	h.Ranker = h.NewRanker(tools)
}

// Serve answers one find_tool or call_tool request. newConfig runs once, on
// the first call, to build the Engine; a nil Config.Ranker takes h.Ranker. A
// dispatched call_tool returns the Dispatcher's value untouched, so when the
// root's Dispatcher returns the result it renders for a direct tools/call, a
// routed result is byte-identical to the direct one. Every other outcome is
// a ToolResult.
func (h *Host) Serve(ctx context.Context, newConfig func() Config, req Request, name string, args map[string]any) any {
	h.once.Do(func() {
		cfg := newConfig()
		if cfg.Ranker == nil {
			cfg.Ranker = h.Ranker
		}
		h.engine = NewEngine(cfg)
	})
	if name == FindToolName {
		return h.engine.Find(ctx, req, args)
	}
	out := h.engine.Call(ctx, req, args)
	switch {
	case !out.Dispatched:
		return out.Result
	case out.Err != nil:
		return errorResult("mcp_router_dispatch_failed", out.Err.Error(), map[string]any{"tool": out.Target})
	default:
		return out.Value
	}
}
