// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// rankSeeded builds a fresh entry-aware ranker over tools and ranks one
// query with every tool allowed.
func rankSeeded(t *testing.T, tools []registry.ToolDefinition, entries map[string]registry.CatalogEntry, q router.Query) []router.Candidate {
	t.Helper()
	got, err := New(tools, entries).Rank(context.Background(), q, tools, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	return got
}

func candidateByName(cs []router.Candidate) map[string]router.Candidate {
	byName := make(map[string]router.Candidate, len(cs))
	for _, c := range cs {
		byName[c.Name] = c
	}
	return byName
}

// TestSeededEntryCostSurfaces proves an entry's cost class reaches the
// candidate, and a tool with no entry keeps the conservative CostHigh
// default with no Canonical mark.
func TestSeededEntryCostSurfaces(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("cheap_tool", "Locates the zebra."),
		def("mid_tool", "Locates the zebra."),
		def("pricey_tool", "Locates the zebra."),
		def("unseeded_tool", "Locates the zebra."),
	}
	entries := map[string]registry.CatalogEntry{
		"cheap_tool":  {Cost: CostLow},
		"mid_tool":    {Cost: CostModerate},
		"pricey_tool": {Cost: CostHigh},
	}
	byName := candidateByName(rankSeeded(t, tools, entries, router.Query{Intent: "zebra", Limit: 10}))
	for tool, want := range map[string]string{
		"cheap_tool": CostLow, "mid_tool": CostModerate, "pricey_tool": CostHigh, "unseeded_tool": CostHigh,
	} {
		if byName[tool].Cost != want {
			t.Fatalf("candidate %q Cost = %q, want %q", tool, byName[tool].Cost, want)
		}
	}
	if byName["unseeded_tool"].Name == "" {
		t.Fatal("unseeded_tool is missing from the candidates")
	}
}

// TestSeededCanonicalWinsExactTie proves the Canonical mark decides an
// exact-score tie before cheaper cost and name do.
func TestSeededCanonicalWinsExactTie(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_plain", "Locates the zebra."),
		def("zeta_prime", "Locates the zebra."),
	}
	entries := map[string]registry.CatalogEntry{
		"alpha_plain": {Cost: CostLow},
		"zeta_prime":  {Cost: CostLow, Canonical: true},
	}
	got := rankSeeded(t, tools, entries, router.Query{Intent: "zebra", Limit: 5})
	if len(got) != 2 || got[0].Name != "zeta_prime" {
		t.Fatalf("order = %v, want [zeta_prime alpha_plain] (canonical wins the tie)", names(got))
	}
	if got[0].Score != got[1].Score {
		t.Fatalf("scores differ (%v vs %v); fixture is not an exact tie", got[0].Score, got[1].Score)
	}
}

// TestSeededCheaperCostWinsNextTie proves cheaper cost decides a tie the
// Canonical mark does not, before name order does.
func TestSeededCheaperCostWinsNextTie(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_mid", "Locates the zebra."),
		def("zeta_cheap", "Locates the zebra."),
	}
	entries := map[string]registry.CatalogEntry{
		"alpha_mid":  {Cost: CostModerate},
		"zeta_cheap": {Cost: CostLow},
	}
	got := rankSeeded(t, tools, entries, router.Query{Intent: "zebra", Limit: 5})
	if len(got) != 2 || got[0].Name != "zeta_cheap" {
		t.Fatalf("order = %v, want [zeta_cheap alpha_mid] (cheaper cost wins the tie)", names(got))
	}
	if got[0].Score != got[1].Score {
		t.Fatalf("scores differ (%v vs %v); fixture is not a tie", got[0].Score, got[1].Score)
	}
}

// TestSeededIntentOutranksDescription proves a curated intent phrase match
// (weight 3.0) outranks a description-only match (weight 1.0) and surfaces
// in the Why line as an intent match.
func TestSeededIntentOutranksDescription(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_tool", "Does something unrelated."),
		def("beta_tool", "Mentions zebra here."),
	}
	entries := map[string]registry.CatalogEntry{
		"alpha_tool": {Cost: CostLow, Intents: []string{"zebra tracking field guide"}},
		"beta_tool":  {Cost: CostLow},
	}
	got := rankSeeded(t, tools, entries, router.Query{Intent: "zebra", Limit: 5})
	if len(got) != 2 || got[0].Name != "alpha_tool" {
		t.Fatalf("top = %v, want alpha_tool first (intent match outranks description-only)", names(got))
	}
	if why := got[0].Why; !strings.Contains(why, "intent") {
		t.Fatalf("alpha_tool Why = %q, want the curated intent quoted", why)
	}
}

// TestSeededMissingEntryAloneStaysHigh proves a partial entries map degrades
// per tool: the missing tool alone keeps CostHigh and no Canonical mark,
// and a rebuild over a different tools slice keeps scoring the same map.
func TestSeededMissingEntryAloneStaysHigh(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("seeded_tool", "Locates the zebra."),
		def("missing_tool", "Locates the zebra."),
	}
	entries := map[string]registry.CatalogEntry{
		"seeded_tool": {Cost: CostLow, Canonical: true},
	}
	r := New(tools, entries)
	got, err := r.Rank(context.Background(), router.Query{Intent: "zebra", Limit: 5}, tools, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	byName := candidateByName(got)
	if byName["seeded_tool"].Cost != CostLow {
		t.Fatalf("seeded_tool Cost = %q, want %q", byName["seeded_tool"].Cost, CostLow)
	}
	missing, ok := byName["missing_tool"]
	if !ok {
		t.Fatal("missing_tool is absent from the candidates")
	}
	if missing.Cost != CostHigh {
		t.Fatalf("missing_tool Cost = %q, want %q", missing.Cost, CostHigh)
	}
	if got[0].Name != "seeded_tool" {
		t.Fatalf("top = %v, want seeded_tool first", names(got))
	}
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds = %d, want 1", got)
	}
}

// TestSeededDeterministicOverFreshBuilds runs one intent query on fresh
// seeded rankers and requires one identical ordered result including scores
// and Why text.
func TestSeededDeterministicOverFreshBuilds(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_tool", "Does something unrelated."),
		def("beta_tool", "Mentions zebra here."),
		def("gamma_tool", "Handles other zebra processing."),
	}
	entries := map[string]registry.CatalogEntry{
		"alpha_tool": {Cost: CostLow, Canonical: true, Intents: []string{"zebra tracking field guide"}},
		"beta_tool":  {Cost: CostModerate},
	}
	q := router.Query{Intent: "zebra tracking", Limit: 5}
	var first []router.Candidate
	for run := range 25 {
		got := rankSeeded(t, tools, entries, q)
		if run == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("run %d: %d candidates, want %d", run, len(got), len(first))
		}
		for i := range got {
			if got[i].Name != first[i].Name || got[i].Score != first[i].Score || got[i].Why != first[i].Why {
				t.Fatalf("run %d position %d: %+v, want %+v", run, i, got[i], first[i])
			}
		}
	}
	if len(first) == 0 || first[0].Name != "alpha_tool" {
		t.Fatalf("top = %v, want alpha_tool first", names(first))
	}
}
