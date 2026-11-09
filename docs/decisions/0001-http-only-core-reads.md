# ADR-0001: Reach the core only over HTTP

## Status

Accepted. Recorded from [What this build serves](../skeleton.md),
which states that the server reaches core truth only through versioned
HTTP read contracts and imports no core implementation packages.

## Context

`eshu-mcp` was split out of the core so the MCP surface — tool
registry, protocol handling, dispatch — could ship on its own
schedule. Importing core storage code, or letting this edge decide
access on its own, would have coupled releases and duplicated the
core's admission logic in a second place.

## Decision

Every tool call becomes a versioned, bounded HTTP read against the
core (30 seconds, 262144 bytes, envelope `Accept`), with the caller's
credential forwarded verbatim. The core remains the admission
authority for every dispatch; edge checks only turn bad callers away
early. With no credential source configured, startup fails rather
than serving wide open.

## Consequences

- One admission authority, one data layer: auditing access means
  auditing the core. Auditing this repo means auditing translation —
  see [Boundaries](../concepts/boundaries.md).
- A bad token is refused before it can browse the catalogue, not
  halfway through a session.
- The cost is a hop per call and a hard dependency on a reachable
  core (`--core-base-url` is required). The
  [parity record](../parity.md) tracks the evidence that the hop
  preserves behaviour.
