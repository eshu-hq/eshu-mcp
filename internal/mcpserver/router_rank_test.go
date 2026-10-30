// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpserver

import (
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/router"
	"github.com/eshu-hq/eshu-mcp/internal/router/rank"
)

// TestRouterSurfaceWithRankerRanks proves a router-surface server wired with
// the catalog-free ranker boots and answers find_tool through POST
// /mcp/message with ranked candidates: no refusal, no ranker-unavailable, a
// named top candidate. The assertions read the router-built
// structuredContent candidates array (find_tool renders its own payload, not
// the dispatched {data, truth, error} envelope), never top-level fields.
func TestRouterSurfaceWithRankerRanks(t *testing.T) {
	t.Parallel()

	tools := []registry.ToolDefinition{
		{Name: "alpha_one", Description: "Handles zebra processing.", InputSchema: map[string]any{"type": "object"}},
		{Name: "alpha_two", Description: "Handles other processing.", InputSchema: map[string]any{"type": "object"}},
	}
	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, tools, WithToolSurface(router.SurfaceRouter), WithRouterRanker(
		func(tools []registry.ToolDefinition) router.Ranker { return rank.New(tools) }))
	defer cleanup()
	mux := mcp.Handler(nil)

	_, listed := postMessage(t, mux, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "Bearer test")
	served := mustSlice(t, mustMap(t, listed["result"])["tools"])
	if len(served) != 2 {
		t.Fatalf("router tools/list serves %d tools, want [find_tool call_tool]", len(served))
	}

	_, decoded := postMessage(t, mux,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_tool","arguments":{"intent":"zebra"}}}`,
		"Bearer test")
	result := mustMap(t, decoded["result"])
	body := mustJSON(t, result["structuredContent"])
	if strings.Contains(string(body), "mcp_router_ranker_unavailable") {
		t.Fatalf("find_tool answered ranker-unavailable with a ranker wired: %s", body)
	}
	if result["isError"] == true {
		t.Fatalf("find_tool isError = true, want ranked candidates: %s", body)
	}
	// Router-built success carries candidates under structuredContent, not
	// under an envelope data key and not at the top level of the result.
	structured := mustMap(t, result["structuredContent"])
	candidates := mustSlice(t, structured["candidates"])
	if len(candidates) == 0 {
		t.Fatal("find_tool returned no candidates, want a ranked list")
	}
	first := mustMap(t, candidates[0])
	if first["name"] != "alpha_one" {
		t.Fatalf("top candidate = %v, want alpha_one", first["name"])
	}
	if first["cost"] != rank.CostHigh {
		t.Fatalf("top candidate cost = %v, want %q (catalog-free default)", first["cost"], rank.CostHigh)
	}
}
