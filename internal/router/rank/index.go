// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	registry "github.com/eshu-hq/eshu-mcp"
)

// Field weights. The tool name is the strongest signal and the free-text
// description is weaker because it carries the most incidental vocabulary.
// With no catalog entries there are no curated intent phrases.
const (
	weightName = 2.0
	weightDesc = 1.0
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
func buildIndex(tools []registry.ToolDefinition) *index {
	idx := &index{
		docs:  make([]*doc, 0, len(tools)),
		tools: tools,
	}
	boiler := findBoilerplate(tools)
	for _, t := range tools {
		idx.docs = append(idx.docs, buildDoc(t, boiler))
	}
	return idx
}

// buildDoc tokenizes one tool. With no catalog entry every tool keeps the
// conservative CostHigh default and no Canonical mark.
func buildDoc(t registry.ToolDefinition, boiler *boilerplate) *doc {
	d := &doc{
		name:      t.Name,
		cost:      CostHigh,
		tf:        map[string]float64{},
		nameTerms: map[string]struct{}{},
		props:     map[string]struct{}{},
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
