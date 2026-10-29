# Walking skeleton (Refs `eshu-hq/eshu#9498`, part of `eshu-hq/eshu#6707`)

The destination builds, tests, and ships as its own module. It serves the
pinned 168-tool registry and reaches core truth only through versioned HTTP
read contracts. Tool dispatch over those clients lands next; this skeleton
proves the independent build, test, and image steps first.

## What is here

- `go.mod`: module `github.com/eshu-hq/eshu-mcp`, stdlib only. No import of
  core implementation packages exists anywhere in this tree.
- `doc.go`, `registry.go`: the served registry, embedded from
  `contracts/tool-registry/v1/tools.json`. Names and order are contractual;
  drift fails the build.
- `internal/readcontract/`: the versioned HTTP client seam. Pinned bounds
  are `DefaultTimeout` 30s, `DefaultResponseByteBudget` 262144 bytes, and
  the `application/eshu.envelope+json` Accept header. The bearer credential
  travels only in the Authorization header and is never logged.
- `cmd/eshu-mcp/`: the binary. Today it exposes `GET /healthz` (contract
  version, tool count, core origin). `--core-base-url` is required. The
  server carries no credential yet; the `ESHU_API_KEY` env contract arrives
  with tool dispatch so the key never appears in process flags.
- `Dockerfile`: multi-stage build, non-root runtime, no shell.

## Proof (local, this increment)

- `go build ./...`, `go test ./... -count=1`, and the race suite pass.
- `GET /healthz` answers `{"contract":"v1","tools":168}` from the built
  binary and from the built image; unknown paths answer 404.
- `python3 scripts/check-parity.py` still passes: the contract surface is
  unchanged by this increment.

## NOT_CHECKED

Release cut with provenance and vulnerability scan, QA deployment from the
destination artifact, parity re-proof against the live core path, rehearsed
rollback, owner production acceptance, and in-tree removal. Tool dispatch,
authentication against live core, concurrency proofs, and the arbiter's
version-pair and shared-shape-publisher decisions all stay open under #9498.
