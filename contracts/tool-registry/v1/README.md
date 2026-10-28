# Tool registry contract v1

Versioned surface of the MCP tool registry extracted for
`eshu-hq/eshu#8873`. Source: `go/internal/mcp/tool_registry_census_test.go`
at the commit pinned in `tools.json`.

## Rules

- `tool_order` reproduces the source census byte-for-byte in registration
  order. Any add, remove, rename, or reorder is a contract change: it needs a
  new contract version and a lockstep source census update.
- `router_surface`, `dispatch_bounds`, and `envelope` pin the values the
  source census tests pin. Do not change them without the matching source
  proof.
- Family-to-backing-route attribution lives in the verbatim ownership map at
  `docs/source/CUTOVER_OWNERSHIP.md`, not here. This file carries the
  machine-checked surface; the map carries the human-readable ownership.
