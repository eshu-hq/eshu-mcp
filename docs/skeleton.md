# Walking skeleton (Refs `eshu-hq/eshu#9498`, part of `eshu-hq/eshu#6707`)

The destination builds, tests, and ships as its own module. It serves the
pinned 173-tool registry and reaches core truth only through versioned HTTP
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
  and otherwise forwards the caller credential verbatim to core, plus the
  RFC 9728 OAuth discovery document and 401 challenge when
  `ESHU_AUTH_RESOURCE_URI` and `ESHU_AUTH_AUTHORIZATION_SERVERS` are set.
- `cmd/eshu-mcp/`: the binary. It exposes `GET /healthz` (contract
  version, tool count, core origin) plus the transport routes.
  `--core-base-url` is required. The edge credential posture folds the
  same three knobs as the in-tree server (`ESHU_API_KEY`,
  `ESHU_SCOPED_TOKENS_FILE`, `ESHU_AUTH_RESOURCE_URI`); the key never
  appears in process flags, and every dispatch forwards the caller
  credential to core for admission.
- `Dockerfile`: multi-stage build, digest-pinned non-root runtime, no shell.

## Proof (local, this increment)

- `go build ./...`, `go test ./... -count=1`, and the race suite pass.
- `GET /healthz` answers the contract version, 173 tools, and the
  core origin from the built binary; unknown paths answer 404.
- `python3 scripts/check-parity.py` passes: 173 tools, first `find_code`,
  tail `search_php_configuration` + `list_ssm_host_inventory` (#9670) +
  `list_service_api_endpoints` (#10477) +
  `list_service_downstream_consumers` (#10535), source commit `9922b49`.

## NOT_CHECKED

The release lane has cut `v0.2.7` and `v0.2.9` (auto-tag, then a dispatched
release run) and `v0.2.8` (a human tag push), each with Trivy, provenance, SBOM and
`verify-published` green; `docs/release.md` Status records the run ids and
digests. QA deployment from the destination artifact is DONE (`0.2.9`
serving 1/1 against `http://eshu:8080`); parity re-proof against the live
core path is DONE for transport, auth gates, hostile shapes, and router
find/call (log on `eshu-hq/eshu#9498`, 2026-11-03), with the
router-surface/ranker delta open per `eshu-hq/eshu#9521`; rollback is
REHEARSED (edge to 0 and back to 1, same log). Tool dispatch and
authentication against live core are DONE (same log). The arbiter's
version-pair and shared-shape-publisher decisions are DECIDED (record on
`eshu-hq/eshu#9498`; `README.md` Compatibility). Still open: concurrency
proofs, owner production acceptance, and ratifying the in-tree removal
(`cf357fcf47` removed `go/cmd/mcp-server` before acceptance).
