# Per-tool route-read census

Companion to eshu-hq/eshu-mcp#86 (closed-world argument validation) and the
eshu-hq/eshu-mcp#73 audit that first mapped every route read. This page
records the route-read census and the coverage claim the closed-world change
depends on.

## What "route-read key" means

A tool's route builder reads its arguments through the dispatch helpers
(`str`, `intOr`, `boolOr`, `stringSlice`, `objectSlice`, `intString`, and the
`routecontract.Arguments` methods `String`, `IntOr`, `BoolOr`,
`OptionalFloat`, `StringSlice`). A key is "route-read" when one of these
helpers is called with that key for that tool, or when the route forwards the
whole argument map verbatim (a pass-through).

## Coverage claim

> Every schema that declares `additionalProperties: false` covers the keys its
> route reads.

`#86` honors `additionalProperties: false` in the validator and declares it on
the router pair, so this claim is the compatibility proof that closing those
schemas cannot refuse a key the route needs. The 173 target tools do not yet
declare `additionalProperties: false` (they stay on the open-world tolerance)
until the declare-or-remove follow-ups below land, so their route reads are
recorded here for that forward path.

## Closed schemas (this change)

| Tool | Route-read keys | Declared |
| --- | --- | --- |
| `find_tool` | `intent`, `anchors`, `limit` | yes |
| `call_tool` | `name`, `arguments` | yes |

Pinned by `TestRouterPairCoversRouteReadKeys` in `closed_world_schema_test.go`.

## Route-read audit (173/173, from eshu-hq/eshu-mcp#73)

Every one of the 173 registered tools was read against its route. Five
categories read a key the served schema did not declare; each was split into
a declare-or-remove follow-up. Status at this change's base:

| Category | Tools | Route-read keys | Follow-up | Status |
| --- | --- | --- | --- | --- |
| `resolve_entity` deprecated alias | `resolve_entity` | `name` | eshu-hq/eshu-mcp#76 | in-flight |
| incident/run fallbacks | `get_incident_context`, `list_ci_cd_run_correlations` | `incident_id`, `run_id` | eshu-hq/eshu-mcp#77 | merged |
| dead-code decorator filter | `analyze_code_relationships` | `exclude_decorated_with` | eshu-hq/eshu-mcp#78 | in-flight |
| search selectors | `search_file_content`, `search_entity_content` | `query`, `repo_id`, `rank_cursor` | eshu-hq/eshu-mcp#79 | in-flight |
| whole-map pass-throughs | `explain_dependency_path`, `get_file_lines`, `list_runtime_version_declarations` | (every key, verbatim) | eshu-hq/eshu-mcp#80 | in-flight |

Once #76, #78, #79, and #80 land, every schema will cover its route reads and
the target tools can declare `additionalProperties: false` without breaking a
working call. Until then they stay open, so a misspelled selector such as
`service` for `service_name` still reaches the route refusal rather than
failing validation.
