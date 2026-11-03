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

## Compatibility

This repository is the authoritative publisher of the 171-tool MCP
registry (arbiter ruling recorded on `eshu-hq/eshu#9498`). Core (`eshu`)
publishes only the HTTP/OpenAPI contract this repository consumes; any
tool add/remove/rename must land here with a contract test against core
OpenAPI (forward rule — `scripts/check-parity.py` covers in-repo
count/order/pins only; the core-OpenAPI gate does not exist yet).

| Dest | Core | Provenance |
|---|---|---|
| `0.2.9` (`sha256:a3fbe4…`) | `50a0ddd3335422c0454be75c9a160a4083a799b7` (image `sha256:6a7966…`) | `docs/qa/0.2.9-parity-20261103.md` (transport/auth/envelope + rollback, 2026-11-03) |

Oldest supported = newest supported = the row above. No N-1/N+1 blanket
claim. Widen the window only by adding a proven row, never by inference.
The router-surface/ranker delta (blocker 3 on `eshu-hq/eshu#9521`) stays
open and is out of scope of this row: this row proves the deployed
transport, auth gates, hostile shapes, and router find/call path only.
This section supersedes the open questions in
`docs/source/CUTOVER_OWNERSHIP.md`, which stays byte-exact as the pinned
verbatim source copy (parity-gated) and cannot carry the ruling itself.

## Layout

- `contracts/tool-registry/v1/tools.json`: versioned tool-contract surface.
  171 registry tools in source registration order, plus the router surface,
  dispatch bounds, and envelope MIME. Order is contractual.
- `contracts/shared-shapes/v1/SHARED_SHAPES.md`: the shared envelope/result
  shapes and their single-publisher rule.
- `docs/source/CUTOVER_OWNERSHIP.md`: verbatim copy of the source ownership
  map at the pinned source commit. Byte-parity is proven in
  [parity](docs/parity.md).
- `docs/provenance.md`: source commit, file mapping, and hashes.
- `docs/parity.md`: parity method and seed-time results.
- `docs/skeleton.md`: the walking-skeleton server, its proof, and what stays
  unchecked.
- `docs/operations.md`: operator note on the per-principal SSE session
  limit (32; everyone behind the shared key shares it). Read before a
  re-pin to v0.2.8 or later.
- `docs/release.md`: tag `v*` and `main`-push release to GHCR with
  provenance, SBOM, and Trivy gate (see `.github/workflows/release.yml`).
- `registry.go` (module root): the served registry, embedded from the
  tool-contract surface.
- `internal/readcontract/`: the versioned HTTP read-contract client seam.
- `cmd/eshu-mcp/`: the binary serving `GET /healthz` plus the MCP
  transport routes (`GET /health`, `GET /sse`, `POST /mcp/message`) over
  the 171-tool registry.

## Checks

Run `python3 scripts/check-parity.py`. CI runs the same check on every push
and pull request. The in-tree implementation is removed only after production
acceptance.

Path-aware gates (`go-ci / lint`, `go-ci / test`, `go-ci / race`,
`go-ci / shell`, `go-ci / image`, `go-ci / govulncheck`, `go-ci / gosec`,
plus the top-level `ci-complete` alias) run from
`.github/workflows/ci.yml`; `make ci` is
the local parity command. See [CONTRIBUTING.md](CONTRIBUTING.md).
