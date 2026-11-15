package routecontract

import (
	"errors"
	"testing"
)

// MissingSelectorError refusal shape (eshu-hq/eshu-mcp#73, slice 1): the
// route a tool needs cannot be built because the caller named none of the
// selector arguments that would satisfy it. Sites keep their historic
// refusal text in Message; Fields names the selector arguments, in the
// precedence order the route checks them, so the renderer can tell the
// caller what to fix.
func TestMissingSelectorErrorKeepsMessage(t *testing.T) {
	t.Parallel()

	refused := &MissingSelectorError{
		Tool:    "get_service_story",
		Fields:  []string{"service_id", "workload_id", "service_name"},
		Message: "get_service_story requires service_id, workload_id, or service_name",
	}
	if got := refused.Error(); got != "get_service_story requires service_id, workload_id, or service_name" {
		t.Fatalf("Error() = %q, want the historic refusal text", got)
	}
	var asErr *MissingSelectorError
	if !errors.As(refused, &asErr) {
		t.Fatal("errors.As did not match *MissingSelectorError")
	}
	if asErr.Tool != "get_service_story" {
		t.Errorf("Tool = %q, want get_service_story", asErr.Tool)
	}
	if len(asErr.Fields) != 3 || asErr.Fields[0] != "service_id" {
		t.Errorf("Fields = %v, want the selector precedence order", asErr.Fields)
	}
}
