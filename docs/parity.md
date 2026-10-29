# Parity

Refs `eshu-hq/eshu#8873`. Seed-time parity proof for the versioned
tool-contract surface.

## Method

Verbatim artifacts are compared byte-for-byte against the source commit with
`cmp` and `sha256sum`. The derived registry surface is compared
name-by-name and position-by-position against the live source census with an
independent re-extraction (the check script parses the source file again
instead of trusting the seed script's memory).

## Results (reseed, 2026-10-29: source `9922b49`, 169 tools)

- `docs/source/CUTOVER_OWNERSHIP.md` is byte-identical to
  `go/cmd/mcp-server/CUTOVER_OWNERSHIP.md` at
  `9922b492d8a3a25ef87b39d38864b36ff522a6ce`:
  `cmp` exited 0 and both files hash to
  `1daf8a6d7637981a8db8d08485fad77561c4749bbd794d3c9671d79bfe7a85df`.
- `contracts/tool-registry/v1/tools.json`: source census count 169, dest
  count 169, order identical. First tool `find_code`, last tool
  `search_dockerfile_installs`. All 169 names unique. Census count
  constant matches.
- Envelope MIME `application/eshu.envelope+json`, dispatch timeout `30s`,
  response budget 262144 bytes, router limits 5/10: equal to the source
  census pins.

The prior seed (2026-10-28, source `e4c385f4`, 168 tools ending at
`list_runtime_version_declarations`) is superseded: the reseed restored the
missing `search_dockerfile_installs` tool from `eshu-hq/eshu#9303`.

Run `python3 scripts/check-parity.py` to re-verify the in-repo invariants.
Cross-repo byte-parity re-verification needs the source checkout; the
commands are recorded above.

## NOT_CHECKED

Live MCP calls against deployed artifacts, QA EKS proof, concurrent
retry/duplicate/ordering/cancellation proof, and production acceptance. The
in-tree implementation stays until that acceptance.
