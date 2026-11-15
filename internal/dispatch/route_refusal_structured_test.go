package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// Missing-selector refusals (eshu-hq/eshu-mcp#73): the schema gate cannot
// stop every selectorless call — open schemas let them through on purpose,
// and the direct tools/call path never validates — so the route refusal
// itself must render the structured mcp_router_invalid_arguments error
// naming the selector fields that would satisfy it, on the direct
// tools/call path as well as the routed call_tool path, which share
// ToolErrorResultForCall. This table is the structured-rendering copy of
// TestResolveRouteRefusesMissingSelector: the refusal texts stay
// byte-identical, the envelope gains the code and fields.
//
// Slice 1 typed the multi-selector refusals whose schemas deliberately stay
// open. This slice adds the single-selector refusals: the explicit route
// checks and the shared empty-path-segment guard, which names the
// schema-required arguments the call left empty.

// selectorRefusalCase pins one refusal: the historic text and the selector
// fields, in route precedence order, that satisfy it.
type selectorRefusalCase struct {
	args    map[string]any
	message string
	fields  []string
}

func selectorRefusalCases() map[string]selectorRefusalCase {
	return map[string]selectorRefusalCase{
		"get_service_story": {
			args:    map[string]any{"service": "example-service"},
			message: "get_service_story requires service_id, workload_id, or service_name",
			fields:  []string{"service_id", "workload_id", "service_name"},
		},
		"get_service_intelligence_report": {
			args:    map[string]any{},
			message: "get_service_intelligence_report requires service_id, workload_id, or service_name",
			fields:  []string{"service_id", "workload_id", "service_name"},
		},
		"list_service_api_endpoints": {
			args:    map[string]any{},
			message: "list_service_api_endpoints requires service_id, workload_id, or service_name",
			fields:  []string{"service_id", "workload_id", "service_name"},
		},
		"list_service_downstream_consumers": {
			args:    map[string]any{},
			message: "list_service_downstream_consumers requires service_id, workload_id, or service_name",
			fields:  []string{"service_id", "workload_id", "service_name"},
		},
		"investigate_service": {
			args:    map[string]any{},
			message: "investigate_service requires service_id or service_name",
			fields:  []string{"service_id", "service_name"},
		},
		"get_repo_summary": {
			args:    map[string]any{},
			message: "repo_id or repo_name is required",
			fields:  []string{"repo_id", "repo_name"},
		},
		"get_service_context": {
			args:    map[string]any{"workload_id": "   "},
			message: "get_service_context requires workload_id",
			fields:  []string{"workload_id"},
		},
		"get_collector_extraction_readiness": {
			args:    map[string]any{},
			message: "family is required",
			fields:  []string{"family"},
		},
		"get_component_extension_diagnostics": {
			args:    map[string]any{},
			message: "component_id is required",
			fields:  []string{"component_id"},
		},
		"get_fact_schema_version": {
			args:    map[string]any{},
			message: "fact_kind is required",
			fields:  []string{"fact_kind"},
		},
		"list_dead_letter_work_items": {
			args:    map[string]any{},
			message: "limit is required",
			fields:  []string{"limit"},
		},
		"list_reducer_input_invalid_facts": {
			args:    map[string]any{},
			message: "scope_id is required",
			fields:  []string{"scope_id"},
		},
		"list_changed_since_poisoned_links": {
			args:    map[string]any{},
			message: "limit is required",
			fields:  []string{"limit"},
		},
		"list_repository_files": {
			args:    map[string]any{},
			message: "repo_id is required",
			fields:  []string{"repo_id"},
		},
		"get_repo_context": {
			args:    map[string]any{},
			message: "repo_id is required",
			fields:  []string{"repo_id"},
		},
		"get_repo_story": {
			args:    map[string]any{},
			message: "repo_id is required",
			fields:  []string{"repo_id"},
		},
		"get_repository_coverage": {
			args:    map[string]any{},
			message: "repo_id is required",
			fields:  []string{"repo_id"},
		},
		"get_repository_freshness": {
			args:    map[string]any{},
			message: "repo_id is required",
			fields:  []string{"repo_id"},
		},
		"get_entity_context": {
			args:    map[string]any{},
			message: "entity_id is required",
			fields:  []string{"entity_id"},
		},
		"get_documentation_evidence_packet": {
			args:    map[string]any{},
			message: "finding_id is required",
			fields:  []string{"finding_id"},
		},
		"check_documentation_evidence_packet_freshness": {
			args:    map[string]any{},
			message: "packet_id is required",
			fields:  []string{"packet_id"},
		},
		"get_workload_context": {
			args:    map[string]any{},
			message: "workload_id is required",
			fields:  []string{"workload_id"},
		},
		"get_workload_story": {
			args:    map[string]any{},
			message: "workload_id is required",
			fields:  []string{"workload_id"},
		},
		"get_incident_context": {
			args:    map[string]any{},
			message: "provider_incident_id is required",
			fields:  []string{"provider_incident_id"},
		},
	}
}

// TestRouteRefusalCarriesSelectorFields proves each refusal below the schema
// gate carries the tool and the selector fields that would satisfy it.
func TestRouteRefusalCarriesSelectorFields(t *testing.T) {
	t.Parallel()

	for tool, tc := range selectorRefusalCases() {
		_, err := runDispatch(context.Background(), nil, tool, tc.args, "", nil, DefaultOptions())
		if err == nil {
			t.Errorf("runDispatch(%s) error = nil, want the %q refusal", tool, tc.message)
			continue
		}
		var refused *routecontract.MissingSelectorError
		if !errors.As(err, &refused) {
			t.Errorf("runDispatch(%s) error type %T, want *routecontract.MissingSelectorError", tool, err)
			continue
		}
		if refused.Tool != tool {
			t.Errorf("runDispatch(%s) tool = %q, want %s", tool, refused.Tool, tool)
		}
		if refused.Error() != tc.message {
			t.Errorf("runDispatch(%s) error = %q, want %q", tool, refused.Error(), tc.message)
		}
		if len(refused.Fields) != len(tc.fields) {
			t.Errorf("runDispatch(%s) fields = %v, want %v", tool, refused.Fields, tc.fields)
			continue
		}
		for i := range tc.fields {
			if refused.Fields[i] != tc.fields[i] {
				t.Errorf("runDispatch(%s) fields = %v, want %v", tool, refused.Fields, tc.fields)
				break
			}
		}
	}
}

// TestRouteRefusalRendersInvalidArguments proves the shared renderer answers
// a selector refusal with the structured mcp_router_invalid_arguments error
// in both result shapes, keeping the historic text as the message.
func TestRouteRefusalRendersInvalidArguments(t *testing.T) {
	t.Parallel()

	for tool, tc := range selectorRefusalCases() {
		_, err := runDispatch(context.Background(), nil, tool, tc.args, "", nil, DefaultOptions())
		if err == nil {
			t.Errorf("runDispatch(%s) error = nil, want the refusal", tool)
			continue
		}
		for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
			res := ToolErrorResultForCall(mode, tool, err, nil, DefaultOptions().ResponseByteBudget)
			if !res.IsError {
				t.Errorf("ToolErrorResultForCall(%s, %s) isError = false, want true", tool, mode)
				continue
			}
			body, marshalErr := json.Marshal(res.StructuredContent)
			if marshalErr != nil {
				t.Errorf("ToolErrorResultForCall(%s, %s) structured content does not marshal: %v", tool, mode, marshalErr)
				continue
			}
			var envelope struct {
				Error struct {
					Code       string `json:"code"`
					Message    string `json:"message"`
					Capability string `json:"capability"`
					Details    struct {
						Tool   string   `json:"tool"`
						Fields []string `json:"fields"`
					} `json:"details"`
				} `json:"error"`
			}
			if unmarshalErr := json.Unmarshal(body, &envelope); unmarshalErr != nil {
				t.Errorf("ToolErrorResultForCall(%s, %s) structured content does not decode: %v", tool, mode, unmarshalErr)
				continue
			}
			if envelope.Error.Code != "mcp_router_invalid_arguments" {
				t.Errorf("ToolErrorResultForCall(%s, %s) code = %q, want mcp_router_invalid_arguments", tool, mode, envelope.Error.Code)
			}
			if envelope.Error.Message != tc.message {
				t.Errorf("ToolErrorResultForCall(%s, %s) message = %q, want %q", tool, mode, envelope.Error.Message, tc.message)
			}
			if envelope.Error.Details.Tool != tool {
				t.Errorf("ToolErrorResultForCall(%s, %s) details.tool = %q, want %s", tool, mode, envelope.Error.Details.Tool, tool)
			}
			if len(envelope.Error.Details.Fields) != len(tc.fields) {
				t.Errorf("ToolErrorResultForCall(%s, %s) details.fields = %v, want %v", tool, mode, envelope.Error.Details.Fields, tc.fields)
			}
		}
	}
}
