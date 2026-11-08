# Getting started with eshu-mcp

Fastest path from nothing to a connected MCP client. You'll need Go
1.26.10 (see `go.mod`) and a running Eshu core. The shared key below
is the simplest of the three credential options.

## 1. Build and run

```bash
go build -o /tmp/eshu-mcp ./cmd/eshu-mcp
ESHU_API_KEY='change-me' /tmp/eshu-mcp --core-base-url http://localhost:8080
```

Two things will stop the boot, both on purpose: `--core-base-url` is
required, and with no credential source set at all the server refuses
to start. That refusal is deliberate — an HTTP service with no
credential posture would otherwise start wide open and say nothing.
[Auth setup](../guides/auth-setup.md) walks through your options.

## 2. Check it's alive

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/healthz
```

You're up when `/health` answers `{"status":"ok"}` and `/healthz`
shows a non-zero tool count.

## 3. Connect your client

Point your MCP client at `GET /sse` and `POST /mcp/message`, with an
`Authorization: Bearer <token>` header on both. (`eshu mcp setup` in
`eshu-cli` will generate this config for you, env-var reference
included.)

Your first calls will be `initialize`, then `tools/list`, then a
`tools/call` — the full trip is told in
[Request story](../concepts/request-story.md) and drawn in
[Diagrams](../diagrams/README.md).

## Where next?

- [Operate](../guides/operate.md) — image or binary, health, logs,
  shutdown, and the SSE session limits.
- [Auth setup](../guides/auth-setup.md) — the three credential options,
  step by step.
- [Troubleshoot](../guides/troubleshoot.md) — every startup refusal,
  decoded.
