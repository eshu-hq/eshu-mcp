#!/usr/bin/env python3
"""In-repo parity invariants for the v1 tool-contract surface.

Verifies what can be checked without the source checkout:
tool count, uniqueness, envelope/dispatch/router pins, the
verbatim ownership-map copy still hashing to the provenance pin, and
the seeded router_catalog section (every ordered tool has a row with
a closed-set cost, at least one canonical mark exists overall, and
intents are non-empty lists of at most 8 phrases).

The router_catalog validators also run as negative controls against
in-memory mutated copies (one deleted row, one corrupted cost); a
control that fails to trip is a broken check, so the script exits
nonzero. Exit nonzero on any mismatch.
"""
import copy
import hashlib
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FAILURES = []

SOURCE_COMMIT = "93f481e3d261cb5b126d765834b4d688b67693b3"
OWNERSHIP_SHA256 = "ed6909ca4169804a120a14674842c11c111176a32d4ae92a832d9b88a874cf72"
COST_SET = {"low", "moderate", "high"}


def check(cond, msg, sink=None):
    if sink is None:
        print(("ok  " if cond else "FAIL") + " " + msg)
        sink = FAILURES
    if not cond:
        sink.append(msg)


def check_catalog(doc, order, label, sink=None):
    """Router-catalog invariants; label names the copy under test."""
    catalog = doc.get("router_catalog")
    check(isinstance(catalog, dict), f"{label}: router_catalog section present", sink)
    if not isinstance(catalog, dict):
        return
    missing = [name for name in order if name not in catalog]
    check(not missing, f"{label}: every ordered tool has a catalog row", sink)
    if missing and sink is None:
        print(f"    missing: {missing[:5]}")
    bad_cost = sorted(
        name for name, row in catalog.items()
        if not isinstance(row, dict) or row.get("cost") not in COST_SET
    )
    check(not bad_cost, f"{label}: every catalog row has a closed-set cost", sink)
    if bad_cost and sink is None:
        print(f"    bad cost: {bad_cost[:5]}")
    canonical = sorted(
        name for name, row in catalog.items()
        if isinstance(row, dict) and row.get("canonical") is True
    )
    check(len(canonical) >= 1, f"{label}: at least one canonical mark exists", sink)
    bad_intents = sorted(
        name for name, row in catalog.items()
        if not isinstance(row, dict)
        or not isinstance(row.get("intents"), list)
        or not row["intents"]
        or len(row["intents"]) > 8
        or not all(isinstance(p, str) and p for p in row["intents"])
    )
    check(
        not bad_intents,
        f"{label}: every catalog row has a non-empty intent list of at most 8",
        sink,
    )
    if bad_intents and sink is None:
        print(f"    bad intents: {bad_intents[:5]}")


def main():
    tools_path = ROOT / "contracts" / "tool-registry" / "v1" / "tools.json"
    doc = json.loads(tools_path.read_text())
    order = doc["registry"]["tool_order"]
    check(doc["contract_version"] == "v1", "contract version is v1")
    check(doc["registry"]["tool_count"] == 173, "registry count is 173")
    check(len(order) == 173, "tool_order has 173 entries")
    check(len(set(order)) == len(order), "tool names are unique")
    check(order[0] == "find_code", "first tool is find_code")
    check(
        order[-4:] == ["list_runtime_version_declarations", "search_dockerfile_installs", "search_php_configuration", "list_ssm_host_inventory"],
        "tail tools are the pre-appendix census tail (the #10477/#10535 pair moved to positions 117-118 in source registration order)",
    )
    check(
        order[117:119] == ["list_service_api_endpoints", "list_service_downstream_consumers"],
        "list_service_api_endpoints and list_service_downstream_consumers sit at positions 117-118 per the source census",
    )
    check(
        doc["source"]["census_count_constant"] == 173,
        "census count constant is 173",
    )
    check(
        doc["source"]["commit"] == SOURCE_COMMIT,
        "source commit is origin/main 93f481e3d2 (fix-07 part B merge)",
    )
    check(
        doc["source"]["ownership_map"] == "go/cmd/api/CUTOVER_OWNERSHIP.md",
        "ownership map path is go/cmd/api/CUTOVER_OWNERSHIP.md (in-tree server moved after #9679)",
    )
    check(
        doc["envelope"]["mime_type"] == "application/eshu.envelope+json",
        "envelope MIME is application/eshu.envelope+json",
    )
    check(
        doc["dispatch_bounds"]["default_timeout"] == "30s",
        "dispatch timeout is 30s",
    )
    check(
        doc["dispatch_bounds"]["default_response_byte_budget"] == 262144,
        "response budget is 262144",
    )
    check(
        doc["router_surface"]["default_limit"] == 5
        and doc["router_surface"]["max_limit"] == 10,
        "router limits are 5/10",
    )

    src_copy = ROOT / "docs" / "source" / "CUTOVER_OWNERSHIP.md"
    digest = hashlib.sha256(src_copy.read_bytes()).hexdigest()
    check(
        digest == doc["source"]["ownership_map_sha256"],
        "verbatim ownership map matches provenance hash",
    )
    check(
        digest == OWNERSHIP_SHA256,
        "verbatim ownership map matches reseed-time hash (93f481e3d2)",
    )

    defs_path = ROOT / "contracts" / "tool-registry" / "v1" / "tool_definitions.json"
    defs = json.loads(defs_path.read_text())
    check(defs["contract_version"] == "v1", "definitions contract is v1")
    check_catalog(defs, order, "seeded contract")

    # Seeded-violation negative controls: the same validators must FAIL on
    # a temp copy with one row deleted and on one with a corrupted cost.
    # A control that stays green means the validator is blind, so the
    # script fails closed. Without a seeded section there is nothing to
    # mutate; the missing-section failure above already covers that.
    if not isinstance(defs.get("router_catalog"), dict):
        print("seeded-violation controls skipped: no router_catalog section")
        print(f"{len(FAILURES)} failure(s)", file=sys.stderr)
        return 1
    doomed = copy.deepcopy(defs)
    doomed["router_catalog"].pop(order[0])
    doomed_failures = []
    check_catalog(doomed, order, "deleted-row control", doomed_failures)
    spoiled = copy.deepcopy(defs)
    spoiled["router_catalog"][order[1]]["cost"] = "priceless"
    spoiled_failures = []
    check_catalog(spoiled, order, "corrupt-cost control", spoiled_failures)
    check(
        bool(doomed_failures) and bool(spoiled_failures),
        "seeded-violation controls trip (deleted row, corrupt cost)",
    )
    if not doomed_failures:
        print("    control broken: deleted row stayed green")
    if not spoiled_failures:
        print("    control broken: corrupt cost stayed green")

    if FAILURES:
        print(f"{len(FAILURES)} failure(s)", file=sys.stderr)
        return 1
    print("all parity checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
