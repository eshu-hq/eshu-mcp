#!/usr/bin/env python3
"""Render eshu-mcp versioned contracts from source seed facts.

Reads seed-facts.json (emitted by TestZZEmitSeedFacts in
go/internal/mcp/emit_seed_test.go inside the source checkout at the pinned
commit) and renders, deterministically:

- contracts/tool-registry/v1/tools.json (re-seeded registry section)
- contracts/tool-registry/v1/tool_definitions.json (served definitions,
  plus the router_catalog section starting with the part-C reseed)
- internal/dispatch/testdata/sweep_resolutions.json (resolution expectations)

JSON keys sort recursively so re-runs are byte-stable. Lists that are
order-contractual (tool_order, registry_tools, router_tools) keep source
registration order and are never sorted. The router_catalog map is keyed by
tool name, so it goes through sort_deep like every other map; intent
phrase order inside each row is source order and is never sorted.

Run the emitter from the source checkout first:

    ESHU_SEED_OUT=<dir> go test ./internal/mcp -run TestZZEmitSeedFacts -count=1

run from the source go/ directory at the pinned commit, then render with
--facts <dir>/seed-facts.json --commit <pin> --ownership-sha256 <sha256 of
go/cmd/api/CUTOVER_OWNERSHIP.md at the pin> --root <this checkout>.

Usage:
    python3 scripts/seed/render.py \
        --facts /tmp/eshu-mcp-seed/seed-facts.json \
        --commit 93f481e3d261cb5b126d765834b4d688b67693b3 \
        --ownership-sha256 <sha256 of source CUTOVER_OWNERSHIP.md> \
        --root .
"""

import argparse
import json
import sys

# Deliberate edge contract (eshu-hq/eshu-mcp#61): unconditional core
# requirements the source schemas omit, plus the selector sentences that
# moved into tool descriptions as the edge contract. The reseed renders
# source facts verbatim, which would silently revert this fix, so the
# renderer re-applies it below. Each entry applies only when missing, so a
# future source that adopts a sentence or requirement is never doubled.
EDGE_CONTRACT_REQUIRED = {
    "compose_replatforming_plan": ["scope_kind"],
    "find_blast_radius": ["target_type"],
}

EDGE_CONTRACT_SENTENCES = {
    "analyze_code_relationships": [
        "Story query types require target or entity_id; route query types "
        "require target as the entity name (or entity_id); call_chain "
        "requires a start->end target or start_entity_id and end_entity_id; "
        "dead_code scans without one; calls missing the required selector "
        "are rejected.",
    ],
    "analyze_pre_change_impact": [
        "Provide changed_paths with repo_id, changes with repo_id, target, "
        "service_name, or topic; calls with none are rejected.",
    ],
    "explain_supply_chain_impact": [
        "Provide finding_id, or advisory_id/cve_id plus one of package_id, "
        "repository_id, subject_digest, image_ref, workload_id, or "
        "service_id; calls with none are rejected.",
    ],
    "export_cloud_runtime_drift_packet": [
        "Provide scope_id, account_id, project_id, or subscription_id; "
        "calls with none are rejected.",
    ],
    "find_blast_radius": [
        "target and target_type are both required.",
    ],
    "find_change_surface": [
        "Provide the target to analyze; target is required.",
    ],
    "find_dead_iac": [
        "Provide repo_id or repo_ids; calls with neither are rejected.",
    ],
    "find_function_call_chain": [
        "Provide start or start_entity_id, and end or end_entity_id; calls "
        "missing either pair are rejected.",
    ],
    "find_unmanaged_resources": [
        "Provide scope_id or account_id; calls with neither are rejected.",
    ],
    "get_sbom_attestation_attachment_inventory": [
        "Provide at least one of subject_digest, document_id, "
        "document_digest, repository_id, workload_id, or service_id; calls "
        "with none are rejected.",
    ],
    "investigate_change_surface": [
        "Provide at least one of target, service_name, workload_id, "
        "resource_id, module_id, topic, query, or changed_paths with "
        "repo_id; calls with none are rejected.",
    ],
    "investigate_contract_impact": [
        "Provide at least one of provider_repo_id, consumer_repo_id, "
        "repo_id, topic, or service_name; calls with none are rejected.",
    ],
    "list_advisory_evidence": [
        "Provide at least one of cve_id, advisory_id, package_id, "
        "repository_id, service_id, or workload_id; calls with none are "
        "rejected.",
    ],
    "list_ci_cd_run_correlations": [
        "Provide at least one of scope_id, repository_id, commit_sha, "
        "provider_run_id, artifact_digest, image_ref, or environment; calls "
        "with none are rejected.",
    ],
    "list_cloud_runtime_drift_findings": [
        "Provide scope_id, account_id, project_id, or subscription_id; "
        "calls with none are rejected.",
    ],
    "list_container_image_identities": [
        "Provide at least one of digest, image_ref, source_repository_id, "
        "repository_id, or outcome; calls with none are rejected.",
    ],
    "list_kubernetes_correlations": [
        "Provide at least one of scope_id, cluster_id, workload_object_id, "
        "namespace, image_ref, or source_digest; calls with none are "
        "rejected.",
    ],
    "list_observability_coverage_correlations": [
        "Provide at least one of scope_id, provider, coverage_signal, "
        "observability_object_ref, target_uid, or target_service_ref; "
        "calls with none are rejected.",
    ],
    "list_package_registry_correlations": [
        "Provide package_id or repository_id; calls with neither are "
        "rejected.",
    ],
    "list_package_registry_dependencies": [
        "Provide package_id or version_id; calls with neither are rejected.",
    ],
    "list_package_registry_packages": [
        "Provide package_id or ecosystem; ecosystem is required when name is "
        "provided; calls with neither are rejected.",
    ],
    "list_sbom_attestation_attachments": [
        "Provide at least one of subject_digest, document_id, "
        "document_digest, repository_id, workload_id, or service_id; calls "
        "with none are rejected.",
    ],
    "list_secrets_iam_identity_trust_chains": [
        "Provide at least one of scope_id, chain_id, workload_object_id, "
        "service_account_join_key, or iam_role_fingerprint; calls with none "
        "are rejected.",
    ],
    "list_secrets_iam_posture_gaps": [
        "Provide at least one of scope_id, gap_id, or "
        "service_account_join_key; calls with none are rejected.",
    ],
    "list_secrets_iam_privilege_posture_observations": [
        "Provide scope_id or observation_id; calls with neither are "
        "rejected.",
    ],
    "list_secrets_iam_secret_access_paths": [
        "Provide at least one of scope_id, path_id, chain_id, or "
        "vault_mount_join_key; calls with none are rejected.",
    ],
    "list_security_alert_reconciliations": [
        "Provide at least one of repository_id, provider, package_id, "
        "cve_id, or ghsa_id; provider_state and reconciliation_status are "
        "filters only; calls with none are rejected.",
    ],
    "list_semantic_code_hints": [
        "Provide at least one of scope_id, repo, provider_profile_id, "
        "source, subject, status, freshness, or fact_id; calls with none "
        "are rejected.",
    ],
    "list_semantic_documentation_observations": [
        "Provide at least one of scope_id, repo, provider_profile_id, "
        "source, subject, status, freshness, or fact_id; calls with none "
        "are rejected.",
    ],
    "list_service_catalog_correlations": [
        "Provide at least one of scope_id, entity_ref, repository_id, "
        "service_id, workload_id, or owner_ref; calls with none are "
        "rejected.",
    ],
    "list_supply_chain_impact_findings": [
        "Provide at least one of cve_id, advisory_id, package_id, "
        "repository_id, subject_digest, image_ref, impact_status, "
        "ecosystem, workload_id, service_id, environment, severity, "
        "priority_bucket, or min_priority_score above zero; calls with none "
        "are rejected.",
    ],
    "list_work_item_evidence": [
        "Provide at least one of scope_id, project_key, work_item_key, "
        "provider_work_item_id, external_url, url_fingerprint, or "
        "observed_after; calls with none are rejected.",
    ],
    "plan_developer_change": [
        "Provide changed_paths with repo_id, changes with repo_id, target, "
        "service_name, or topic; calls with none are rejected.",
    ],
    "search_php_configuration": [
        "Supply a non-empty directive or mode; other requests are rejected.",
    ],
}

EDGE_CONTRACT_EXAMPLES = {
    "compose_replatforming_plan": {
        "account_id": "<account_id>",
        "scope_kind": "account",
    },
    "find_blast_radius": {
        "target_type": "repository",
    },
}


def apply_edge_contract(definitions):
    """Re-apply the eshu-hq/eshu-mcp#61 edge contract onto verbatim facts.

    Returns the list of (tool, kind, detail) applications made. Every entry
    is conditional on absence, so re-running over already-diverged facts is
    a no-op and a source that later adopts a sentence is never doubled.
    """
    applied = []
    by_name = {tool["name"]: tool for tool in definitions["registry_tools"]}
    for name, fields in EDGE_CONTRACT_REQUIRED.items():
        schema = by_name[name]["inputSchema"]
        required = schema.setdefault("required", [])
        for field in fields:
            if field not in required:
                required.append(field)
                applied.append((name, "required", field))
    for name, sentences in EDGE_CONTRACT_SENTENCES.items():
        tool = by_name[name]
        for sentence in sentences:
            if sentence not in tool["description"]:
                tool["description"] = tool["description"].rstrip() + " " + sentence
                applied.append((name, "description", sentence[:60]))
    for name, keys in EDGE_CONTRACT_EXAMPLES.items():
        example = definitions["examples"].setdefault(name, {})
        for key, value in keys.items():
            if key not in example:
                example[key] = value
                applied.append((name, "example", key))
    return applied


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
        "ownership_map": "go/cmd/api/CUTOVER_OWNERSHIP.md",
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
        "router_catalog": {
            name: facts["router_catalog"][name] for name in names
        },
    }
    applied = apply_edge_contract(definitions)
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
    print(f"router catalog rows: {len(definitions['router_catalog'])}")
    print(f"edge contract applications: {len(applied)}")


if __name__ == "__main__":
    main()
