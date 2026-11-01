# Parity

Refs `eshu-hq/eshu#8873`. Seed-time parity proof for the versioned
tool-contract surface.

## Method

Verbatim artifacts are compared byte-for-byte against the source commit with
`cmp` and `sha256sum`. The derived registry surface is compared
name-by-name and position-by-position against the live source census with an
independent re-extraction (the check script parses the source file again
instead of trusting the seed script's memory).

## Results (reseed, 2026-10-29: source `9922b49`, 169 tools; appendix #9670: 171 tools)

- `docs/source/CUTOVER_OWNERSHIP.md` is byte-identical to
  `go/cmd/mcp-server/CUTOVER_OWNERSHIP.md` at
  `9922b492d8a3a25ef87b39d38864b36ff522a6ce`:
  `cmp` exited 0 and both files hash to
  `1daf8a6d7637981a8db8d08485fad77561c4749bbd794d3c9671d79bfe7a85df`.
- `contracts/tool-registry/v1/tools.json`: source census count 171, dest
  count 171, order identical. First tool `find_code`, tail tools
  `search_php_configuration` then `list_ssm_host_inventory`. All 171 names
  unique. Census count constant matches.
- Envelope MIME `application/eshu.envelope+json`, dispatch timeout `30s`,
  response budget 262144 bytes, router limits 5/10: equal to the source
  census pins.

The prior seed (2026-10-28, source `e4c385f4`, 168 tools ending at
`list_runtime_version_declarations`) is superseded: the reseed restored the
missing `search_dockerfile_installs` tool from `eshu-hq/eshu#9303`, and the
#9670 appendix ports the two tools the source census appended after the
reseed (`search_php_configuration`, `list_ssm_host_inventory`), verified
verbatim against the in-tree census tail and routes at
`b1748fc0b1b65d8f2ee29fbd58bbf5b2251d6a84`.

Run `python3 scripts/check-parity.py` to re-verify the in-repo invariants.
Cross-repo byte-parity re-verification needs the source checkout; the
commands are recorded above.

## Edge credential admission (eshu-hq/eshu-mcp#48, #50)

The edge door check classifies credentials by shape and does not admit them.

- A dotted Bearer credential is a JWT attempt and is verified at the door.
- A dotless Bearer (Eshu personal and scoped tokens) and any non-Bearer scheme
  forward as a `fwd:` principal. The edge then asks core to admit it before it
  serves `initialize`, `tools/list`, `ping`, or the `GET /sse` session mint,
  which is what core does for every method (eshu-hq/eshu-mcp#50, the owner
  ruling on #27). Core still admits `tools/call` on dispatch.
- The admission probe is `GET /api/v0/capabilities?limit=1` with the caller's
  `Authorization` header verbatim. It is authenticated, carries no tenant data,
  and core lets every credential class it accepts call it. A 200 admits; a 401
  is relayed with core's `WWW-Authenticate` (the edge's own challenge when core
  sent none); a 403 is relayed as a 403 without a challenge; any other status,
  a timeout, or no answer fails closed with a retryable 503. Decisions are
  cached by credential hash: 30 seconds for an admit, 10 seconds for a denial,
  never for a 503, and at most 4096 entries; a full cache evicts the entry
  closest to expiry, so a repeated credential still costs one probe per TTL.
  A revoked credential can keep reaching
  the locally served methods for up to the admit TTL; `tools/call` is
  re-admitted by core every time. The shared key and an edge-verified JWT never
  probe.
- `/healthz` reports `admission` counters (`admitted`, `denied`, `unavailable`,
  `cache_hit`) so an operator can tell a core outage from a denial storm.
- Operator constraint: core treats exactly three non-empty dot-separated
  segments as a JWT; any other shape falls through to its token registries.
  The edge treats any dotted Bearer as a JWT attempt. A token in
  `ESHU_SCOPED_TOKENS_FILE` that contains a dot is therefore refused at the
  edge, although core would accept it unless it has the three-segment JWT
  shape. Use dotless tokens.
- The edge denial log line carries the same message and `iss` and `outcome`
  keys as core's `deny()` line, plus `method` and `path`. The E2E scraper
  matches on the message and `outcome`.

## NOT_CHECKED

Live MCP calls against deployed artifacts, QA EKS proof, concurrent
retry/duplicate/ordering/cancellation proof, and production acceptance. The
in-tree implementation stays until that acceptance.
