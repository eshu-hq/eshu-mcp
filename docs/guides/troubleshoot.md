# Troubleshoot eshu-mcp

Every startup failure here exits with code `1`, and every message
names the fix. No silent fallbacks, which is exactly what you want at
boot. Work through in order.

## 1. `--core-base-url is required`

`eshu-mcp: --core-base-url is required`. Pass the flag, pointing at
your core.

## 2. The no-credential-posture error

Set `ESHU_API_KEY`, `ESHU_SCOPED_TOKENS_FILE`, or
`ESHU_AUTH_RESOURCE_URI` — or the local-only escape hatch, if you
really mean it. [Auth setup](auth-setup.md) compares the three.

## 3. Transport, surface, shape, or SSE-limit values refused

- `ESHU_MCP_TRANSPORT=... is not supported` — use `http` or unset it.
  This binary doesn't do stdio; stdio is the local embedded flow in
  the core.
- An invalid `ESHU_MCP_TOOL_SURFACE` or `ESHU_MCP_RESULT_SHAPE` — fix
  the value. A typo fails the boot instead of quietly serving the
  wrong surface to every client. Allowed values are in
  [Reference: configuration](../reference/configuration.md).
- An invalid SSE limit — use a positive whole number.

## 4. `registry load failed`

The embedded tool registry didn't load, so the server refuses to serve
an empty contract. Treat the build as broken and don't roll it out.

## 5. A warning about a plain-HTTP issuer

Not fatal, but the issuer's signing keys then travel unauthenticated.
Prefer an `https` issuer.

## 6. It starts, but clients get turned away

Separate the two auth layers in your head. The edge may verify JWTs
itself when discovery is configured, and the core still admits every
dispatch. The `admission` counters on `/healthz` (admitted, denied,
unavailable, cache hits) tell you which layer is saying no.

If the refusal is a `429` on `GET /sse`, that's not auth at all — it's
the session cap. See
[Operating: SSE session limits](../operations.md) and watch
`sse.rejected` on `/healthz`.
