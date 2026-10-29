package dispatch

// Dispatch over HTTP read contracts, replacing the in-tree runner half of
// go/internal/mcp/dispatch.go at 9922b49 (dispatchTool,
// dispatchToolWithOptions, runDispatch). Resolution is verbatim (resolve.go);
// execution calls core over the network through readcontract instead of an
// in-process handler. Every verdict — envelope pass-through, plain payload,
// timeout/cancelled typing, deadline-partial, 3xx warning, over-budget
// replacement — matches the source decision for decision. See docs/parity.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// Options tunes one dispatch.
type Options struct {
	// Timeout bounds the call; zero or negative selects the pinned default.
	Timeout time.Duration
	// ResponseByteBudget caps the rendered result; zero or negative disables
	// the guard. Tests override it to exercise the boundary.
	ResponseByteBudget int
}

// DefaultOptions seeds a dispatch with the pinned census bounds: the 30s
// timeout and the 256 KiB response budget every tool runs under.
func DefaultOptions() Options {
	return Options{
		Timeout:            defaultToolDispatchTimeout,
		ResponseByteBudget: defaultToolResponseByteBudget,
	}
}

// Outcome labels for one dispatch. Each names how the dispatch ended; the
// set is closed. The destination logs the label instead of counting it:
// without a telemetry bootstrap there is no counter to write.
const (
	outcomeOK             = "ok"
	outcomeError          = "error"
	outcomeOverBudget     = "over_budget"
	outcomeUnknownTool    = "unknown_tool"
	outcomeInvalidRequest = "invalid_request"
	outcomeTimeout        = "timeout"
	outcomePartialTimeout = "partial_timeout"
	outcomeCanceled       = "canceled"
)

// Dispatch runs one MCP tool call against core over HTTP read contracts. It
// resolves the tool to its versioned route, forwards the caller's credential
// verbatim, and returns the parsed result, or the typed error the transport
// renders.
func Dispatch(ctx context.Context, client *readcontract.Client, toolName string, args map[string]any, authHeader string, logger *slog.Logger, options Options) (*Result, error) {
	result, err := runDispatch(ctx, client, toolName, args, authHeader, logger, options)
	outcome := classifyOutcome(toolName, result, err)
	if logger != nil {
		logger.Debug("mcp tool dispatch ended", "tool", toolName, "outcome", outcome)
	}
	return result, err
}

// classifyOutcome names how a dispatch ended for the operator log.
func classifyOutcome(toolName string, result *Result, err error) string {
	if err != nil {
		if errors.Is(err, errUnknownTool) {
			return outcomeUnknownTool
		}
		if routeErr := (*routeRefusedError)(nil); errors.As(err, &routeErr) {
			return outcomeInvalidRequest
		}
		var ctxErr *dispatchContextErr
		if errors.As(err, &ctxErr) {
			if errors.Is(ctxErr.err, context.DeadlineExceeded) {
				return outcomeTimeout
			}
			return outcomeCanceled
		}
		return outcomeError
	}
	if result != nil && result.IsError && isOverBudget(result) {
		return outcomeOverBudget
	}
	if result != nil && result.IsError {
		return outcomeError
	}
	return outcomeOK
}

// routeRefusedError marks a route-builder refusal (anything ResolveRoute
// reports that is not errUnknownTool). It exists only to type the outcome;
// the error text itself is the refusal.
type routeRefusedError struct{ err error }

func (e *routeRefusedError) Error() string { return e.err.Error() }
func (e *routeRefusedError) Unwrap() error { return e.err }

// isOverBudget reports whether result is the over-budget replacement envelope.
func isOverBudget(result *Result) bool {
	env := result.Envelope
	return env != nil && env.Error != nil && env.Error.Code == errorCodeResponseOverBudget
}

func runDispatch(
	ctx context.Context,
	client *readcontract.Client,
	toolName string,
	args map[string]any,
	authHeader string,
	logger *slog.Logger,
	options Options,
) (*Result, error) {
	route, err := ResolveRoute(toolName, args)
	if err != nil {
		if errors.Is(err, errUnknownTool) {
			return nil, err
		}
		return nil, &routeRefusedError{err: err}
	}

	mode, _ := resultshape.FromContext(ctx)
	mode = resultshape.OrDual(mode)

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultToolDispatchTimeout
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if logger != nil {
		logger.Debug("dispatch tool", "tool", toolName, "method", route.Method, "path", route.Path)
	}

	if err := dispatchCtx.Err(); err != nil {
		return nil, dispatchContextError(toolName, timeout, err, logger)
	}
	status, raw, err := client.DoRaw(dispatchCtx, readcontract.RawRequest{
		Method: route.Method,
		Path:   route.Path,
		Query:  route.Query,
		Body:   route.Body,
	}, authHeader)
	if err != nil {
		if ctxErr := dispatchCtx.Err(); ctxErr != nil {
			if partial, ok := deadlinePartialResult(status, raw, toolName, mode, options.ResponseByteBudget, logger); ok {
				return partial, nil
			}
			return nil, dispatchContextError(toolName, timeout, ctxErr, logger)
		}
		return nil, err
	}
	if err := dispatchCtx.Err(); err != nil {
		// A core handler that answered its own deadline with a bounded
		// partial envelope already wrote the useful truth into the answer;
		// surface it instead of the data-nil timeout error. Anything else
		// keeps the error-only verdict.
		if errors.Is(err, context.DeadlineExceeded) {
			if partial, ok := deadlinePartialResult(status, raw, toolName, mode, options.ResponseByteBudget, logger); ok {
				return partial, nil
			}
		}
		return nil, dispatchContextError(toolName, timeout, err, logger)
	}

	if status >= 300 && status < 400 {
		// No MCP-reachable route redirects on purpose, so a 3xx means the
		// request never reached its handler. Warn before the body is parsed
		// so an envelope-shaped redirect body logs too, and an operator
		// can find the route builder that produced it.
		if logger != nil {
			logger.Warn("mcp tool dispatch got a redirect, not a handler answer",
				"tool", toolName, "status", status, "path", route.Path)
		}
	}

	if envelope, ok := parseCanonicalEnvelope(raw); ok {
		return applyResponseBudget(&Result{
			Value:    envelope,
			Envelope: envelope,
			IsError:  !dispatchStatusSucceeded(status),
			Shape:    mode,
		}, toolName, options.ResponseByteBudget, logger), nil
	}

	if !dispatchStatusSucceeded(status) {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(raw))
	}

	// The structured MCP field is an object. Preserve non-object JSON as raw
	// bytes so integers and exponent forms survive the HTTP-to-MCP boundary.
	// Dual retains its historical decoding and wire layout.
	if mode == resultshape.Structured {
		body := bytes.TrimSpace(raw)
		// Object-shaped answers take the existing single unmarshal below.
		// Validate only non-object candidates before retaining raw JSON.
		if len(body) == 0 || body[0] != '{' {
			if !json.Valid(body) {
				return applyResponseBudget(&Result{
					Value: string(raw), NonObjectEncoding: "text", Shape: mode,
				}, toolName, options.ResponseByteBudget, logger), nil
			}
			return applyResponseBudget(&Result{
				Value: json.RawMessage(bytes.Clone(body)), NonObjectEncoding: "json", Shape: mode,
			}, toolName, options.ResponseByteBudget, logger), nil
		}
	}

	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		encoding := ""
		if mode == resultshape.Structured {
			encoding = "text"
		}
		return applyResponseBudget(&Result{
			Value: string(raw), NonObjectEncoding: encoding, Shape: mode,
		}, toolName, options.ResponseByteBudget, logger), nil
	}
	return applyResponseBudget(&Result{Value: result, Shape: mode}, toolName, options.ResponseByteBudget, logger), nil
}
