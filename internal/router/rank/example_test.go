// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// TestExampleBuildsFromSchemaAlone proves the candidate example carries every
// required property, schema defaults, first-enum values, and supplied
// anchors, with placeholders by type for the rest.
func TestExampleBuildsFromSchemaAlone(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":   map[string]any{"type": "string"},
			"limit":   map[string]any{"type": "integer", "default": 5},
			"mode":    map[string]any{"type": "string", "enum": []any{"fast", "full"}},
			"repo_id": map[string]any{"type": "string"},
			"verbose": map[string]any{"type": "boolean"},
			"tags":    map[string]any{"type": "array"},
			"filter":  map[string]any{"type": "object"},
			"ratio":   map[string]any{"type": "number"},
			"extra":   map[string]any{"type": "string", "default": "x"},
		},
		"required": []any{"query", "limit", "mode", "repo_id", "verbose", "tags", "filter", "ratio"},
	}
	tools := []registry.ToolDefinition{{Name: "alpha_tool", Description: "Handles zebra processing.", InputSchema: schema}}
	got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: 1})
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	example := got[0].Example
	for wantKey, want := range map[string]any{
		"query":   "<query>",
		"limit":   5,
		"mode":    "fast",
		"repo_id": "<repo_id>",
		"verbose": false,
		"ratio":   1,
		"extra":   "x",
	} {
		if example[wantKey] != want {
			t.Errorf("example[%q] = %#v, want %#v", wantKey, example[wantKey], want)
		}
	}
	if tags, ok := example["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("example[tags] = %#v, want an empty array", example["tags"])
	}
	if filter, ok := example["filter"].(map[string]any); !ok || len(filter) != 0 {
		t.Errorf("example[filter] = %#v, want an empty object", example["filter"])
	}

	anchored := router.Query{Intent: "zebra", Limit: 1}
	anchored.Anchors.RepoID = "repo-9"
	gotAnchored := rankWith(t, tools, anchored)
	if gotAnchored[0].Example["repo_id"] != "repo-9" {
		t.Fatalf("example[repo_id] = %#v, want the supplied anchor", gotAnchored[0].Example["repo_id"])
	}
}

// TestExampleDeepClonesSchemaDefaults proves mutating a candidate example
// cannot reach the registry definition.
func TestExampleDeepClonesSchemaDefaults(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"labels": map[string]any{"type": "object", "default": map[string]any{"team": "graph"}},
			"stages": map[string]any{"type": "array", "default": []any{"plan"}},
		},
		"required": []any{"labels", "stages"},
	}
	tools := []registry.ToolDefinition{{Name: "alpha_tool", Description: "Handles zebra processing.", InputSchema: schema}}
	q := router.Query{Intent: "zebra", Limit: 1}
	first := rankWith(t, tools, q)[0].Example
	labels, ok := first["labels"].(map[string]any)
	if !ok {
		t.Fatalf("example[labels] = %T, want an object", first["labels"])
	}
	labels["team"] = "mutated"
	stages, ok := first["stages"].([]any)
	if !ok || len(stages) == 0 {
		t.Fatalf("example[stages] = %T, want a non-empty array", first["stages"])
	}
	stages[0] = "mutated"
	second := rankWith(t, tools, q)[0].Example
	if !reflect.DeepEqual(second["labels"], map[string]any{"team": "graph"}) {
		t.Fatalf("labels after mutation = %#v, want the schema default", second["labels"])
	}
	if !reflect.DeepEqual(second["stages"], []any{"plan"}) {
		t.Fatalf("stages after mutation = %#v, want the schema default", second["stages"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %T, want an object", schema["properties"])
	}
	labelProp, ok := props["labels"].(map[string]any)
	if !ok {
		t.Fatalf("labels property = %T, want an object", props["labels"])
	}
	if schemaDefault := labelProp["default"]; !reflect.DeepEqual(schemaDefault, map[string]any{"team": "graph"}) {
		t.Fatalf("registry default after mutation = %#v, want it untouched", schemaDefault)
	}
}

// TestExampleSchemaSpellings proves the schema readers accept each spelling
// the hand-built registry has used: map properties beside typed-map
// properties, []string beside []any required lists and enums, and type lists
// beside plain type names.
func TestExampleSchemaSpellings(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]map[string]any{
			"query": {"type": "string"},
			"scope": {"type": []string{"null", "string"}},
			"kind":  {"type": []any{"null", "string"}, "enum": []string{"repo", "service"}},
		},
		"required": []string{"query", "scope", "kind"},
	}
	tools := []registry.ToolDefinition{{Name: "alpha_tool", Description: "Handles zebra processing.", InputSchema: schema}}
	got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: 1})
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	example := got[0].Example
	if example["query"] != "<query>" {
		t.Errorf("example[query] = %#v, want the string placeholder", example["query"])
	}
	if example["scope"] != "<scope>" {
		t.Errorf("example[scope] = %#v, want the type-list placeholder", example["scope"])
	}
	if example["kind"] != "repo" {
		t.Errorf("example[kind] = %#v, want the first []string enum value", example["kind"])
	}
}

// TestCostRankOrdersCheapToExpensive pins the conservative default: anything
// unrecognized, including the empty class, ranks as high.
func TestCostRankOrdersCheapToExpensive(t *testing.T) {
	for tool, want := range map[string]int{CostLow: 0, CostModerate: 1, CostHigh: 2, "bogus": 2, "": 2} {
		if got := costRank(tool); got != want {
			t.Errorf("costRank(%q) = %d, want %d", tool, got, want)
		}
	}
}

// TestRankEmptyRegistryIsEmptyNotError proves an empty registry ranks to no
// candidates and no error.
func TestRankEmptyRegistryIsEmptyNotError(t *testing.T) {
	got, err := New(nil).Rank(context.Background(), router.Query{Intent: "zebra", Limit: 5}, nil, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want none", names(got))
	}
}

// TestRankContextCanceled proves a canceled context fails the Rank instead of
// ranking.
func TestRankContextCanceled(t *testing.T) {
	tools := []registry.ToolDefinition{def("alpha_one", "Handles zebra processing.")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(tools).Rank(ctx, router.Query{Intent: "zebra", Limit: 3}, tools, allowAll); err == nil {
		t.Fatalf("Rank with a canceled context returned no error")
	}
}

// TestRankConcurrentCallsAgree proves concurrent Ranks over one ranker agree
// on names and order.
func TestRankConcurrentCallsAgree(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("find_symbol", "Locate a function or type definition by its name in indexed source code.", "repo_id", "name"),
		def("list_repositories", "Enumerate every repository known to the graph.", "limit"),
		def("get_dead_code", "Report symbols with no incoming references.", "repo_id"),
	}
	r := New(tools)
	q := router.Query{Intent: "find a function definition", Limit: 5}
	want, err := r.Rank(context.Background(), q, tools, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				got, err := r.Rank(context.Background(), q, tools, allowAll)
				if err != nil || !equalStrings(names(got), names(want)) {
					t.Errorf("concurrent Rank = %v, %v; want %v", names(got), err, names(want))
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestRankWhyIsTruncated proves a long match is cut to one bounded line.
func TestRankWhyIsTruncated(t *testing.T) {
	long := "zebra " + strings.Repeat("word ", 200)
	tools := []registry.ToolDefinition{def("alpha_tool", long)}
	got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: 1})
	if len(got) != 1 || len(got[0].Why) > 260 {
		t.Fatalf("Why = %d bytes, want one candidate and <= 260", len(got[0].Why))
	}
}
