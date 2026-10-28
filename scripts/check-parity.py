#!/usr/bin/env python3
"""In-repo parity invariants for the v1 tool-contract surface.

Verifies what can be checked without the source checkout:
tool count, uniqueness, envelope/dispatch/router pins, and that the
verbatim ownership-map copy still hashes to the provenance pin.
Exit nonzero on any mismatch.
"""
import hashlib
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FAILURES = []


def check(cond, msg):
    print(("ok  " if cond else "FAIL") + " " + msg)
    if not cond:
        FAILURES.append(msg)


def main():
    tools_path = ROOT / "contracts" / "tool-registry" / "v1" / "tools.json"
    doc = json.loads(tools_path.read_text())
    order = doc["registry"]["tool_order"]
    check(doc["contract_version"] == "v1", "contract version is v1")
    check(doc["registry"]["tool_count"] == 168, "registry count is 168")
    check(len(order) == 168, "tool_order has 168 entries")
    check(len(set(order)) == len(order), "tool names are unique")
    check(order[0] == "find_code", "first tool is find_code")
    check(
        order[-1] == "list_runtime_version_declarations",
        "last tool is list_runtime_version_declarations",
    )
    check(
        doc["source"]["census_count_constant"] == 168,
        "census count constant is 168",
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
        digest
        == "71b005ee7305ca0768f38694ac6de237901d963df75d4077dd57e189bfadc89f",
        "verbatim ownership map matches seed-time hash",
    )

    if FAILURES:
        print(f"{len(FAILURES)} failure(s)", file=sys.stderr)
        return 1
    print("all parity checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
