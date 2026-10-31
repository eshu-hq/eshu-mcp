package dispatch

import (
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

func partialBody(data string) []byte {
	return []byte(`{"data":` + data + `,"truth":null,"error":null}`)
}

func TestDeadlinePartialResult(t *testing.T) {
	t.Parallel()

	marked := partialBody(`{"deadline_truncated":true}`)
	if _, ok := deadlinePartialResult(200, marked, "other_tool", resultshape.Mode(""), 1024, nil); ok {
		t.Fatal("wrong tool accepted")
	}
	if _, ok := deadlinePartialResult(200, []byte("nope"), "investigate_code_topic", resultshape.Mode(""), 1024, nil); ok {
		t.Fatal("garbage body accepted")
	}
	if _, ok := deadlinePartialResult(200, partialBody(`[1,2]`), "investigate_code_topic", resultshape.Mode(""), 1024, nil); ok {
		t.Fatal("non-map data accepted")
	}
	if _, ok := deadlinePartialResult(200, partialBody(`{"deadline_truncated":"yes"}`), "investigate_code_topic", resultshape.Mode(""), 1024, nil); ok {
		t.Fatal("non-bool marker accepted")
	}
	if _, ok := deadlinePartialResult(200, partialBody(`{"deadline_truncated":false}`), "investigate_code_topic", resultshape.Mode(""), 1024, nil); ok {
		t.Fatal("unmarked envelope accepted")
	}
	res, ok := deadlinePartialResult(200, marked, "investigate_code_topic", resultshape.Mode(""), 1024, nil)
	if !ok || res == nil || res.IsError {
		t.Fatalf("marked envelope rejected: %+v", res)
	}
	errRes, ok := deadlinePartialResult(503, marked, "investigate_code_topic", resultshape.Mode(""), 1024, nil)
	if !ok || errRes == nil || !errRes.IsError {
		t.Fatal("5xx partial must keep IsError")
	}
}

func TestBoundedToolLabel(t *testing.T) {
	t.Parallel()

	names := registry.ToolNames()
	if len(names) == 0 {
		t.Fatal("no registered tool names")
	}
	if boundedToolLabel(names[0]) != names[0] {
		t.Fatal("registered name not returned")
	}
	if boundedToolLabel("no_such_tool_xyz") != "unknown" {
		t.Fatal("unknown name not bucketed")
	}
}
