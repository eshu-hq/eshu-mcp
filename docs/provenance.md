# Provenance

Refs `eshu-hq/eshu#8873`, part of `eshu-hq/eshu#6707`.

## Source

- Repository: `github.com/eshu-hq/eshu`.
- Commit: `9922b492d8a3a25ef87b39d38864b36ff522a6ce`
  (`origin/main` at reseed time, 2026-10-29). The prior seed used
  `e4c385f493494ed9d6c7f192b04ce0255361f621` (168 tools).
- Ownership map: `go/cmd/mcp-server/CUTOVER_OWNERSHIP.md`, recorded by
  merged `eshu-hq/eshu#9437` and tightened by `eshu-hq/eshu#9445`.
- Destination plan (preparation only, nothing pushed by it):
  `docs/internal/evidence/8873-eshu-mcp-destination-plan.md` in the source
  tree.

Note: the assigning brief said 170 tools. The source census pins 169
(`censusToolCount = 169` in `go/internal/mcp/tool_registry_census_test.go`
at the reseed commit). This seed follows the source: 169, ending at
`search_dockerfile_installs` (restored from `eshu-hq/eshu#9303`, which the
prior 168-tool seed predated).

## File mapping

| This file | Source | Class |
| --- | --- | --- |
| `docs/source/CUTOVER_OWNERSHIP.md` | `go/cmd/mcp-server/CUTOVER_OWNERSHIP.md` at the source commit | Verbatim copy; byte-parity proven in `docs/parity.md` |
| `contracts/tool-registry/v1/tools.json` | `censusToolNames` in `go/internal/mcp/tool_registry_census_test.go`, plus router/dispatch/envelope pins from the same file | Derived surface; order and count proven equal in `docs/parity.md` |
| `contracts/shared-shapes/v1/SHARED_SHAPES.md` | `go/internal/mcp/result/shape/shape.go`, `go/internal/mcp/contract/tool/contract.go`, `go/internal/mcp/contract/route/contract.go`, `go/internal/query/envelope_aliases.go` | References with hashes; no Go code moved |

## Hashes (source commit)

- `CUTOVER_OWNERSHIP.md`:
  `1daf8a6d7637981a8db8d08485fad77561c4749bbd794d3c9671d79bfe7a85df`
  (prior seed: `71b005ee7305ca0768f38694ac6de237901d963df75d4077dd57e189bfadc89f`
  at `e4c385f4`)
- `result/shape/shape.go`:
  `e2ca3bf593694d87aaa3207b5236f199f4ba42b2537ab3a199658d83140fd82b`
- `contract/tool/contract.go`:
  `f77e1f42a63662b033248487f8f58b1e5f4bcd0c24bbf130041be5497e3be64a`
- `contract/route/contract.go`:
  `a146745325224caebce0409606bf49d09b370169780608c47fa917cf14d6e416`
- `query/envelope_aliases.go`:
  `f955532fbe87540d0e6650266728781ce2bb7587ad2da8ff908458046990ec84`

SHA-256 was chosen because it is the preimage-resistant checksum the
destination plan already implies for artifact provenance. Recompute with
`sha256sum` against the source commit to re-verify.
