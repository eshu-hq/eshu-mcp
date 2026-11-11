# eshu-mcp docs

Welcome in. `eshu-mcp` lets AI assistants use Eshu as tools over MCP
(Model Context Protocol): your assistant calls a tool, we turn it into
a read against the Eshu core, and the result comes back in MCP shape.
New here? Start with [Getting started](getting-started/README.md).

## Start here

| Page | Read it when… |
| --- | --- |
| [Getting started](getting-started/README.md) | You want the server running and a client connected |
| [Guides](guides/README.md) | You need step-by-step help: operate (including SSE session limits), pick a credential, troubleshoot |
| [Concepts](concepts/README.md) | You want the mental model: what we own, what the core decides, how a tool call travels |
| [Reference](reference/README.md) | You need an exact flag, env var, surface, or result shape |
| [Architecture](architecture/README.md) | You want our place in Eshu, the components, and the stack |
| [Diagrams](diagrams/README.md) | You think in pictures (every diagram lives here, with a text version) |
| [Decisions](decisions/README.md) | You want the recorded architecture decisions (ADRs) |
| [Contributing](contributing/README.md) | You're changing code and want setup, `make`, and PR rules |

## Standing references in this folder

These pages predate the tree above and stay where they are:

- [Operating: SSE session limits](operations.md) — read before any
  shared-key deploy or re-pin at v0.2.8 or later.
- [What this build serves](skeleton.md) — scope of the current build.
- [Release](release.md) — tags, images, and how to check a published
  digest.
- [Tool registry contract](../contracts/tool-registry/v1/tools.json) —
  the 173 tools, in contract order, with pinned read bounds.
- [Shared shapes](../contracts/shared-shapes/v1/SHARED_SHAPES.md) —
  wire shapes shared with the other Eshu surfaces.

## Maintainer history

Extraction records, kept for provenance rather than day-to-day reading:

- [Parity](parity.md) and [Provenance](provenance.md) — what moved
  where, with source pins.
- [Cutover ownership](source/CUTOVER_OWNERSHIP.md) and the QA notes in
  `qa/` (for example `qa/0.2.9-parity-20261103.md`).

The repo [README](../README.md) has the one-screen version.
