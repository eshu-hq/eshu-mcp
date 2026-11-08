# Architecture: eshu-mcp

## Place in Eshu

`eshu-mcp` is the interpreter between AI assistants and Eshu. An MCP
client (editor or agent) talks HTTP + SSE to this repo; we turn each
tool call into a versioned HTTP read against the Eshu core, forwarding
the caller's credential; the core alone holds the graph and durable
stores, and keeps the final say on who's allowed in. The repo
[README](../../README.md) keeps the one-screen version, and
[Diagrams](../diagrams/README.md) draws the topology.

Sibling repos: `eshu` holds the core, `eshu-api` is the HTTP front
door, `eshu-cli` is the command line (its `mcp setup` command can write
client config for you), and
[eshu-deploy](https://github.com/eshu-hq/eshu-deploy) runs the full
stack. For scope of the current build, see
[What this build serves](../skeleton.md).

## Components

| Component | Job |
| --- | --- |
| `cmd/eshu-mcp` | The binary. Parses `--addr` / `--core-base-url`, folds the credential posture from env, wires admission and SSE limits, serves. Also exposes `GET /healthz`. |
| `doc.go`, `registry.go`, `definitions.go` | The served tool registry, embedded from [tools.json](../../contracts/tool-registry/v1/tools.json). Names and order are contractual. |
| `internal/mcpserver/` | The HTTP transport: `GET /health`, `GET /sse`, `POST /mcp/message`, edge auth, and the OAuth discovery document. |
| `internal/router/` | Request validation, surface advertisement, and the `find_tool` / `call_tool` engine. |
| `internal/dispatch/` | The tool families: turning a call into a core route, within the pinned time and byte budgets, and rendering the envelope reply. |
| `internal/readcontract/` | The versioned HTTP client seam to the core (`30s`, `262144` bytes, envelope `Accept`). |
| `internal/resultshape/`, `internal/routecontract/`, `internal/sourcetool/` | Result-layout choice, route contracts, and source-tool plumbing. |
| `contracts/` | The tool-registry contract and the [shared shapes](../../contracts/shared-shapes/v1/SHARED_SHAPES.md) shared with the other surfaces. |

How a call travels through these parts is told in
[Request story](../concepts/request-story.md); ownership lines are in
[Boundaries](../concepts/boundaries.md).

## Stack

- Go 1.26.10 (see `go.mod`), module `github.com/eshu-hq/eshu-mcp`.
  Bearer JWT verification uses `github.com/golang-jwt/jwt/v5`.
- Image: multi-arch (`linux/amd64,linux/arm64`) build stamping the
  release version, onto a distroless non-root base with no shell;
  published as `ghcr.io/eshu-hq/eshu-mcp` with SLSA provenance and an
  SPDX SBOM — see [Release](../release.md).
- Contract gate: `python3 scripts/check-parity.py` keeps the served
  registry honest against the pinned contract.

The load-bearing decision behind this shape — reach the core only over
HTTP and let the core admit every call — is recorded in
[ADR-0001](../decisions/0001-http-only-core-reads.md).
