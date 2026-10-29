package dispatch

// Dispatch deadline and cancellation errors, ported from
// go/internal/mcp/dispatch_timeout.go at 9922b49. The partial-envelope path
// reads the HTTP response body the core answered with instead of the
// in-process recorder; the verdicts are unchanged. See docs/parity.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// defaultToolDispatchTimeout bounds every tool dispatch. It matches the
// source census pin: TestToolRegistryCensusDispatchBounds fails the source
// build when this is not 30s, and the dest census test below locks the same
// value from this side.
const defaultToolDispatchTimeout = 30 * time.Second

// dispatchContextErr carries a dispatch that ended with its context: a
// deadline (timeout) or a caller cancellation.
type dispatchContextErr struct {
	toolName string
	timeout  time.Duration
	err      error
}

func (e *dispatchContextErr) Error() string {
	if errors.Is(e.err, context.DeadlineExceeded) {
		return fmt.Sprintf("MCP tool %q dispatch deadline exceeded: %v", e.toolName, e.err)
	}
	return fmt.Sprintf("MCP tool %q dispatch canceled: %v", e.toolName, e.err)
}

func (e *dispatchContextErr) Unwrap() error {
	return e.err
}

func dispatchContextError(toolName string, timeout time.Duration, err error, logger *slog.Logger) error {
	if logger != nil {
		logger.Warn("mcp tool dispatch context ended", "tool", toolName, "timeout", timeout.String(), "err", err)
	}
	return &dispatchContextErr{
		toolName: toolName,
		timeout:  timeout,
		err:      err,
	}
}

// partialCapableToolName is the only MCP tool whose handler can answer its
// own deadline with a bounded partial envelope. Any other tool reusing the
// deadline_truncated field name must not silently opt into success-on-timeout.
const partialCapableToolName = "investigate_code_topic"

// deadlinePartialResult surfaces a bounded deadline-partial envelope when
// core answered the dispatch deadline with one: the body holds a canonical
// envelope whose data carries deadline_truncated. It reports false for
// anything else — a missing or unparseable body, a non-envelope payload, an
// envelope without the marker, or a tool other than the partial-capable
// one — so those keep the data-nil timeout verdict. A parent cancellation
// (not a deadline) never takes this path: the client is gone, so there is
// no one to read the partial.
func deadlinePartialResult(status int, body []byte, toolName string, mode resultshape.Mode, responseByteBudget int, logger *slog.Logger) (*Result, bool) {
	if toolName != partialCapableToolName {
		return nil, false
	}
	envelope, ok := parseCanonicalEnvelope(body)
	if !ok || envelope == nil {
		return nil, false
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return nil, false
	}
	marked, ok := data["deadline_truncated"].(bool)
	if !ok {
		return nil, false
	}
	if !marked {
		return nil, false
	}
	if logger != nil {
		logger.Debug("mcp tool dispatch deadline exceeded; surfacing bounded partial", "tool", toolName)
	}
	return applyResponseBudget(&Result{
		Value:    envelope,
		Envelope: envelope,
		IsError:  status >= 400,
		Shape:    mode,
	}, toolName, responseByteBudget, logger), true
}

// dispatchErrorStructuredContent renders a dispatch context error as the
// canonical {data, truth, error} timeout/cancelled envelope.
func dispatchErrorStructuredContent(err error) (map[string]any, bool) {
	var dispatchErr *dispatchContextErr
	if !errors.As(err, &dispatchErr) {
		return nil, false
	}
	code := "mcp_dispatch_canceled"
	if errors.Is(dispatchErr.err, context.DeadlineExceeded) {
		code = "mcp_dispatch_timeout"
	}
	return map[string]any{
		"data":  nil,
		"truth": nil,
		"error": map[string]any{
			"code":       code,
			"message":    dispatchErr.Error(),
			"capability": "mcp.dispatch",
			"details": map[string]any{
				"tool":               dispatchErr.toolName,
				"configured_timeout": dispatchErr.timeout.String(),
			},
		},
	}, true
}
