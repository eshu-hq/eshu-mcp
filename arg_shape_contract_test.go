package registry

import (
	"strings"
	"testing"
)

// Deliberate selector contracts (eshu-hq/eshu-mcp#61): where the handler
// requires one of several selectors (anyOf) or a conditional target, the
// input schema cannot express the requirement (the OpenAI MCP contract test
// forbids a top-level anyOf/oneOf), so the tool description carries it.
// This table pins the contract sentence for every such tool: amended text
// for the #61 drive, pre-existing text for the tools that already stated
// it. If a description is reworded, the selector requirement must survive.
func TestDeliberateSelectorContractText(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		// Amended by the #61 drive.
		"find_blast_radius":                               "target and target_type are both required",
		"find_change_surface":                             "target is required",
		"search_php_configuration":                        "directive or mode",
		"investigate_contract_impact":                     "provider_repo_id, consumer_repo_id, repo_id, topic, or service_name",
		"investigate_change_surface":                      "changed_paths with repo_id",
		"analyze_pre_change_impact":                       "changes with repo_id, target, service_name, or topic",
		"plan_developer_change":                           "changes with repo_id, target, service_name, or topic",
		"find_unmanaged_resources":                        "scope_id or account_id",
		"list_cloud_runtime_drift_findings":               "scope_id, account_id, project_id, or subscription_id",
		"export_cloud_runtime_drift_packet":               "scope_id, account_id, project_id, or subscription_id",
		"find_function_call_chain":                        "start or start_entity_id, and end or end_entity_id",
		"find_dead_iac":                                   "repo_id or repo_ids",
		"list_kubernetes_correlations":                    "workload_object_id, namespace, image_ref, or source_digest",
		"list_supply_chain_impact_findings":               "min_priority_score above zero",
		"explain_supply_chain_impact":                     "advisory_id/cve_id plus one of package_id",
		"list_container_image_identities":                 "source_repository_id, repository_id, or outcome",
		"list_observability_coverage_correlations":        "observability_object_ref, target_uid, or target_service_ref",
		"list_package_registry_packages":                  "package_id or ecosystem",
		"list_package_registry_correlations":              "package_id or repository_id",
		"list_package_registry_dependencies":              "package_id or version_id",
		"list_ci_cd_run_correlations":                     "provider_run_id, artifact_digest, image_ref, or environment",
		"list_service_catalog_correlations":               "entity_ref, repository_id, service_id, workload_id, or owner_ref",
		"list_advisory_evidence":                          "advisory_id, package_id, repository_id, service_id, or workload_id",
		"get_sbom_attestation_attachment_inventory":       "document_digest, repository_id, workload_id, or service_id",
		"list_sbom_attestation_attachments":               "document_digest, repository_id, workload_id, or service_id",
		"list_security_alert_reconciliations":             "provider_state and reconciliation_status are filters only",
		"list_secrets_iam_identity_trust_chains":          "service_account_join_key, or iam_role_fingerprint",
		"list_secrets_iam_privilege_posture_observations": "scope_id or observation_id",
		"list_secrets_iam_secret_access_paths":            "path_id, chain_id, or vault_mount_join_key",
		"list_secrets_iam_posture_gaps":                   "gap_id, or service_account_join_key",
		"list_work_item_evidence":                         "provider_work_item_id, external_url, url_fingerprint, or observed_after",
		"analyze_code_relationships":                      "dead_code scans without one",
		"list_semantic_code_hints":                        "provider_profile_id, source, subject, status, freshness, or fact_id",
		"list_semantic_documentation_observations":        "provider_profile_id, source, subject, status, freshness, or fact_id",
		// Pre-existing deliberate text, pinned against rewording.
		"get_repo_summary":                 "Provide exactly one of repo_id",
		"get_service_story":                "one of service_id, workload_id, or service_name",
		"get_service_intelligence_report":  "one of service_id, workload_id, or service_name",
		"investigate_service":              "service_id or service_name",
		"search_registry_bundles":          "non-empty query or ecosystem scope",
		"get_code_relationship_story":      "Provide target or entity_id",
		"investigate_deployment_config":    "Provide service_name or workload_id",
		"investigate_resource":             "Provide query or resource_id",
		"count_secrets_iam_posture":        "Requires scope_id",
		"list_container_image_tag_history": "repository_id and tag are both required",
		"trace_exposure_path":              "or source_entity_id",
		"get_changed_since":                "mutually exclusive scope selector is required",
		"investigate_import_dependencies":  "at least one scope filter",
		"compose_replatforming_plan":       "scope_kind plus scope_id or account_id",
		"get_replatforming_rollups":        "scope_id or account_id",
		"propose_terraform_import_plan":    "scope_id or account_id",
		"get_iac_management_status":        "scope_id or account_id plus arn or resource_id",
		"explain_iac_management_status":    "scope_id or account_id plus arn or resource_id",
		"list_aws_runtime_drift_findings":  "scope_id or account_id",
		"find_unmanaged_resource_owners":   "scope_id or account_id",
	}
	defs := map[string]ToolDefinition{}
	for _, tool := range Definitions() {
		defs[tool.Name] = tool
	}
	for name, want := range cases {
		definition, ok := defs[name]
		if !ok {
			t.Errorf("Definition(%s) missing", name)
			continue
		}
		if !containsFold(definition.Description, want) {
			t.Errorf("Description(%s) lost its selector contract %q", name, want)
		}
	}
}

// containsFold reports whether text contains want, ignoring ASCII case, so
// a capitalization-only rewording does not fail the contract pin.
func containsFold(text, want string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(want))
}
