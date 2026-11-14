# Provenance

Refs `eshu-hq/eshu#8873`, part of `eshu-hq/eshu#6707`.

## Source

- Repository: `github.com/eshu-hq/eshu`.
- Commit: `66e9ae4223fccb108318525ad32897edc4844de6`
  (eshu-hq/eshu-mcp#77 reseed vehicle: `93f481e3d2` plus only the two
  fallback-alias schema properties; the reviewable change is on
  `drive/77-incident-run-fallbacks`, `f158b476ac`). The prior seed used
  `93f481e3d261cb5b126d765834b4d688b67693b3` (173 tools, fix-07 part C).
- Ownership map: `go/cmd/api/CUTOVER_OWNERSHIP.md` (the in-tree server
  moved from `go/cmd/mcp-server` after `eshu-hq/eshu#9679`; the satellite
  keeps a verbatim copy).
- Destination plan (preparation only, nothing pushed by it):
  `docs/internal/evidence/8873-eshu-mcp-destination-plan.md` in the source
  tree.

Note: the source census pins 173 (`censusToolCount = 173` in
`go/internal/mcp/tool_registry_census_test.go` at the reseed commit).
The former appendix tools (`search_php_configuration`,
`list_ssm_host_inventory` from `#9670`; `list_service_api_endpoints`
from `#10477`; `list_service_downstream_consumers` from `#10535`) are
now in-census rows; the `#10477`/`#10535` pair sits at positions
117-118 in source registration order, and `tool_order` follows the
source census byte-for-byte. A set/order move is a contract-version
matter — reported on `eshu-hq/eshu#10609`, not conflated with this
reseed. The part-C reseed additionally renders the `router_catalog`
section from the part-B seed facts (`TestZZEmitSeedFacts` in
`go/internal/mcp/emit_seed_test.go`).

Appendix (#9670): the source census appended two tools after the reseed —
`search_php_configuration` (`go/internal/mcp/tools_content.go`,
route `POST /api/v0/content/php-config/search`) and
`list_ssm_host_inventory` (`go/internal/mcp/cloud/ssm_host_inventory_tools.go`,
route `GET /api/v0/cloud/ssm-host-inventory`). Both are ported here verbatim
(descriptions, input schemas, and routes verified against in-tree
`origin/main` at `b1748fc0b1b65d8f2ee29fbd58bbf5b2251d6a84`), in census order,
bringing the contract to the full 171-tool pre-extraction set.

Appendix (#10477, `eshu-hq/eshu-mcp#65`): the `drive/10477-service-paging`
branch adds `list_service_api_endpoints`
(`go/internal/mcp/service/context_tools.go`, route
`GET /api/v0/services/{service_name}/api-surface/endpoints`) plus the
`downstream_read_limit` bound on `get_service_story`/`get_service_context`,
ahead of its source census landing. It is ported here verbatim (description,
input schema, selector precedence, and opt-in `limit`/`offset` forwarding
verified against the drive branch) as entry 172, so the source commit and
census count stay pinned at `9922b49`/171 while the served registry is 172.

Appendix (#10535): the `drive/10535-consumer-list-tool` branch adds
`list_service_downstream_consumers`
(`go/internal/mcp/service/context_tools.go`, route
`GET /api/v0/services/{service_name}/downstream-consumers`) with the
keyset `limit`/`after_repository`/`after_repo_id` page parameters, ahead of
its source census landing. It is ported here verbatim (description, input
schema, selector precedence, and opt-in `limit`/cursor forwarding verified
against the drive branch) as entry 173, so the source commit and census
count stay pinned at `9922b49`/171 while the served registry is 173.

## File mapping

| This file | Source | Class |
| --- | --- | --- |
| `docs/source/CUTOVER_OWNERSHIP.md` | `go/cmd/api/CUTOVER_OWNERSHIP.md` at the source commit | Verbatim copy; byte-parity proven in `docs/parity.md` |
| `contracts/tool-registry/v1/tools.json` | `censusToolNames` in `go/internal/mcp/tool_registry_census_test.go`, plus router/dispatch/envelope pins from the same file | Derived surface; order and count proven equal in `docs/parity.md` |
| `contracts/shared-shapes/v1/SHARED_SHAPES.md` | `go/internal/mcp/result/shape/shape.go`, `go/internal/mcp/contract/tool/contract.go`, `go/internal/mcp/contract/route/contract.go`, `go/internal/query/envelope_aliases.go` | References with hashes; no Go code moved |

## Hashes (source commit)

- `CUTOVER_OWNERSHIP.md`:
  `ed6909ca4169804a120a14674842c11c111176a32d4ae92a832d9b88a874cf72`
  (prior seed: `1daf8a6d7637981a8db8d08485fad77561c4749bbd794d3c9671d79bfe7a85df`
  at `9922b49`)
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
