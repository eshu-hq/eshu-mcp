# Boundaries: what eshu-mcp owns (and never touches)

Your AI assistant is good at calling tools. Eshu is good at answering
questions about code and cloud systems. This server is the interpreter
between the two. It speaks MCP on one side and plain versioned HTTP
reads on the other, and it owns nothing in between except the
translation. The picture is in [Diagrams](../diagrams/README.md).

## We own

- The tool registry: 173 tools, names and order pinned in
  [tools.json](../../contracts/tool-registry/v1/tools.json) (contract
  `v1`, `registry.tool_count: 173`), embedded at build time. If that
  file fails to load, the server refuses to start rather than
  cheerfully serving health with zero tools.
- The protocol handling: `GET /sse`, `POST /mcp/message`,
  `initialize`, `tools/list`, `tools/call` — including the router pair
  `find_tool` and `call_tool`, which help clients search the full list
  instead of swallowing it whole.
- Dispatch and reply shaping: each call becomes a bounded HTTP read,
  and the core's envelope reply becomes an MCP tool result.

## The core owns

- The data: the graph and every durable store behind it.
- Admission: we forward your credential to the core on every dispatch,
  and the core remains the admission authority. Edge checks (when
  you've configured them) turn bad callers away early; they don't
  replace the core's call. Credentials we can't verify locally still
  have to be admitted by the core before `initialize`, `tools/list`,
  `ping`, or an SSE session succeeds, so a bad token doesn't get to
  browse the catalogue first and fail later.

## We never touch

- Core storage code or stores. We import no core storage code and
  reach the core only over HTTP — that decision is recorded in
  [ADR-0001](../decisions/0001-http-only-core-reads.md).
- Access rules or stored data. Changing those is core work; terminal
  commands are `eshu-cli`; the plain HTTP API is `eshu-api` or the
  core.

One hard rule follows from all this: with no credential source
configured at all, the server won't start. The escape hatch and the
three proper options are in [Auth setup](../guides/auth-setup.md).
