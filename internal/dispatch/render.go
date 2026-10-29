package dispatch

// Result rendering, ported from go/internal/mcp/dispatch_envelope.go at
// 9922b49 (renderToolResult, mcpToolErrorResult, mcpToolErrorResultForCall).
// The MCP tool-result wire types live here because they are dispatch's
// output contract; the transport serves them without re-shaping. Telemetry
// record calls are dropped; the source keeps no log lines in these paths.
// See docs/parity.md.

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// ToolContent is one MCP content block.
type ToolContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	Resource *ToolResource `json:"resource,omitempty"`
}

// ToolResource is one MCP embedded-resource block.
type ToolResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ToolResult is the MCP tools/call result the transport serves.
type ToolResult struct {
	Content           []ToolContent     `json:"content"`
	StructuredContent any               `json:"structuredContent,omitempty"`
	IsError           bool              `json:"isError,omitempty"`
	Meta              map[string]string `json:"_meta,omitempty"`
}

// renderToolResult keeps MCP response construction shared by the byte guard
// and the transport, so the measured shape is the one sent to clients. The
// dual shape sends a text summary, the payload as an embedded resource, and
// structuredContent (omitted when ResourceOnly). The structured shape sends
// the text summary and structuredContent only.
func RenderToolResult(toolName string, result *Result) ToolResult {
	structuredShape := resultshape.OrDual(result.Shape) == resultshape.Structured
	var structuredContent any
	if result.Envelope != nil {
		summaryText := "MCP dispatch failed; details in structuredContent.error"
		if !structuredShape || !result.GenericDispatchError {
			summaryText = summarizeToolText(toolName, result.Envelope)
		}
		summary := ToolContent{Type: "text", Text: summaryText}
		if structuredShape {
			return ToolResult{Content: []ToolContent{summary}, StructuredContent: result.Envelope, IsError: result.IsError}
		}
		resourceText, marshalErr := json.Marshal(result.Envelope)
		if marshalErr != nil {
			resourceText = nil
		}
		if !result.ResourceOnly {
			structuredContent = result.Envelope
		}
		return ToolResult{
			Content: []ToolContent{
				summary,
				{Type: "resource", Resource: &ToolResource{
					URI:      "eshu://tool-result/envelope",
					MimeType: "application/eshu.envelope+json",
					Text:     string(resourceText),
				}},
			},
			StructuredContent: structuredContent,
			IsError:           result.IsError,
		}
	}
	if structuredShape && result.NonObjectEncoding != "" {
		return ToolResult{
			Content:           []ToolContent{{Type: "text", Text: fmt.Sprintf("%s result is in structuredContent.value", toolName)}},
			StructuredContent: map[string]any{"value": result.Value},
			Meta:              map[string]string{"eshu/nonObjectEncoding": result.NonObjectEncoding},
			IsError:           result.IsError,
		}
	}
	summary := ToolContent{Type: "text", Text: summarizePlainToolText(toolName, result.Value)}
	if structuredShape {
		return ToolResult{Content: []ToolContent{summary}, StructuredContent: result.Value, IsError: result.IsError}
	}
	resourceText, marshalErr := json.Marshal(result.Value)
	if marshalErr != nil {
		resourceText = nil
	}
	if !result.ResourceOnly {
		structuredContent = result.Value
	}
	return ToolResult{
		Content: []ToolContent{
			summary,
			{Type: "resource", Resource: &ToolResource{
				URI:      "eshu://tool-result/payload",
				MimeType: "application/json",
				Text:     string(resourceText),
			}},
		},
		StructuredContent: structuredContent,
		IsError:           result.IsError,
	}
}

// ToolErrorResult renders a dispatch failure without a transport budget.
// Callers that can send a tools/call result use ToolErrorResultForCall, so
// generic structured errors pass through the exact wire-size guard.
func ToolErrorResult(mode resultshape.Mode, err error) ToolResult {
	return ToolErrorResultForCall(mode, "", err, nil, 0)
}

// ToolErrorResultForCall renders typed timeout/cancellation detail as
// before. A generic failure under structured gets an evidence-neutral error
// envelope, measured by the same renderer that sends it. Dual keeps its
// original bytes.
func ToolErrorResultForCall(mode resultshape.Mode, toolName string, err error, logger *slog.Logger, budget int) ToolResult {
	structured, ok := dispatchErrorStructuredContent(err)
	if !ok && resultshape.OrDual(mode) == resultshape.Structured {
		envelope := &ResponseEnvelope{Error: &ErrorEnvelope{
			Code: "mcp_dispatch_error", Message: err.Error(), Capability: "mcp.dispatch",
		}}
		result := applyResponseBudget(&Result{
			Value: envelope, Envelope: envelope, IsError: true,
			Shape: resultshape.Structured, GenericDispatchError: true,
		}, toolName, budget, logger)
		return RenderToolResult(toolName, result)
	}
	result := ToolResult{
		Content: []ToolContent{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
	if !ok {
		return result
	}
	result.StructuredContent = structured
	if resultshape.OrDual(mode) == resultshape.Structured {
		return result
	}
	structuredJSON, marshalErr := json.Marshal(structured)
	if marshalErr != nil {
		structuredJSON = nil
	}
	result.Content = append(result.Content, ToolContent{
		Type: "resource",
		Resource: &ToolResource{
			URI:      "eshu://tool-error/dispatch",
			MimeType: "application/json",
			Text:     string(structuredJSON),
		},
	})
	return result
}
