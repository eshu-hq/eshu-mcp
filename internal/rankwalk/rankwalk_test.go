package rankwalk

import (
	"strings"
	"testing"
)

// TestAdvanceChains proves the cursor codec round-trips: advancing the start
// token over window ids mints a token that advances further, and the text
// keeps the source "<code>: <reason>: <message>" shape on failure.
func TestAdvanceChains(t *testing.T) {
	t.Parallel()

	start := cursor{tool: 1, lane: 1}.encode()
	first, err := Advance(start, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Advance(start) error = %v, want nil", err)
	}
	if !strings.HasPrefix(first, "rc1.") {
		t.Errorf("cursor = %q, want the versioned token", first)
	}
	second, err := Advance(first, []string{"c"})
	if err != nil {
		t.Fatalf("Advance(chained) error = %v, want nil", err)
	}
	if second == first {
		t.Error("chained advance returned the same token, want progress")
	}
	_, err = Advance(first, []string{"c", "c"})
	if err == nil || !strings.Contains(err.Error(), "rank_cursor_invalid: malformed:") {
		t.Errorf("repeated-id advance error = %v, want the malformed refusal", err)
	}
	_, err = Advance(first, []string{""})
	if err == nil {
		t.Error("empty-id advance succeeded, want the malformed refusal")
	}
	_, err = Advance("not-a-token", []string{"a"})
	if err == nil {
		t.Error("malformed advance succeeded, want the malformed refusal")
	}
	many := make([]string, 0, WindowSize+1)
	for i := 0; i <= WindowSize; i++ {
		many = append(many, "id")
	}
	if _, err := Advance(cursor{tool: 1, lane: 1}.encode(), many); err == nil {
		t.Error("past-window advance succeeded, want the malformed refusal")
	}
}

// TestTrimWalkCuts proves a walk page cuts to the kept prefix with a
// re-minted cursor, a kept count, and an open window.
func TestTrimWalkCuts(t *testing.T) {
	t.Parallel()

	cursor, err := Advance(cursor{tool: 1, lane: 1}.encode(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("mint cursor: %v", err)
	}
	block := map[string]any{"page_cursor": cursor, "order": "walk", "window_end": true}
	kept := []any{
		map[string]any{"entity_id": "a"},
		map[string]any{"entity_id": "b"},
	}
	trimmed, ok := TrimWalk(block, kept)
	if !ok {
		t.Fatal("TrimWalk(valid) = false, want the cut page")
	}
	if trimmed["next_cursor"] == nil || trimmed["next_cursor"] == "" {
		t.Error("cut page next_cursor is empty, want the resumed cursor")
	}
	if trimmed["position"] != 3+2 {
		t.Errorf("cut page position = %v, want the served-plus-kept count", trimmed["position"])
	}
	if trimmed["window_end"] != false {
		t.Errorf("cut page window_end = %v, want false", trimmed["window_end"])
	}
	if trimmed["order"] != "walk" {
		t.Error("cut page drops block keys, want the block carried over")
	}
}

// TestTrimWalkRefuses proves an unprovable cut keeps the over-budget error
// path: malformed cursor, cursor-less block, and rows without entity ids.
func TestTrimWalkRefuses(t *testing.T) {
	t.Parallel()

	rows := []any{map[string]any{"entity_id": "a"}}
	if _, ok := TrimWalk(map[string]any{"page_cursor": "bogus"}, rows); ok {
		t.Error("TrimWalk(malformed cursor) = true, want false")
	}
	if _, ok := TrimWalk(map[string]any{}, rows); ok {
		t.Error("TrimWalk(missing cursor) = true, want false")
	}
	cursor, err := Advance(cursor{tool: 1, lane: 1}.encode(), []string{"a"})
	if err != nil {
		t.Fatalf("mint cursor: %v", err)
	}
	if _, ok := TrimWalk(map[string]any{"page_cursor": cursor}, []any{map[string]any{"no_id": 1}}); ok {
		t.Error("TrimWalk(id-less row) = true, want false")
	}
}
