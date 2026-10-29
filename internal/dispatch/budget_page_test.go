package dispatch

import (
	"encoding/json"
	"strings"
	"testing"
)

func pagedEnvelope(rows []any, extra map[string]any) *ResponseEnvelope {
	data := map[string]any{"results": rows, "count": len(rows)}
	for key, value := range extra {
		data[key] = value
	}
	return &ResponseEnvelope{Data: data, Truth: &TruthEnvelope{Level: "authoritative"}}
}

func textRows(prefix string, count int, size int) []any {
	rows := make([]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{"id": float64(i), "text": prefix + strings.Repeat("x", size)})
	}
	return rows
}

func renderBytes(t *testing.T, result *Result) int {
	t.Helper()

	raw, err := json.Marshal(RenderToolResult("find_code", result))
	if err != nil {
		t.Fatalf("marshal rendered result: %v", err)
	}
	return len(raw)
}

// TestBudgetPagesRows proves an over-budget row list becomes a page of whole
// rows with a next_offset, not an error.
func TestBudgetPagesRows(t *testing.T) {
	t.Parallel()

	rows := textRows("row", 5, 2000)
	full := &Result{Value: pagedEnvelope(rows, nil), Envelope: pagedEnvelope(rows, nil)}
	resourceOnly := &Result{Value: full.Value, Envelope: full.Envelope, ResourceOnly: true, Shape: full.Shape}
	paged := applyResponseBudget(full, "find_code", renderBytes(t, resourceOnly)-100, nil)
	if paged.IsError {
		t.Fatalf("paged result isError = true, want the page: %+v", paged.Envelope)
	}
	page, ok := paged.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("paged envelope data = %T, want map[string]any", paged.Envelope.Data)
	}
	kept, ok := page["results"].([]any)
	if !ok {
		t.Fatalf("page results = %T, want []any", page["results"])
	}
	if len(kept) == 0 || len(kept) >= 5 {
		t.Fatalf("page keeps %d rows, want a proper prefix", len(kept))
	}
	if page["truncated"] != true {
		t.Error("page truncated is not true")
	}
	nextRaw, marshalErr := json.Marshal(page["next_offset"])
	if marshalErr != nil {
		t.Fatalf("marshal next_offset: %v", marshalErr)
	}
	keepRaw, marshalErr := json.Marshal(len(kept))
	if marshalErr != nil {
		t.Fatalf("marshal kept length: %v", marshalErr)
	}
	if string(nextRaw) != string(keepRaw) {
		t.Errorf("page next_offset wire = %s, want %s", nextRaw, keepRaw)
	}
	// The kept rows are the front of the offset order, in display order.
	for i, row := range kept {
		fields, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("kept row %d = %T, want map[string]any", i, row)
		}
		if fields["id"] != float64(i) {
			t.Errorf("kept row %d = %v, want the offset prefix in order", i, row)
		}
	}
}

// TestBudgetPageRerankedPrefix proves a re-ranked page cuts by offset
// position, not display order.
func TestBudgetPageRerankedPrefix(t *testing.T) {
	t.Parallel()

	rows := []any{
		map[string]any{"id": "display-first", "text": strings.Repeat("a", 3000), "page_position": float64(2)},
		map[string]any{"id": "display-second", "text": strings.Repeat("b", 3000), "page_position": float64(0)},
		map[string]any{"id": "display-third", "text": strings.Repeat("c", 3000), "page_position": float64(1)},
	}
	full := &Result{Value: pagedEnvelope(rows, nil), Envelope: pagedEnvelope(rows, nil)}
	paged := applyResponseBudget(full, "find_code", 4000, nil)
	if paged.IsError {
		t.Fatalf("paged result isError = true, want the page: %+v", paged.Envelope)
	}
	page, ok := paged.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("paged envelope data = %T, want map[string]any", paged.Envelope.Data)
	}
	kept, ok := page["results"].([]any)
	if !ok {
		t.Fatalf("page results = %T, want []any", page["results"])
	}
	if len(kept) == 0 {
		t.Fatal("page keeps no rows")
	}
	for _, row := range kept {
		fields, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("kept row = %T, want map[string]any", row)
		}
		pos, ok := fields["page_position"].(float64)
		if !ok {
			t.Fatalf("kept row %v lacks a page_position", row)
		}
		if pos >= float64(len(kept)) {
			t.Errorf("kept row %v breaks the offset prefix", row)
		}
	}
}

// TestBudgetPageRankWalk proves a rank-walk page cuts in walk order, mints
// next_cursor, and carries no next_offset.
func TestBudgetPageRankWalk(t *testing.T) {
	t.Parallel()

	ids := []string{"e1", "e2", "e3", "e4", "e5"}
	rows := make([]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"entity_id": id, "text": strings.Repeat("w", 2000)})
	}
	// mintedCursor is rankwalk.Advance over e1..e5 from the zero walk
	// state; the codec is deterministic, so the token is stable.
	const mintedCursor = "rc1.AQEBAAUAAAAAAAAAAAAAAAAAAAAA-GI23p67wfMTRYhNBGQ99yhw8rAbyiR6"
	cursor := mintedCursor
	full := &Result{
		Value:    pagedEnvelope(rows, map[string]any{"rank_walk": map[string]any{"page_cursor": cursor}}),
		Envelope: pagedEnvelope(rows, map[string]any{"rank_walk": map[string]any{"page_cursor": cursor}}),
	}
	paged := applyResponseBudget(full, "search_entity_content", 5000, nil)
	if paged.IsError {
		t.Fatalf("paged result isError = true, want the walk page: %+v", paged.Envelope)
	}
	page, ok := paged.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("paged envelope data = %T, want map[string]any", paged.Envelope.Data)
	}
	if _, present := page["next_offset"]; present {
		t.Error("walk page carries next_offset, want only the re-minted cursor")
	}
	walk, ok := page["rank_walk"].(map[string]any)
	if !ok {
		t.Fatalf("page rank_walk = %T, want map[string]any", page["rank_walk"])
	}
	if walk["next_cursor"] == nil || walk["next_cursor"] == "" {
		t.Errorf("walk page next_cursor = %v, want the resumed cursor", walk["next_cursor"])
	}
	if walk["window_end"] != false {
		t.Errorf("walk page window_end = %v, want false", walk["window_end"])
	}
}

// TestBudgetPageMaxOffsetSuppressesNextOffset proves a next_offset the tool
// would reject is not emitted.
func TestBudgetPageMaxOffsetSuppressesNextOffset(t *testing.T) {
	t.Parallel()

	rows := textRows("row", 4, 3000)
	data := map[string]any{"results": rows, "count": len(rows), "offset": float64(10000)}
	envelope := &ResponseEnvelope{Data: data, Truth: &TruthEnvelope{Level: "authoritative"}}
	full := &Result{Value: envelope, Envelope: envelope}
	paged := applyResponseBudget(full, "search_entity_content", 4000, nil)
	if paged.IsError {
		t.Fatalf("paged result isError = true, want the page: %+v", paged.Envelope)
	}
	page, ok := paged.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("paged envelope data = %T, want map[string]any", paged.Envelope.Data)
	}
	if _, present := page["next_offset"]; present {
		t.Errorf("page carries next_offset past the max offset: %v", page["next_offset"])
	}
	if page["truncated"] != true {
		t.Error("page truncated is not true")
	}
}

// TestBudgetSingleRowOverBudgetReplaces proves a one-row response that does
// not fit is replaced by the bounded error envelope: a page needs at least
// two rows to cut.
func TestBudgetSingleRowOverBudgetReplaces(t *testing.T) {
	t.Parallel()

	rows := textRows("solo", 1, 5000)
	full := &Result{Value: pagedEnvelope(rows, nil), Envelope: pagedEnvelope(rows, nil)}
	replaced := applyResponseBudget(full, "find_code", 100, nil)
	if !replaced.IsError || !isOverBudget(replaced) {
		t.Errorf("single-row over-budget result = %+v, want the replacement envelope", replaced.Envelope)
	}
}

// TestBudgetUnpagedToolReplaces proves a tool outside the page set is
// replaced, never paged.
func TestBudgetUnpagedToolReplaces(t *testing.T) {
	t.Parallel()

	rows := textRows("row", 5, 2000)
	full := &Result{Value: pagedEnvelope(rows, nil), Envelope: pagedEnvelope(rows, nil)}
	replaced := applyResponseBudget(full, "get_repo_summary", 100, nil)
	if !replaced.IsError || !isOverBudget(replaced) {
		t.Errorf("unpaged over-budget result = %+v, want the replacement envelope", replaced.Envelope)
	}
}

// TestBudgetRecountsClippedRows proves the page rewrites clipped-row counts
// over the kept rows.
func TestBudgetRecountsClippedRows(t *testing.T) {
	t.Parallel()

	rows := []any{
		map[string]any{"id": float64(0), "text": strings.Repeat("a", 3000), "source_cache_clipped": true},
		map[string]any{"id": float64(1), "text": strings.Repeat("b", 3000), "source_cache_clipped": true},
		map[string]any{"id": float64(2), "text": strings.Repeat("c", 3000)},
	}
	data := map[string]any{"results": rows, "count": len(rows), "source_cache_clipped_rows": 2}
	full := &Result{Value: pagedEnvelope(rows, data), Envelope: pagedEnvelope(rows, data)}
	resourceOnly := &Result{Value: full.Value, Envelope: full.Envelope, ResourceOnly: true, Shape: full.Shape}
	paged := applyResponseBudget(full, "find_code", renderBytes(t, resourceOnly)-100, nil)
	if paged.IsError {
		t.Fatalf("paged result isError = true, want the page: %+v", paged.Envelope)
	}
	page, ok := paged.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("paged envelope data = %T, want map[string]any", paged.Envelope.Data)
	}
	kept, ok := page["results"].([]any)
	if !ok {
		t.Fatalf("page results = %T, want []any", page["results"])
	}
	clipped := 0
	for _, row := range kept {
		fields, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("kept row = %T, want map[string]any", row)
		}
		if fields["source_cache_clipped"] == true {
			clipped++
		}
	}
	wire, marshalErr := json.Marshal(page["source_cache_clipped_rows"])
	if marshalErr != nil {
		t.Fatalf("marshal clipped rows: %v", marshalErr)
	}
	want, marshalErr := json.Marshal(clipped)
	if marshalErr != nil {
		t.Fatalf("marshal clipped count: %v", marshalErr)
	}
	if string(wire) != string(want) {
		t.Errorf("clipped count wire = %s, want %s", wire, want)
	}
}
