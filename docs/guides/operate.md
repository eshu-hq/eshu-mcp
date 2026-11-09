# Operate eshu-mcp

Day-to-day running first: get it up, tell whether it's healthy, read
the logs. Then the one nasty surprise — SSE session limits — in its
own standing page. Read that before a shared-key deploy.

## Step 1: run it

Releases publish `ghcr.io/eshu-hq/eshu-mcp`. The `Dockerfile` compiles
with Go 1.26.10, stamps the build version, and drops the binary onto a
distroless base with no shell. The image exposes port `8080` and
starts `/eshu-mcp`.

```bash
docker build -t eshu-mcp:dev .
docker run --rm -p 8080:8080 -e ESHU_API_KEY='change-me' \
  eshu-mcp:dev --core-base-url http://host.docker.internal:8080
```

Running the binary directly is the
[Getting started](../getting-started/README.md) path: build
`./cmd/eshu-mcp`, set one credential source, pass `--core-base-url`.
For the full stack, core included, use
[eshu-deploy](https://github.com/eshu-hq/eshu-deploy). This repo alone
doesn't deploy the core.

## Step 2: check health

Both health endpoints are public, so probes need no token:

- `GET /health` returns `200` with `{"status":"ok"}`. Use it as your
  liveness probe.
- `GET /healthz` returns `200` with the contract version, the
  advertised tool count, your core URL, `sse.active` and
  `sse.rejected`, and admission counters when transport auth is wired.
  This is the one to watch after a deploy. A tool count of zero means
  something is badly wrong, and a growing `sse.rejected` means callers
  are hitting the session limit (see Step 4).

## Step 3: read logs, plan shutdown

Logs are JSON lines on stdout. At startup, look for `eshu-mcp serving`
with the contract version, advertised tool count, surface, core URL,
and listen address, then `mcp server started`. If OAuth discovery is
on, you'll also see `oauth discovery enabled`. A boot failure exits
with code `1` and logs `eshu-mcp exited` with the error.

On shutdown the HTTP server drains with a five-second bound once its
context is cancelled. The binary itself doesn't trap SIGTERM or SIGINT
today, so in practice an orchestrator's signal ends the process. Keep
that in mind for rolling deploys: SSE clients will drop and rely on
their own reconnect behaviour.

## Step 4: plan for SSE session limits

Each principal may hold at most 32 open SSE sessions, and everyone
behind the shared `ESHU_API_KEY` counts together as one principal.
Before a shared-key deploy or a re-pin at v0.2.8 or later, read the
standing page:
[Operating: SSE session limits](../operations.md). It covers what a
refused caller sees, how to size
`ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL`, and what to watch on
`/healthz`.

## Releases

Pushes to `main` and version-tag runs build the image, scan it with
Trivy (any `HIGH` or `CRITICAL` CVE blocks the release), then push
`ghcr.io/eshu-hq/eshu-mcp` for `linux/amd64,linux/arm64` with SLSA
provenance and an SPDX SBOM. Tag runs also cut a GitHub Release, and a
PR merged with the `release:patch` label gets the next patch tag
automatically. [Release](../release.md) has the tag rules and digest
checks.

Won't start, or clients get turned away? See
[Troubleshoot](troubleshoot.md).
