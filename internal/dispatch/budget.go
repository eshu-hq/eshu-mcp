package dispatch

// Response-budget guard, ported from go/internal/mcp/dispatch_budget.go at
// 9922b49. The OTEL instruments (per-tool histogram, over-budget/fallback/
// page counters) stay in core: the destination has no telemetry bootstrap
// yet (NOT_CHECKED under #9498), so the record calls are dropped and the
// operator-visible slog lines are kept. Every sizing, paging, and envelope
// decision is unchanged. See docs/parity.md.

import (
	"encoding/json"
	"log/slog"
	"sync"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/rankwalk"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// defaultToolResponseByteBudget caps the serialized mcpToolResult, excluding
// the enclosing JSON-RPC response and transport newline. It matches the
// source census pin: TestToolRegistryCensusDispatchBounds fails the source
// build when this is not 256 KiB, and the dest parity script locks the same
// value from this side.
const defaultToolResponseByteBudget = 256 * 1024

// errorCodeResponseOverBudget is the canonical error code returned when a
// tool response exceeds the dispatch response-size budget.
const errorCodeResponseOverBudget ErrorCode = "mcp_response_over_budget"

// pagePositionKey is the row field a re-ranked page carries with each row's
// place in the page's offset order.
const pagePositionKey = "page_position"

// budgetPageReason is the reason string a budget page reports in
// data.budget_page.reason and in the truth.omissions detail.
const budgetPageReason = "response_byte_budget"

// budgetPagedTools names the tools whose data.results rows are independent,
// ordered, and re-readable through the data.offset the same tool accepts.
// Only these tools can answer an over-budget request with a page: a client
// that gets next_offset can pass it back as offset and read the remainder,
// so no row is lost. Every other tool keeps the over-budget error envelope.
// The value is the largest offset the tool accepts (0 when the page window
// bounds it already): a next_offset past it would be rejected, so it is not
// emitted.
func budgetPagedTools(toolName string) (maxOffset int, ok bool) {
	switch toolName {
	case "find_code":
		return 0, true
	case "search_entity_content":
		maxOffset, _, _ := registry.SourceBounds()
		return maxOffset, true
	default:
		return 0, false
	}
}

// budgetPage is the outcome of trimming an over-budget success envelope to
// a page of whole rows.
type budgetPage struct {
	result       *Result
	rowsReturned int
	rowsAvail    int
	bytes        int
}

var boundedToolLabels = sync.OnceValue(func() map[string]bool {
	labels := map[string]bool{"unknown": true}
	for _, name := range registry.ToolNames() {
		labels[name] = true
	}
	return labels
})

// boundedToolLabel returns a registered tool name or the one unknown bucket,
// so an unregistered name cannot grow an operator log label without bound.
func boundedToolLabel(requested string) string {
	if boundedToolLabels()[requested] {
		return requested
	}
	return "unknown"
}

// applyResponseBudget enforces a serialized response-size budget on a
// dispatch result. When budget <= 0 the guard is disabled and the result is
// returned unchanged. The budget counts the serialized result as sent in
// result.Shape. Under the dual shape, if both complete copies exceed the
// budget but the full embedded resource fits, the structured copy is
// omitted. The structured shape sends one copy, so it skips that step. If
// the result still exceeds the budget and the tool pages (budgetPagedTools),
// the result becomes the largest page of whole rows that fits, marked
// truncated with a next_offset (trimToBudgetPage). Otherwise, or when the
// first row alone is over budget, a bounded error envelope replaces the
// result.
//
// The replacement is itself an error envelope (IsError=true) so MCP clients
// and summarizers treat it as a structured failure, not partial data. A
// structured log event records every budget hit for 3 AM operability.
func applyResponseBudget(result *Result, toolName string, budget int, logger *slog.Logger) *Result {
	if result == nil {
		return result
	}
	result.ToolName = toolName
	result.Shape = resultshape.OrDual(result.Shape)
	mode := result.Shape
	if budget <= 0 {
		return result
	}
	label := boundedToolLabel(toolName)
	size := estimateResponseBytes(result)
	if size <= budget {
		return result
	}
	// The resource fallback exists because the dual shape sends the payload
	// twice. The structured shape sends it once, so size is already the
	// emitted size and goes straight to a page or the error envelope.
	singleCopy := mode == resultshape.Structured
	emittedSize := size
	if !singleCopy {
		result.ResourceOnly = true
		emittedSize = estimateResponseBytes(result)
	}
	if !singleCopy && emittedSize > 0 && emittedSize <= budget {
		if logger != nil {
			logger.Info("mcp tool response resource fallback",
				"tool", label,
				"shape", string(mode),
				"response_bytes", size,
				"emitted_bytes", emittedSize,
				"budget_bytes", budget,
			)
		}
		return result
	}
	if page, ok := trimToBudgetPage(result, toolName, budget); ok {
		if logger != nil {
			logger.Info("mcp tool response budget page",
				"tool", label,
				"shape", string(mode),
				"response_bytes", size,
				"emitted_bytes", page.bytes,
				"budget_bytes", budget,
				"rows_returned", page.rowsReturned,
				"rows_available", page.rowsAvail,
			)
		}
		return page.result
	}
	if logger != nil {
		// resource_bytes is the size of the resource-only copy, so it exists
		// only where that copy was measured. The structured shape has no such
		// copy: response_bytes is already the single emitted size.
		attrs := []any{
			"tool", label,
			"shape", string(mode),
			"response_bytes", size,
		}
		if !singleCopy {
			attrs = append(attrs, "resource_bytes", emittedSize)
		}
		attrs = append(attrs, "budget_bytes", budget)
		logger.Warn("mcp tool response over budget", attrs...)
	}
	overBudget := overBudgetResult(label, size, budget)
	overBudget.Shape = mode
	return overBudget
}

// estimateResponseBytes returns the exact serialized size of mcpToolResult,
// including the summary, embedded resource, and optional structured copy. It
// excludes the JSON-RPC wrapper and transport newline, as the old guard did.
// A marshal failure yields 0 so the guard does not refuse an unsized response.
func estimateResponseBytes(result *Result) int {
	if result == nil {
		return 0
	}
	encoded, err := json.Marshal(RenderToolResult(result.ToolName, result))
	if err != nil {
		return 0
	}
	return len(encoded)
}

// trimToBudgetPage trims an over-budget success envelope to the largest page
// of whole rows that fits budget in its single-copy (resource-only) form. It
// reports false when the tool does not page, the response is not a row list,
// or even the first row alone is over budget; the caller then keeps the
// over-budget error envelope.
//
// The trim runs on the serialized-size measure the budget itself uses
// (estimateResponseBytes), not on an estimate, so a returned page is within
// budget by construction. The page keeps the first rows of the page's offset
// order, in the order the handler displayed them, marks data.truncated, and
// sets data.next_offset to the offset of the first dropped row, so
// offset-paging the same request reads every row exactly once. When the
// handler re-ranked the page, each row carries page_position, its place in
// the offset order, and the cut keeps the rows with the smallest positions:
// the cursor is a position in the offset order, so the kept rows must be a
// prefix of it, not a prefix of the re-ranked order. A rank-walk page
// (data.rank_walk) is the exception: its rows are already in walk order, so
// the cut keeps rows[:keep], emits no next_offset, and re-mints
// rank_walk.next_cursor through rankwalk.TrimWalk; a walk block that cannot
// prove that cursor keeps the error envelope. The cut is a binary search
// over whole rows; a row is never split.
func trimToBudgetPage(result *Result, toolName string, budget int) (budgetPage, bool) {
	maxOffset, paged := budgetPagedTools(toolName)
	if !paged || result == nil || result.IsError {
		return budgetPage{}, false
	}
	// A dual page is a single-copy resource-only result; a structured page
	// is already single-copy, so it never sets ResourceOnly.
	mode := resultshape.OrDual(result.Shape)
	envelope := result.Envelope
	if envelope == nil || envelope.Error != nil || envelope.Truth == nil {
		return budgetPage{}, false
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return budgetPage{}, false
	}
	rows, ok := data["results"].([]any)
	if !ok || len(rows) < 2 {
		return budgetPage{}, false
	}
	_, rankWalk := data[rankwalk.BlockKey]
	positions, ok := rowPagePositions(rows)
	if !ok {
		return budgetPage{}, false
	}
	offset := pageOffset(data["offset"])
	cutFailed := false
	candidate := func(keep int) *Result {
		if !rankWalk {
			page := budgetPageEnvelope(envelope, data, offsetPrefix(rows, positions, keep), len(rows), keep, offset, maxOffset, budget)
			return &Result{Value: page, Envelope: page, ToolName: toolName, Shape: mode, ResourceOnly: mode == resultshape.Dual}
		}
		// A rank-walk page is cut to a prefix of its walk order, and the
		// walk block is re-minted so next_cursor resumes at the first
		// dropped row. It never carries next_offset.
		walk, ok := rankwalk.TrimWalk(data[rankwalk.BlockKey], rows[:keep])
		cutFailed = cutFailed || !ok
		page := budgetPageEnvelope(envelope, data, rows[:keep], len(rows), keep, offset, -1, budget)
		pageData, ok := page.Data.(map[string]any)
		if !ok {
			cutFailed = true
		} else {
			pageData[rankwalk.BlockKey] = walk
		}
		return &Result{Value: page, Envelope: page, ToolName: toolName, Shape: mode, ResourceOnly: mode == resultshape.Dual}
	}
	if size := estimateResponseBytes(candidate(1)); size <= 0 || size > budget {
		return budgetPage{}, false
	}
	// The full row set is known to be over budget in this form, so the answer
	// lies in [1, len(rows)-1]; keep is the largest count known to fit.
	keep, over := 1, len(rows)
	for over-keep > 1 {
		middle := keep + (over-keep)/2
		if size := estimateResponseBytes(candidate(middle)); size > 0 && size <= budget {
			keep = middle
		} else {
			over = middle
		}
	}
	page := candidate(keep)
	if cutFailed {
		return budgetPage{}, false
	}
	return budgetPage{
		result:       page,
		rowsReturned: keep,
		rowsAvail:    len(rows),
		bytes:        estimateResponseBytes(page),
	}, true
}

// rowPagePositions returns each row's position in the page's offset order. A
// page the handler did not re-rank carries no page_position key, so row i
// sits at position i. A re-ranked page must give every row a distinct
// integer position in [0, len(rows)); anything else reports false, and the
// caller keeps the over-budget error envelope rather than cut at a position
// it cannot prove.
func rowPagePositions(rows []any) ([]int, bool) {
	positions := make([]int, len(rows))
	seen := make([]bool, len(rows))
	keyed := 0
	for index, row := range rows {
		fields, ok := row.(map[string]any)
		if !ok {
			return nil, false
		}
		raw, present := fields[pagePositionKey]
		if !present {
			positions[index] = index
			continue
		}
		keyed++
		number, isNumber := raw.(float64)
		position := int(number)
		if !isNumber || float64(position) != number || position < 0 || position >= len(rows) || seen[position] {
			return nil, false
		}
		seen[position] = true
		positions[index] = position
	}
	return positions, keyed == 0 || keyed == len(rows)
}

// offsetPrefix returns the rows whose offset position is below keep, in the
// order the handler displayed them. For a page that was not re-ranked this
// is rows[:keep].
func offsetPrefix(rows []any, positions []int, keep int) []any {
	kept := make([]any, 0, keep)
	for index, row := range rows {
		if positions[index] < keep {
			kept = append(kept, row)
		}
	}
	return kept
}

// budgetPageEnvelope returns a copy of envelope that carries only kept, the
// keep rows at the front of the page's offset order. The original envelope
// and its data map are left untouched. The copy reports truncated, the next
// offset, a budget_page block, a truth.omissions entry, and clip counts
// recomputed over the kept rows. next_offset is left out when it would exceed
// maxOffset, because the tool would reject it.
func budgetPageEnvelope(
	envelope *ResponseEnvelope,
	data map[string]any,
	kept []any,
	available, keep, offset, maxOffset, budget int,
) *ResponseEnvelope {
	page := make(map[string]any, len(data)+2)
	for key, value := range data {
		page[key] = value
	}
	page["results"] = kept
	page["count"] = keep
	page["truncated"] = true
	if maxOffset >= 0 && (maxOffset == 0 || offset+keep <= maxOffset) {
		page["next_offset"] = offset + keep
	}
	page["budget_page"] = map[string]any{
		"reason":         budgetPageReason,
		"budget_bytes":   budget,
		"rows_returned":  keep,
		"rows_available": available,
	}
	recountClipped(page, "source_cache_clipped_rows", "source_cache_clipped", kept)
	recountClipped(page, "docstring_clipped_rows", "docstring_clipped", kept)

	truth := *envelope.Truth
	truth.Truncated = true
	truth.Omissions = append(append([]TruthOmission(nil), truth.Omissions...), TruthOmission{
		Section: "results",
		Detail:  budgetPageReason,
		Total:   available,
	})
	return &ResponseEnvelope{Data: page, Truth: &truth, Error: envelope.Error}
}

// recountClipped rewrites a response-level clipped-row count over the kept
// rows. It does nothing when the response never carried the count.
func recountClipped(page map[string]any, countKey, rowKey string, rows []any) {
	if _, present := page[countKey]; !present {
		return
	}
	clipped := 0
	for _, row := range rows {
		fields, ok := row.(map[string]any)
		if ok && fields[rowKey] == true {
			clipped++
		}
	}
	page[countKey] = clipped
}

// pageOffset reads the page's start offset from the decoded JSON data block.
// A missing, non-numeric, or negative value is offset zero.
func pageOffset(value any) int {
	if number, ok := value.(float64); ok && number > 0 {
		return int(number)
	}
	return 0
}
