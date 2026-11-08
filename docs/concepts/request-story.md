# One tool call's story

Here's a single call from connect to result. The drawn version is in
[Diagrams](../diagrams/README.md).

## The trip

1. You connect and post `initialize`; we agree on protocol versions.
2. You ask `tools/list`; we answer with what this surface advertises.
3. You call one tool with a Bearer token.
4. Each call becomes a bounded HTTP read against the core with your
   credential attached, asking for `application/eshu.envelope+json`.
5. The core's envelope reply gets reshaped into an MCP tool result.

If your credential doesn't convince anyone, you're turned away at the
door rather than halfway through — see [Boundaries](boundaries.md).

## Surfaces: what `tools/list` advertises

What gets advertised depends on `ESHU_MCP_TOOL_SURFACE`:

- `full` (the default) advertises all 171 tools.
- `router` advertises just `find_tool` and `call_tool`.
- `core_router` advertises 10 core tools plus the router pair.

Advertisement is only about discovery. A direct call to a registered
tool still works on every surface, so shrinking the list saves your
client context without locking any capability away. Set
`ESHU_MCP_QUERY_PROFILE=production` and the production-hidden tools
drop out of the served set entirely. Any other value keeps the full
list.

## Bounds and shapes on the way out

Every dispatch runs into two hard bounds from the contract file:
30 seconds and 262144 bytes. The result layout is yours to pick with
`ESHU_MCP_RESULT_SHAPE`. `dual` (the default) sends text content plus
structured content. `structured` sends one text summary plus
structured content, with no embedded copy. Pick whichever your client
handles better. The server won't guess: a bad value fails startup,
loudly, which beats finding out mid-session.

Exact values for every knob mentioned here are in
[Reference: configuration](../reference/configuration.md).
