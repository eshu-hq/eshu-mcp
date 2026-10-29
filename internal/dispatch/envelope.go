package dispatch

// Wire envelope types and result rendering, ported from
// go/internal/mcp/dispatch_envelope.go at 9922b49. The structs mirror
// go/internal/query/querycontract/truth.go at that commit field for field,
// so a passed-through envelope re-marshals byte-identically; Data stays any
// so new payload keys survive without a type change. OTEL record calls are
// replaced by the slog lines the source keeps. See docs/parity.md.

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// ErrorCode is a stable machine-readable query error code.
type ErrorCode string

// ErrorEnvelope carries a stable query error.
type ErrorEnvelope struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message"`
	Capability    string         `json:"capability,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Profiles      *ErrorProfiles `json:"profiles,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

// ErrorProfiles names the active and minimum required profiles.
type ErrorProfiles struct {
	Current  string `json:"current,omitempty"`
	Required string `json:"required,omitempty"`
}

// ResponseEnvelope is the canonical {data, truth, error} envelope.
type ResponseEnvelope struct {
	Data  any            `json:"data"`
	Truth *TruthEnvelope `json:"truth"`
	Error *ErrorEnvelope `json:"error"`
}

// TruthEnvelope carries the truth labels of an answer.
type TruthEnvelope struct {
	Level      string          `json:"level"`
	Capability string          `json:"capability,omitempty"`
	Profile    string          `json:"profile,omitempty"`
	Basis      string          `json:"basis,omitempty"`
	Backend    string          `json:"backend,omitempty"`
	Freshness  TruthFreshness  `json:"freshness"`
	Reason     string          `json:"reason,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	Omissions  []TruthOmission `json:"omissions,omitempty"`
}

// TruthFreshness carries the freshness state of an answer.
type TruthFreshness struct {
	State      string `json:"state"`
	ObservedAt string `json:"observed_at,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Cause      string `json:"cause,omitempty"`
}

// TruthOmission names one omitted answer section.
type TruthOmission struct {
	Section string `json:"section"`
	Detail  string `json:"detail"`
	Total   int    `json:"total"`
}

// Result is one dispatch outcome, ready to render.
type Result struct {
	Value any
	// NonObjectEncoding marks a structured-only non-object HTTP success. The
	// renderer wraps Value in an object without mutating it.
	NonObjectEncoding string
	// GenericDispatchError selects the bounded summary for a generic error
	// synthesized at the transport boundary. The diagnostic lives once in
	// Envelope.Error.Message, not again in text content.
	GenericDispatchError bool
	Envelope             *ResponseEnvelope
	IsError              bool
	ToolName             string
	// ResourceOnly drops the structured copy of a dual-shape result whose two
	// copies exceed the response budget. It is never set under the structured
	// shape, which has no second copy to drop.
	ResourceOnly bool
	// Shape is the wire layout the result is rendered and budgeted in. The
	// zero value renders as dual.
	Shape resultshape.Mode
}

// parseCanonicalEnvelope reports whether body is a canonical {data, truth,
// error} envelope. A non-envelope payload dispatches as a plain value.
func parseCanonicalEnvelope(body []byte) (*ResponseEnvelope, bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false
	}
	if _, ok := top["data"]; !ok {
		return nil, false
	}
	if _, ok := top["truth"]; !ok {
		return nil, false
	}
	if _, ok := top["error"]; !ok {
		return nil, false
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false
	}
	return &envelope, true
}

// overBudgetResult builds the bounded canonical envelope returned in place
// of an over-budget tool response.
func overBudgetResult(toolName string, size, budget int) *Result {
	envelope := &ResponseEnvelope{
		Data: nil,
		Error: &ErrorEnvelope{
			Code:       ErrorCode(errorCodeResponseOverBudget),
			Message:    fmt.Sprintf("MCP tool %q response of %d bytes exceeds the %d byte response budget", toolName, size, budget),
			Capability: "mcp.dispatch",
			Details: map[string]any{
				"tool":             toolName,
				"response_bytes":   size,
				"budget_bytes":     budget,
				"estimated_tokens": estimateResponseTokens(size),
				"guidance":         responseBudgetGuidance(),
			},
		},
	}
	return &Result{
		Value:    envelope,
		Envelope: envelope,
		IsError:  true,
	}
}

// estimateResponseTokens converts a serialized byte size into a conservative
// token estimate using the repo's shared ~4-bytes-per-token heuristic. It is
// a bound, not a billing-grade tokenizer.
func estimateResponseTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

// responseBudgetGuidance returns a deterministic instruction teaching the
// agent how to shrink a tool response that exceeded the dispatch budget.
func responseBudgetGuidance() string {
	return "response exceeded the MCP response budget; lower limit, add repo_id/scope filters, " +
		"request a single relationship_type or direction, set a smaller token_budget where supported, " +
		"then drill in via the returned handles instead of fetching the whole result at once; " +
		"a row with source_cache_clipped=true has a clipped body, so call get_entity_content for its full text"
}
