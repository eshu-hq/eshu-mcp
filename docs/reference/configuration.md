# Reference: configuring eshu-mcp

Two flags and a handful of env vars. The flag you'll forget is
`--core-base-url`, and the env decision that matters most is which
credential source you pick. Everything else has a sane default and can
wait until you have a reason. For a walkthrough, see
[Auth setup](../guides/auth-setup.md).

| Setting | Required | Default | What it does |
| --- | --- | --- | --- |
| `--addr` | No | `:8080` | Address and port to listen on |
| `--core-base-url` | Yes | none | Core read-API origin, for example `https://core:8443` |
| `ESHU_API_KEY` (env) | One of three* | empty | Shared key. Everyone who presents it shares one identity |
| `ESHU_SCOPED_TOKENS_FILE` (env) | One of three* | empty | Path to the scoped-token file |
| `ESHU_AUTH_RESOURCE_URI` (env) | One of three* | empty | OAuth resource URI. Pairs with the issuers below |
| `ESHU_AUTH_AUTHORIZATION_SERVERS` (env) | No | empty | Space-separated issuer URLs for OAuth discovery |
| `ESHU_AUTH_RESOURCE_DOCUMENTATION` (env) | No | empty | Copied into the discovery document as-is |
| `ESHU_AUTH_PREREGISTERED_CLIENT_ID` (env) | No | empty | Copied into the discovery document as-is |
| `ESHU_MCP_ALLOW_UNAUTHENTICATED` (env) | No | off | Truthy (`1`, `true`, `t`, `yes`, `y`, `on`) starts with no edge check. Local use only |
| `ESHU_MCP_TRANSPORT` (env) | No | unset | Only `http` or unset is accepted. This binary serves HTTP only |
| `ESHU_MCP_TOOL_SURFACE` (env) | No | `full` | `full`, `router`, or `core_router`. Controls what `tools/list` advertises |
| `ESHU_MCP_RESULT_SHAPE` (env) | No | `dual` | `dual` or `structured`. Controls the tool-result layout |
| `ESHU_MCP_QUERY_PROFILE` (env) | No | empty | Exactly `production` hides the production-hidden tools |
| `ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL` (env) | No | `32` | Open SSE sessions per identity. Positive whole number |
| `ESHU_MCP_MAX_SSE_SESSIONS_TOTAL` (env) | No | `2048` | Open SSE sessions for the whole process. Positive whole number |

*Set at least one of the three, or the escape hatch. Keys travel by
env only, never by flag, so they stay out of process listings.

The simplest working setup:

```bash
ESHU_API_KEY='change-me' /tmp/eshu-mcp \
  --core-base-url http://localhost:8080
```

## Picking a credential source

With none of the three set, startup fails with an error that names
your options. That's the server protecting you from yourself: an HTTP
service with no credential posture would otherwise start wide open and
say nothing.

- The shared key (`ESHU_API_KEY`) is the easy path. Callers present it
  as a Bearer token and share one identity, which also means they
  share one SSE session allowance. Fine for a small team, worth
  reading [Operating: SSE session limits](../operations.md) before you
  hand it to a big one.
- A scoped-token file (`ESHU_SCOPED_TOKENS_FILE`) gives callers their
  own tokens and identities.
- OAuth (`ESHU_AUTH_RESOURCE_URI` plus
  `ESHU_AUTH_AUTHORIZATION_SERVERS`) turns on discovery at
  `/.well-known/oauth-protected-resource` and lets the edge verify
  Bearer JWTs itself. Discovery only counts as enabled when the
  resource URI is valid and at least one issuer is set. If an issuer
  URL uses plain HTTP outside loopback, startup logs a warning,
  because its signing keys then travel unauthenticated. Prefer
  `https`.

Whichever you pick, remember the division of labour: the edge forwards
the caller's credential to the core on every dispatch, and the core
stays in charge of admission. Edge checks turn bad callers away early.
They don't replace the core's call. The escape hatch,
`ESHU_MCP_ALLOW_UNAUTHENTICATED=true`, skips the edge check for local
or loopback work and logs a warning at startup so you can't set it by
accident and forget. Never expose this port publicly with it set.

Your client sends its credential as `Authorization: Bearer <token>` on
both `GET /sse` and `POST /mcp/message`. Store the URL and header in
your client's own config format, and use an env-var reference for the
token if your client supports one. Don't paste raw secrets into shared
config files. (`eshu mcp setup` in `eshu-cli` will generate this
config for you, env-var reference included.)

## Surfaces, shapes, and profiles

These three knobs shape what clients see, and all three fail fast on a
typo rather than falling back silently:

- `ESHU_MCP_TOOL_SURFACE` takes `full` (all 171 tools advertised),
  `router` (just `find_tool` and `call_tool`), or `core_router`
  (10 core tools plus the pair). Matching is case-insensitive. Direct
  calls to registered tools work on every surface.
- `ESHU_MCP_RESULT_SHAPE` takes `dual` or `structured`,
  case-insensitive. `dual` sends text plus structured content;
  `structured` sends one text summary plus structured content.
- `ESHU_MCP_QUERY_PROFILE` only reacts to the exact value `production`
  (surrounding spaces are trimmed, case is not forgiven). That hides
  the production-hidden tools. Anything else keeps the full list.

`ESHU_MCP_TRANSPORT` deserves one line: this binary is a network
service and serves HTTP only. Ask for `stdio` and startup refuses,
because stdio is the local embedded flow in the core, not something we
should pretend to offer.

The two SSE limits are covered properly, with the shared-key maths, in
[Operating: SSE session limits](../operations.md). Short version:
leave them alone until `/healthz` tells you not to.
