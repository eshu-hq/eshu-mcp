#!/usr/bin/env python3
"""Render eshu-mcp versioned contracts from source seed facts.

Reads seed-facts.json (emitted by emit_seed_test.go inside the source
checkout at the pinned commit) and renders, deterministically:

- contracts/tool-registry/v1/tools.json (re-seeded registry section)
- contracts/tool-registry/v1/tool_definitions.json (new: served definitions)
- internal/dispatch/testdata/sweep_resolutions.json (resolution expectations)

JSON keys sort recursively so re-runs are byte-stable. Lists that are
order-contractual (tool_order, registry_tools, router_tools) keep source
registration order and are never sorted.

Usage:
    python3 scripts/seed/render.py \
        --facts /tmp/eshu-mcp-seed/seed-facts.json \
        --commit 9922b492d8a3a25ef87b39d38864b36ff522a6ce \
        --ownership-sha256 <sha256 of source CUTOVER_OWNERSHIP.md> \
        --root .
"""

import argparse
import json
import sys


def sort_deep(value):
    if isinstance(value, dict):
        return {key: sort_deep(value[key]) for key in sorted(value)}
    if isinstance(value, list):
        return [sort_deep(item) for item in value]
    return value


def dump(path, value):
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(value, handle, indent=2)
        handle.write("\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--facts", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--ownership-sha256", required=True)
    parser.add_argument("--root", required=True)
    args = parser.parse_args()

    with open(args.facts, encoding="utf-8") as handle:
        facts = json.load(handle)

    registry = facts["registry"]
    names = [tool["name"] for tool in registry]
    if len(names) != len(set(names)):
        sys.exit("duplicate tool names in seed facts")
    if names[0] != "find_code":
        sys.exit(f"first tool is {names[0]!r}, want 'find_code'")

    tools_path = (
        f"{args.root}/contracts/tool-registry/v1/tools.json"
    )
    with open(tools_path, encoding="utf-8") as handle:
        tools_doc = json.load(handle)

    tools_doc["source"] = {
        "repo": "github.com/eshu-hq/eshu",
        "commit": args.commit,
        "census_test": "go/internal/mcp/tool_registry_census_test.go",
        "census_count_constant": len(names),
        "ownership_map": "go/cmd/mcp-server/CUTOVER_OWNERSHIP.md",
        "ownership_map_sha256": args.ownership_sha256,
        "note": "Tool order is part of the contract and must stay byte-stable with the source census.",
    }
    tools_doc["registry"] = {"tool_count": len(names), "tool_order": names}
    # tools.json keeps its existing key order; only the reseeded sections
    # change. sort_deep would reorder the top-level contract keys.
    dump(tools_path, tools_doc)

    consts = facts["source_constants"]
    definitions = {
        "contract_version": "v1",
        "source": {
            "repo": "github.com/eshu-hq/eshu",
            "commit": args.commit,
            "census_test": "go/internal/mcp/tool_registry_census_test.go",
            "schema_census_test": "go/internal/mcp/tool_registry_schema_census_test.go",
            "note": "Served definitions are verbatim source facts; tools/list serves registry_tools in order.",
            "bounds": {
                "content_search_max_offset": consts["content_search_max_offset"],
                "envelope_mime": consts["envelope_mime"],
                "protocol_versions": consts["protocol_versions"],
            },
        },
        "registry_tools": [
            {
                "name": tool["name"],
                "description": tool["description"],
                "inputSchema": sort_deep(tool["input_schema"]),
            }
            for tool in registry
        ],
        "router_tools": [
            {
                "name": tool["name"],
                "description": tool["description"],
                "inputSchema": sort_deep(tool["input_schema"]),
            }
            for tool in facts["router_pair"]
        ],
        "examples": {name: facts["examples"][name] for name in names},
        "core_router_names": facts["core_router_names"],
        "production_hidden": sorted(facts["production_hidden"]),
    }
    dump(
        f"{args.root}/contracts/tool-registry/v1/tool_definitions.json",
        sort_deep(definitions),
    )

    resolutions = {
        name: [
            {key: row[key] for key in sorted(row)}
            for row in rows
        ]
        for name, rows in sorted(facts["resolutions"].items())
    }
    dump(
        f"{args.root}/internal/dispatch/testdata/sweep_resolutions.json",
        resolutions,
    )

    print(f"registry tools: {len(names)}")
    print(f"router tools: {len(facts['router_pair'])}")
    print(f"resolution tools: {len(resolutions)}")
    print(f"core router names: {len(facts['core_router_names'])}")
    print(f"production hidden: {sorted(facts['production_hidden'])}")


if __name__ == "__main__":
    main()
