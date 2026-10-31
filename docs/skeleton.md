# Walking skeleton (Refs `eshu-hq/eshu#9498`, part of `eshu-hq/eshu#6707`)

The destination builds, tests, and ships as its own module. It serves the
pinned 171-tool registry and reaches core truth only through versioned HTTP
read contracts. Tool dispatch, the router, the HTTP/SSE transport, and edge
credential handling are landed: this is the full read path, proven by the
independent build, test, and image steps.

## What is here

- `go.mod`: module `github.com/eshu-hq/eshu-mcp`, stdlib only. No import of
  core implementation packages exists anywhere in this tree.
- `doc.go`, `registry.go`: the served registry, embedded from
  `contracts/tool-registry/v1/tools.json`. Names and order are contractual;
  drift fails the build. A load failure fails startup fast instead of
  serving an empty contract.
- `internal/readcontract/`: the versioned HTTP client seam. Pinned bounds
  are `DefaultTimeout` 30s, `DefaultResponseByteBudget` 262144 bytes, and
  the `application/eshu.envelope+json` Accept header. Live reads are capped
  by the budget on the wire, and client construction validates its bounds.
  The bearer token travels only in the Authorization header and is never
  logged.
- `internal/dispatch/`: the 27 ported tool families plus the response
  budget, envelope rendering, and sweep-verified route resolutions.
- `internal/router/`: request validation, surface advertisement, and the
  `call_tool`/`find_tool` engine with the catalog-free lexical ranker
  (`internal/router/rank`, built from the versioned registry tools alone;
  a nil ranker still answers ranker-unavailable).
- `internal/mcpserver/`: the HTTP transport (`GET /health`, `GET /sse`,
  `POST /mcp/message`) with edge auth that refuses credential-less callers
  and otherwise forwards the caller credential verbatim to core.
- `cmd/eshu-mcp/`: the binary. It exposes `GET /healthz` (contract
  version, tool count, core origin) plus the transport routes.
  `--core-base-url` is required. `ESHU_API_KEY` sets the edge credential
  posture; the key never appears in process flags, and every dispatch
  forwards the caller credential to core for admission.
- `Dockerfile`: multi-stage build, digest-pinned non-root runtime, no shell.

## Proof (local, this increment)

- `go build ./...`, `go test ./... -count=1`, and the race suite pass.
- `GET /healthz` answers the contract version, 171 tools, and the
  core origin from the built binary; unknown paths answer 404.
- `python3 scripts/check-parity.py` passes: 171 tools, first `find_code`,
  tail `search_php_configuration` + `list_ssm_host_inventory` (#9670),
  source commit `9922b49`.

## NOT_CHECKED

The release lane is wired (`release.yml`, multi-arch image, version stamp,
provenance, scan record) but no tag has been cut from it: the first `v*`
push, its digest, and its provenance/Trivy proof stay `NOT_CHECKED` until
an owner cuts that tag. QA deployment from the destination artifact, parity
re-proof against the live core path, rehearsed rollback, owner production
acceptance, and in-tree removal stay open too. Tool dispatch,
authentication against live core, concurrency proofs, and the arbiter's
version-pair and shared-shape-publisher decisions all stay open under #9498.
