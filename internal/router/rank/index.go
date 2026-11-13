// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	registry "github.com/eshu-hq/eshu-mcp"
)

// Field weights. A curated intent phrase is the strongest signal, the tool
// name is next, and the free-text description is the weakest because it
// carries the most incidental vocabulary. The values mirror the in-tree
// ranker; tuning them is the in-tree eval harness's job (#8756), never this
// port's.
const (
	weightIntent = 3.0
	weightName   = 2.0
	weightDesc   = 1.0
)

// unitKind says which free-text source a Why candidate came from.
type unitKind int

const (
	unitIntent unitKind = iota
	unitSentence
)

// unit is one description sentence kept for the Why line.
type unit struct {
	text  string
	kind  unitKind
	terms map[string]struct{}
}

// doc is the immutable, precomputed form of one tool.
type doc struct {
	name      string
	cost      string
	costRank  int
	canonical bool
	// tf maps a stemmed term to its field-weighted frequency in this tool.
	tf map[string]float64
	// length is the field-weighted token count, the BM25 document length.
	length    float64
	nameTerms map[string]struct{}
	units     []unit
	props     map[string]struct{}
	schema    map[string]any
}

// index is the immutable corpus Rank scores against. Build it once.
//
// It holds the tools slice it was built from. Holding it keeps the backing
// array alive, and Rank compares identity against this reference.
type index struct {
	docs  []*doc
	tools []registry.ToolDefinition
}

// costRank orders cost classes cheap to expensive. Anything unrecognized
// ranks as high, matching the catalog's conservative default for
// unannotated surfaces.
func costRank(c string) int {
	switch c {
	case CostLow:
		return 0
	case CostModerate:
		return 1
	default:
		return 2
	}
}

// buildIndex tokenizes every tool once and strips registry-wide boilerplate.
// entries carries the seeded catalog rows keyed by tool name; a nil map (a
// contract predating the router_catalog section) reproduces today's exact
// catalog-free behavior.
func buildIndex(tools []registry.ToolDefinition, entries map[string]registry.CatalogEntry) *index {
	idx := &index{
		docs:  make([]*doc, 0, len(tools)),
		tools: tools,
	}
	boiler := findBoilerplate(tools)
	for _, t := range tools {
		idx.docs = append(idx.docs, buildDoc(t, entries[t.Name], boiler))
	}
	return idx
}

// buildDoc tokenizes one tool. The cost and Canonical mark come from the
// tool's catalog entry; a tool with no entry — or an entry with an empty
// cost — keeps the conservative CostHigh default and no Canonical mark.
// Intent phrases score above the name and description and surface in the
// Why line through the unitIntent kind, mirroring the in-tree buildDoc.
func buildDoc(t registry.ToolDefinition, entry registry.CatalogEntry, boiler *boilerplate) *doc {
	d := &doc{
		name:      t.Name,
		cost:      entry.Cost,
		canonical: entry.Canonical,
		tf:        map[string]float64{},
		nameTerms: map[string]struct{}{},
		props:     map[string]struct{}{},
	}
	if d.cost == "" {
		d.cost = CostHigh
	}
	d.costRank = costRank(d.cost)
	add := func(toks []string, w float64) map[string]struct{} {
		set := make(map[string]struct{}, len(toks))
		for _, tok := range toks {
			d.tf[tok] += w
			d.length += w
			set[tok] = struct{}{}
		}
		return set
	}
	d.nameTerms = add(tokens(t.Name), weightName)
	for _, phrase := range entry.Intents {
		d.units = append(d.units, unit{text: phrase, kind: unitIntent, terms: add(tokens(phrase), weightIntent)})
	}
	for _, s := range sentences(t.Description) {
		kept := boiler.keep(s)
		if len(kept) == 0 {
			continue
		}
		d.units = append(d.units, unit{text: s, kind: unitSentence, terms: add(kept, weightDesc)})
	}
	if schema, ok := t.InputSchema.(map[string]any); ok {
		d.schema = schema
		for _, name := range propertyNames(schema) {
			d.props[name] = struct{}{}
		}
	}
	return d
}

// matches reports whether the caller handed Rank the very slice the index was
// built from: the same backing array and length. Identity, not equality, is
// the test, so a new slice is always ranked from a fresh index even when its
// names look the same. Editing a retained slice's elements in place is not
// detected; callers must not.
func (x *index) matches(tools []registry.ToolDefinition) bool {
	if len(tools) != len(x.tools) {
		return false
	}
	if len(tools) == 0 {
		return true
	}
	return &tools[0] == &x.tools[0]
}
