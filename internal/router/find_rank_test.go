// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/router"
	"github.com/eshu-hq/eshu-mcp/internal/router/rank"
)

// errRanker is a Ranker that always fails, proving Find maps a rank failure
// to the rank-failed error instead of leaking it.
type errRanker struct{}

func (errRanker) Rank(context.Context, router.Query, []registry.ToolDefinition, func(string) bool) ([]router.Candidate, error) {
	return nil, errors.New("boom")
}

func findRankTools() []registry.ToolDefinition {
	return []registry.ToolDefinition{
		{Name: "alpha_one", Description: "Handles zebra processing.", InputSchema: map[string]any{"type": "object"}},
		{Name: "alpha_two", Description: "Handles other processing.", InputSchema: map[string]any{"type": "object"}},
	}
}

// findBody decodes a Find result's structured content through JSON, so the
// test reads the wire shape whatever Go type the engine built.
func findBody(t *testing.T, result router.ToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	return body
}

func findCandidateNames(t *testing.T, result router.ToolResult) []string {
	t.Helper()
	body := findBody(t, result)
	raw, ok := body["candidates"].([]any)
	if !ok {
		t.Fatalf("find response has no candidates array: %v", body)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		candidate, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("candidate = %T, want an object", item)
		}
		name, ok := candidate["name"].(string)
		if !ok {
			t.Fatalf("candidate name = %T, want a string", candidate["name"])
		}
		out = append(out, name)
	}
	return out
}

// TestFindWithRankerRanksNamedTop proves a valid find_tool request on a
// ranker-wired engine ranks instead of answering ranker-unavailable, with a
// named top candidate carrying the catalog-free cost default.
func TestFindWithRankerRanksNamedTop(t *testing.T) {
	t.Parallel()

	tools := findRankTools()
	engine := router.NewEngine(router.Config{Registry: tools, Ranker: rank.New(tools)})
	result := engine.Find(context.Background(), router.Request{Visible: tools}, map[string]any{"intent": "zebra"})

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if strings.Contains(string(raw), "mcp_router_ranker_unavailable") {
		t.Fatalf("Find() answered ranker-unavailable with a ranker wired: %s", raw)
	}
	if result.IsError {
		t.Fatalf("Find() isError = true, structured = %s", raw)
	}
	names := findCandidateNames(t, result)
	if len(names) == 0 || names[0] != "alpha_one" {
		t.Fatalf("top = %v, want alpha_one first", names)
	}
	body := findBody(t, result)
	rawCandidates, ok := body["candidates"].([]any)
	if !ok || len(rawCandidates) == 0 {
		t.Fatalf("find response has no candidates: %v", body)
	}
	first, ok := rawCandidates[0].(map[string]any)
	if !ok {
		t.Fatalf("candidate = %T, want an object", rawCandidates[0])
	}
	for _, key := range []string{"name", "why", "cost", "inputSchema", "example", "score"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("candidate %v missing %q", first, key)
		}
	}
	if first["cost"] != rank.CostHigh {
		t.Fatalf("candidate cost = %v, want %q (catalog-free default)", first["cost"], rank.CostHigh)
	}
}

// TestFindRankFailureIsRankFailed proves a ranker failure answers
// mcp_router_rank_failed with no candidates.
func TestFindRankFailureIsRankFailed(t *testing.T) {
	t.Parallel()

	tools := findRankTools()
	engine := router.NewEngine(router.Config{Registry: tools, Ranker: errRanker{}})
	result := engine.Find(context.Background(), router.Request{Visible: tools}, map[string]any{"intent": "zebra"})
	if !result.IsError {
		t.Fatal("Find() isError = false, want true for a rank failure")
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if !strings.Contains(string(raw), "mcp_router_rank_failed") {
		t.Fatalf("Find() result lacks the rank-failed code: %s", raw)
	}
}

// TestFindEmptyResultIsNotError proves an intent with no match answers an
// empty candidate list, not an error.
func TestFindEmptyResultIsNotError(t *testing.T) {
	t.Parallel()

	tools := findRankTools()
	engine := router.NewEngine(router.Config{Registry: tools, Ranker: rank.New(tools)})
	result := engine.Find(context.Background(), router.Request{Visible: tools}, map[string]any{"intent": "xylophone quasar"})
	if result.IsError {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("marshal structured content: %v", err)
		}
		t.Fatalf("Find() isError = true, want an empty list: %s", raw)
	}
	if names := findCandidateNames(t, result); len(names) != 0 {
		t.Fatalf("candidates = %v, want none", names)
	}
}

// TestFindWithRankerHidesUnallowed proves Find never offers a tool the caller
// may not see or call: a profile-hidden tool and the refused router targets
// stay out even when they match lexically.
func TestFindWithRankerHidesUnallowed(t *testing.T) {
	t.Parallel()

	registryTools := []registry.ToolDefinition{
		{Name: "alpha_one", Description: "Handles zebra processing.", InputSchema: map[string]any{"type": "object"}},
		{Name: "secret_zebra", Description: "Zebra zebra zebra internals.", InputSchema: map[string]any{"type": "object"}},
		{Name: "ask", Description: "Answers zebra questions.", InputSchema: map[string]any{"type": "object"}},
	}
	visible := registryTools[:1]
	engine := router.NewEngine(router.Config{Registry: registryTools, Ranker: rank.New(registryTools)})
	result := engine.Find(context.Background(), router.Request{Visible: visible}, map[string]any{"intent": "zebra", "limit": 10})

	if result.IsError {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("Find() isError = true and structured content does not marshal: %v", err)
		}
		t.Fatalf("Find() isError = true, structured = %s", raw)
	}
	for _, name := range findCandidateNames(t, result) {
		if name == "secret_zebra" || name == "ask" {
			t.Fatalf("candidates offer %q, want only visible non-refused tools", name)
		}
	}
}

// leakyRanker ignores allowed and returns every tool it was built over,
// proving Find's own re-check drops what the ranker leaks.
type leakyRanker struct {
	names []string
}

func (l leakyRanker) Rank(context.Context, router.Query, []registry.ToolDefinition, func(string) bool) ([]router.Candidate, error) {
	out := make([]router.Candidate, 0, len(l.names))
	for _, name := range l.names {
		out = append(out, router.Candidate{Name: name, Cost: rank.CostHigh, Score: 1})
	}
	return out, nil
}

// TestFindRecheckDropsWhatTheRankerLeaks proves Find upholds the visibility
// filter itself: a ranker that ignores allowed and returns a hidden tool and
// a refused target still gets neither offered.
func TestFindRecheckDropsWhatTheRankerLeaks(t *testing.T) {
	t.Parallel()

	registryTools := []registry.ToolDefinition{
		{Name: "alpha_one", Description: "Handles zebra processing.", InputSchema: map[string]any{"type": "object"}},
		{Name: "secret_zebra", Description: "Zebra internals.", InputSchema: map[string]any{"type": "object"}},
		{Name: "ask", Description: "Answers questions.", InputSchema: map[string]any{"type": "object"}},
	}
	visible := registryTools[:1]
	leaky := leakyRanker{names: []string{"secret_zebra", "ask", "alpha_one"}}
	engine := router.NewEngine(router.Config{Registry: registryTools, Ranker: leaky})
	result := engine.Find(context.Background(), router.Request{Visible: visible}, map[string]any{"intent": "zebra", "limit": 10})

	if result.IsError {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("Find() isError = true and structured content does not marshal: %v", err)
		}
		t.Fatalf("Find() isError = true, structured = %s", raw)
	}
	names := findCandidateNames(t, result)
	if len(names) != 1 || names[0] != "alpha_one" {
		t.Fatalf("candidates = %v, want only [alpha_one] after Find's re-check", names)
	}
}
