# Set up auth for eshu-mcp

With no credential source configured at all, the server won't start.
That's the server protecting you from yourself. Pick one of the three
proper options below; exact env names are in
[Reference: configuration](../reference/configuration.md).

Whichever you pick, remember the division of labour: the edge forwards
the caller's credential to the core on every dispatch, and the core
stays in charge of admission. Edge checks turn bad callers away early.
They don't replace the core's call.

## Option A: the shared key (simplest)

```bash
ESHU_API_KEY='change-me' /tmp/eshu-mcp \
  --core-base-url http://localhost:8080
```

Callers present the key as a Bearer token and share one identity —
which also means they share one SSE session allowance of 32. Fine for
a small team. Before you hand it to a big one, read
[Operating: SSE session limits](../operations.md).

## Option B: a scoped-token file

Set `ESHU_SCOPED_TOKENS_FILE` to the path of your scoped-token file.
Callers get their own tokens and identities, and their own session
allowances.

## Option C: OAuth discovery

Set `ESHU_AUTH_RESOURCE_URI` plus `ESHU_AUTH_AUTHORIZATION_SERVERS`
(space-separated issuer URLs). That turns on discovery at
`/.well-known/oauth-protected-resource` and lets the edge verify
Bearer JWTs itself. Discovery only counts as enabled when the resource
URI is valid and at least one issuer is set. Prefer `https` issuers:
if an issuer URL uses plain HTTP outside loopback, startup logs a
warning, because its signing keys then travel unauthenticated.

## The escape hatch (local use only)

`ESHU_MCP_ALLOW_UNAUTHENTICATED=true` skips the edge check for local
or loopback work and logs a warning at startup so you can't set it by
accident and forget. Even then, every dispatch still forwards whatever
credential arrived to the core. Never expose this port publicly with
it set.

## Connect your client

Your client sends its credential as `Authorization: Bearer <token>` on
both `GET /sse` and `POST /mcp/message`. Store the URL and header in
your client's own config format, and use an env-var reference for the
token if your client supports one. Don't paste raw secrets into shared
config files. (`eshu mcp setup` in `eshu-cli` will generate this
config for you, env-var reference included.)

Keys travel by env only, never by flag, so they stay out of process
listings.

Server starts but clients get turned away? See
[Troubleshoot](troubleshoot.md).
