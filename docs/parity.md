# Parity

Refs `eshu-hq/eshu#8873`. Seed-time parity proof for the versioned
tool-contract surface.

## Method

Verbatim artifacts are compared byte-for-byte against the source commit with
`cmp` and `sha256sum`. The derived registry surface is compared
name-by-name and position-by-position against the live source census with an
independent re-extraction (the check script parses the source file again
instead of trusting the seed script's memory).

## Results (seed time, 2026-10-28)

- `docs/source/CUTOVER_OWNERSHIP.md` is byte-identical to
  `go/cmd/mcp-server/CUTOVER_OWNERSHIP.md` at
  `e4c385f493494ed9d6c7f192b04ce0255361f621`:
  `cmp` exited 0 and both files hash to
  `71b005ee7305ca0768f38694ac6de237901d963df75d4077dd57e189bfadc89f`.
- `contracts/tool-registry/v1/tools.json`: source census count 168, dest
  count 168, order identical. First tool `find_code`, last tool
  `list_runtime_version_declarations`. All 168 names unique. Census count
  constant matches.
- Envelope MIME `application/eshu.envelope+json`, dispatch timeout `30s`,
  response budget 262144 bytes, router limits 5/10: equal to the source
  census pins.

Run `python3 scripts/check-parity.py` to re-verify the in-repo invariants.
Cross-repo byte-parity re-verification needs the source checkout; the
commands are recorded above.

## NOT_CHECKED

Live MCP calls against deployed artifacts, QA EKS proof, concurrent
retry/duplicate/ordering/cancellation proof, and production acceptance. The
in-tree implementation stays until that acceptance.
