// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/router"
	"github.com/eshu-hq/eshu-mcp/internal/router/rank"
)

// goldenQuery is one pinned ranking: the catalog-free top-5, plus the
// seeded top-5 from the part-C reseed.
type goldenQuery struct {
	Intent          string   `json:"intent"`
	Limit           int      `json:"limit"`
	CatalogFreeTop5 []string `json:"catalog_free_top5"`
	SeededTop5      []string `json:"seeded_top5"`
}

type goldenDoc struct {
	ContractVersion string        `json:"contract_version"`
	Queries         []goldenQuery `json:"queries"`
}

func loadGoldens(t *testing.T) goldenDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "catalog_seeding_goldens.json"))
	if err != nil {
		t.Fatalf("read goldens fixture: %v", err)
	}
	var doc goldenDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode goldens fixture: %v", err)
	}
	if doc.ContractVersion != "v1" {
		t.Fatalf("goldens contract_version = %q, want v1", doc.ContractVersion)
	}
	return doc
}

// TestCatalogSeedingGoldensSeeded pins the part-C seeded ranking for the
// report intents (ownership/leads discovery) and the core-router sanity
// set. A change here means the ranker or the seeded catalog moved —
// intended only with a reseed, which re-fills seeded_top5 from the new
// seed facts.
func TestCatalogSeedingGoldensSeeded(t *testing.T) {
	t.Parallel()

	tools := registry.Definitions()
	if len(tools) == 0 {
		t.Fatal("embedded registry is empty")
	}
	entries := registry.RouterCatalog()
	if len(entries) == 0 {
		t.Fatal("embedded router_catalog is empty")
	}
	r := rank.New(tools, entries)
	doc := loadGoldens(t)
	if len(doc.Queries) == 0 {
		t.Fatal("goldens fixture holds no queries")
	}
	allow := func(string) bool { return true }
	for _, q := range doc.Queries {
		if len(q.SeededTop5) == 0 {
			t.Fatalf("intent %q: goldens fixture has no seeded_top5", q.Intent)
		}
		got, err := r.Rank(context.Background(), router.Query{Intent: q.Intent, Limit: q.Limit}, tools, allow)
		if err != nil {
			t.Fatalf("Rank %q: %v", q.Intent, err)
		}
		top := make([]string, 0, len(got))
		for _, c := range got {
			top = append(top, c.Name)
		}
		if len(top) != len(q.SeededTop5) {
			t.Fatalf("intent %q: top = %v, want %v", q.Intent, top, q.SeededTop5)
		}
		for i := range top {
			if top[i] != q.SeededTop5[i] {
				t.Fatalf("intent %q: top = %v, want %v", q.Intent, top, q.SeededTop5)
			}
		}
	}
}

// TestCatalogSeedingGoldensCatalogFree pins the catalog-free ranking for
// the report intents (ownership/leads discovery) and the core-router sanity
// set: the nil-entries fallback path. A change here means the ranker moved
// under the current contract — intended only with a reseed.
func TestCatalogSeedingGoldensCatalogFree(t *testing.T) {
	t.Parallel()

	tools := registry.Definitions()
	if len(tools) == 0 {
		t.Fatal("embedded registry is empty")
	}
	r := rank.New(tools, nil)
	doc := loadGoldens(t)
	if len(doc.Queries) == 0 {
		t.Fatal("goldens fixture holds no queries")
	}
	allow := func(string) bool { return true }
	for _, q := range doc.Queries {
		got, err := r.Rank(context.Background(), router.Query{Intent: q.Intent, Limit: q.Limit}, tools, allow)
		if err != nil {
			t.Fatalf("Rank %q: %v", q.Intent, err)
		}
		top := make([]string, 0, len(got))
		for _, c := range got {
			top = append(top, c.Name)
		}
		if len(top) != len(q.CatalogFreeTop5) {
			t.Fatalf("intent %q: top = %v, want %v", q.Intent, top, q.CatalogFreeTop5)
		}
		for i := range top {
			if top[i] != q.CatalogFreeTop5[i] {
				t.Fatalf("intent %q: top = %v, want %v", q.Intent, top, q.CatalogFreeTop5)
			}
		}
	}
}
