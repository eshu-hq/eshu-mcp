package dispatch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// TestDispatchRawOverBudgetMapsToEnvelope proves a core body over the raw
// read cap renders the same mcp_response_over_budget envelope as the render
// budget: tool name, budget, and guidance in both result shapes. Today the
// raw-cap error falls through to the generic dispatch error.
func TestDispatchRawOverBudgetMapsToEnvelope(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: strings.Repeat("x", readcontract.DefaultResponseByteBudget+1)}
	result, err := dispatchAgainstStub(t, stub, "find_code",
		map[string]any{"query": "sweep"}, "", DefaultOptions())
	if result != nil || err == nil {
		t.Fatalf("Dispatch() = (%v, %v), want the raw-cap error", result, err)
	}
	if !errors.Is(err, readcontract.ErrOverBudget) {
		t.Fatalf("Dispatch() error = %v, want ErrOverBudget", err)
	}

	for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
		rendered := ToolErrorResultForCall(mode, "find_code", err, nil, DefaultOptions().ResponseByteBudget)
		if !rendered.IsError {
			t.Fatalf("shape %s: IsError = false, want true", mode)
		}
		envelope, ok := rendered.StructuredContent.(*ResponseEnvelope)
		if !ok || envelope == nil || envelope.Error == nil {
			t.Fatalf("shape %s: structured content = %#v, want the error envelope", mode, rendered.StructuredContent)
		}
		if envelope.Error.Code != errorCodeResponseOverBudget {
			t.Errorf("shape %s: code = %q, want %q", mode, envelope.Error.Code, errorCodeResponseOverBudget)
		}
		details := envelope.Error.Details
		if details["tool"] != "find_code" {
			t.Errorf("shape %s: details.tool = %v, want find_code", mode, details["tool"])
		}
		if details["budget_bytes"] != readcontract.DefaultResponseByteBudget {
			t.Errorf("shape %s: details.budget_bytes = %v, want %d", mode, details["budget_bytes"], readcontract.DefaultResponseByteBudget)
		}
		if details["response_bytes"] != readcontract.DefaultResponseByteBudget+1 {
			t.Errorf("shape %s: details.response_bytes = %v, want at least budget+1", mode, details["response_bytes"])
		}
		guidance, ok := details["guidance"].(string)
		if !ok {
			t.Errorf("shape %s: details.guidance = %v (%T), want a string", mode, details["guidance"], details["guidance"])
		} else if !strings.Contains(guidance, "lower limit") {
			t.Errorf("shape %s: guidance = %q, want the budget guidance", mode, guidance)
		}
		// The core body is never echoed: neither the error text nor the
		// rendered result may carry the over-cap bytes.
		if strings.Contains(err.Error(), strings.Repeat("x", 64)) {
			t.Errorf("shape %s: error text echoes the core body", mode)
		}
		encoded, marshalErr := json.Marshal(rendered)
		if marshalErr != nil {
			t.Fatalf("shape %s: marshal rendered result: %v", mode, marshalErr)
		}
		if strings.Contains(string(encoded), strings.Repeat("x", 64)) {
			t.Errorf("shape %s: rendered result echoes the core body", mode)
		}
		// Each shape keeps its renderer contract: dual sends text plus
		// the embedded resource, structured sends text only.
		if mode == resultshape.Structured {
			if len(rendered.Content) != 1 || rendered.Content[0].Resource != nil {
				t.Errorf("shape %s: content = %+v, want one text block", mode, rendered.Content)
			}
		} else if len(rendered.Content) != 2 || rendered.Content[1].Resource == nil {
			t.Errorf("shape %s: content = %+v, want text plus resource", mode, rendered.Content)
		}
	}
}

// TestClassifyOutcomeCountsRawOverBudget proves the operator log names the
// wire-cap refusal instead of folding it into the generic error bucket.
func TestClassifyOutcomeCountsRawOverBudget(t *testing.T) {
	t.Parallel()

	over := &readcontract.OverBudgetError{Method: "GET", Path: "/api/v0/repos", Budget: 16}
	if got := classifyOutcome("find_code", nil, over); got != outcomeOverBudget {
		t.Errorf("classifyOutcome(over-budget) = %q, want %q", got, outcomeOverBudget)
	}
	if got := classifyOutcome("find_code", nil, errors.New("boom")); got != outcomeError {
		t.Errorf("classifyOutcome(generic) = %q, want %q", got, outcomeError)
	}
}

// TestDispatchBudgetsStayAligned relates the two 256 KiB caps so they cannot
// drift apart unnoticed: the wire read cap owns the bytes on the wire, the
// render budget owns the rendered result, and production keeps them equal.
// A zero dispatch budget disables the render guard; that path is covered by
// TestRawOverBudgetZeroDispatchBudgetKeepsEnvelope, not by this pin.
func TestDispatchBudgetsStayAligned(t *testing.T) {
	t.Parallel()

	if defaultToolResponseByteBudget != readcontract.DefaultResponseByteBudget {
		t.Errorf("defaultToolResponseByteBudget = %d, want the read cap %d",
			defaultToolResponseByteBudget, readcontract.DefaultResponseByteBudget)
	}
	if defaultToolResponseByteBudget != 256*1024 {
		t.Errorf("defaultToolResponseByteBudget = %d, want 256 KiB", defaultToolResponseByteBudget)
	}
}

// TestRawOverBudgetZeroDispatchBudgetKeepsEnvelope proves a disabled render
// guard (budget <= 0, as in the ToolErrorResult shorthand) still renders
// the envelope from the cap the error carries.
func TestRawOverBudgetZeroDispatchBudgetKeepsEnvelope(t *testing.T) {
	t.Parallel()

	err := &readcontract.OverBudgetError{Method: "GET", Path: "/api/v0/repos", Budget: 16}
	for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
		rendered := ToolErrorResultForCall(mode, "find_code", err, nil, 0)
		if !rendered.IsError {
			t.Fatalf("shape %s: IsError = false, want true with a disabled guard", mode)
		}
		envelope, ok := rendered.StructuredContent.(*ResponseEnvelope)
		if !ok || envelope == nil || envelope.Error == nil {
			t.Fatalf("shape %s: structured content = %#v, want the error envelope", mode, rendered.StructuredContent)
		}
		if envelope.Error.Code != errorCodeResponseOverBudget {
			t.Errorf("shape %s: code = %q, want %q", mode, envelope.Error.Code, errorCodeResponseOverBudget)
		}
		if envelope.Error.Details["budget_bytes"] != 16 {
			t.Errorf("shape %s: budget_bytes = %v, want the cap from the error", mode, envelope.Error.Details["budget_bytes"])
		}
		if envelope.Error.Details["response_bytes"] != 17 {
			t.Errorf("shape %s: response_bytes = %v, want cap+1", mode, envelope.Error.Details["response_bytes"])
		}
	}
	// The no-budget shorthand keeps the same envelope.
	shorthand := ToolErrorResult(resultshape.Structured, err)
	envelope, ok := shorthand.StructuredContent.(*ResponseEnvelope)
	if !ok || envelope == nil || envelope.Error == nil || envelope.Error.Code != errorCodeResponseOverBudget {
		t.Errorf("shorthand structured content = %#v, want the over-budget envelope", shorthand.StructuredContent)
	}
}

// TestDispatchRawCapExactBoundary proves the raw cap fires only past the
// limit: a body of exactly the budget passes the wire read.
func TestDispatchRawCapExactBoundary(t *testing.T) {
	t.Parallel()

	stub := &stubCore{status: 200, body: strings.Repeat("x", readcontract.DefaultResponseByteBudget)}
	result, err := dispatchAgainstStub(t, stub, "find_code",
		map[string]any{"query": "sweep"}, "", DefaultOptions())
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil at exactly the budget", err)
	}
	if result == nil {
		t.Fatal("Dispatch() result is nil, want the dispatch outcome")
	}
}
