// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rank implements the deterministic lexical router.Ranker behind
// the MCP router surface's find_tool.
//
// Lexical scores each tool with BM25 over its name, its description, and
// its seeded catalog intent phrases, weighting intents above the name and
// the name above the description. Wording repeated across many
// descriptions, such as the scoped-token refusal sentence, is removed before
// scoring. Input-schema property names let supplied anchors prefer the tools
// that accept them. A tool with no catalog entry keeps the conservative
// CostHigh default and no Canonical mark, so a contract predating the
// router_catalog section ranks exactly as the old catalog-free build did:
// equal scores order by name, then registry position, and the same query on
// the same build always returns the same order.
//
// This is the port of the in-tree rank build (go/internal/mcp/router/rank):
// this module cannot import internal/ask/catalog, so the entries ride the
// versioned contract as registry.CatalogEntry rows instead.
//
// The corpus index is built once by New. Rank filters with the caller's
// allowed func before it scores, so a hidden tool never appears and never
// shifts a visible tool's score. The package holds no runtime state beyond
// the immutable index, reads no configuration, and emits no telemetry; the
// find_tool dispatch lane owns outcome metrics.
//
// The package imports the router contract but never the mcp root, which
// injects the ranker.
package rank
