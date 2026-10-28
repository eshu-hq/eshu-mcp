# eshu-mcp

Eshu MCP protocol handling and tool dispatch. Pre-release: holds no cutover
authority until its reviewed cutover. Refs #8873, part of #6707.

## Scope

This repository owns MCP protocol handling, tool registration, dispatch, and
adaptation to supported read contracts, served as versioned read-contract
clients.

Core-owned and staying in `eshu`: read-model implementation, canonical graph
projection, durable stores, producer admission authority, redaction policy,
and the in-tree `mcp-server` composition root (including the Postgres-backed
runtime surface). This repository never imports core implementation handles;
at cutover it replaces `wiring_router.go`-style composition with versioned
HTTP read-contract clients.

Adapters stay separate repositories. Shared response and result shapes get
one explicit public contract with a single publisher, never whichever adapter
imports them first. See
[shared shapes](contracts/shared-shapes/v1/SHARED_SHAPES.md).

## Layout

- `contracts/tool-registry/v1/tools.json`: versioned tool-contract surface.
  168 registry tools in source registration order, plus the router surface,
  dispatch bounds, and envelope MIME. Order is contractual.
- `contracts/shared-shapes/v1/SHARED_SHAPES.md`: the shared envelope/result
  shapes and their single-publisher rule.
- `docs/source/CUTOVER_OWNERSHIP.md`: verbatim copy of the source ownership
  map at the pinned source commit. Byte-parity is proven in
  [parity](docs/parity.md).
- `docs/provenance.md`: source commit, file mapping, and hashes.
- `docs/parity.md`: parity method and seed-time results.

## Checks

Run `python3 scripts/check-parity.py`. CI runs the same check on every push
and pull request. The in-tree implementation is removed only after production
acceptance.
