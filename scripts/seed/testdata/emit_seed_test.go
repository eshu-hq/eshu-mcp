package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestZZEmitSeedFacts dumps the extraction seed facts for eshu-mcp.
// TEMPORARY extraction tooling: runs once in a scratch worktree at the
// pinned source commit, then the file is deleted. Output goes to
// $ESHU_SEED_OUT/seed-facts.json.
func TestZZEmitSeedFacts(t *testing.T) {
	outDir := os.Getenv("ESHU_SEED_OUT")
	if outDir == "" {
		t.Skip("ESHU_SEED_OUT not set")
	}

	engine := router.NewEngine(router.Config{
		Registry: ReadOnlyTools(),
		Resolve:  resolveRoute,
	})

	tools := ReadOnlyTools()
	defs := make([]map[string]any, 0, len(tools))
	examples := map[string]any{}
	for _, tool := range tools {
		schemaJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema for %q: %v", tool.Name, err)
		}
		defs = append(defs, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": json.RawMessage(schemaJSON),
		})
		ex, ok := engine.Example(tool.Name)
		if !ok {
			ex = map[string]any{}
		}
		examples[tool.Name] = ex
	}

	pair := router.Tools()
	pairDefs := make([]map[string]any, 0, len(pair))
	for _, tool := range pair {
		schemaJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal router schema for %q: %v", tool.Name, err)
		}
		pairDefs = append(pairDefs, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": json.RawMessage(schemaJSON),
		})
	}

	// Resolution sweep with the checked-in catalog sweep args table.
	sweepRaw, err := os.ReadFile(filepath.Join("testdata", "catalog_sweep_args.json"))
	if err != nil {
		t.Fatalf("read sweep args: %v", err)
	}
	var sweepTable struct {
		Tools map[string][]struct {
			Label     string         `json:"label"`
			Arguments map[string]any `json:"arguments"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(sweepRaw, &sweepTable); err != nil {
		t.Fatalf("parse sweep args: %v", err)
	}
	resolutions := map[string]any{}
	for name, cases := range sweepTable.Tools {
		rows := make([]map[string]any, 0, len(cases))
		for _, c := range cases {
			route, err := resolveRoute(name, c.Arguments)
			row := map[string]any{"label": c.Label, "arguments": c.Arguments}
			if err != nil {
				row["error"] = err.Error()
			} else {
				row["method"] = route.Method
				row["path"] = route.Path
				if route.Query != nil {
					row["query"] = route.Query
				}
				if route.Body != nil {
					row["body"] = route.Body
				} else {
					row["body"] = nil
				}
			}
			rows = append(rows, row)
		}
		resolutions[name] = rows
	}

	coreNames := router.CoreToolNames()
	hidden := []string{}
	for name := range productionHiddenToolNames() {
		hidden = append(hidden, name)
	}

	facts := map[string]any{
		"registry":          defs,
		"router_pair":       pairDefs,
		"examples":          examples,
		"resolutions":       resolutions,
		"core_router_names": coreNames,
		"production_hidden": hidden,
		"source_constants": map[string]any{
			"content_search_max_offset": query.ContentSearchMaxOffset,
			"envelope_mime":             query.EnvelopeMIMEType,
			"protocol_versions":         supportedProtocolVersions,
		},
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatalf("marshal facts: %v", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir out: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "seed-facts.json"), encoded, 0o644); err != nil {
		t.Fatalf("write facts: %v", err)
	}
}
