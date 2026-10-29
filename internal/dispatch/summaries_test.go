package dispatch

import (
	"strings"
	"testing"
)

func envelopeWith(toolData map[string]any, truth *TruthEnvelope) *ResponseEnvelope {
	return &ResponseEnvelope{Data: toolData, Truth: truth}
}

func freshTruth() *TruthEnvelope {
	return &TruthEnvelope{Level: "authoritative", Freshness: TruthFreshness{State: freshnessFresh}}
}

// TestSummarizeEnvelopeErrors proves error envelopes render the code and
// message in every combination.
func TestSummarizeEnvelopeErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		code, message, want string
	}{
		"code and message": {"mcp_timeout", "too slow", "error mcp_timeout: too slow"},
		"code only":        {"mcp_timeout", "", "error mcp_timeout"},
		"message only":     {"", "too slow", "too slow"},
		"neither":          {"", "", "Eshu query returned an error."},
	}
	for name, c := range cases {
		got := summarizeToolText("find_code", &ResponseEnvelope{Error: &ErrorEnvelope{Code: ErrorCode(c.code), Message: c.message}})
		if got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
	if got := summarizeToolText("find_code", nil); got != "Eshu query completed." {
		t.Errorf("nil envelope summary = %q", got)
	}
}

// TestSummarizeTruthPrefixes proves every truth level/freshness combination
// leads the summary.
func TestSummarizeTruthPrefixes(t *testing.T) {
	t.Parallel()

	data := map[string]any{"count": float64(1)}
	cases := map[string]struct {
		truth *TruthEnvelope
		want  string
	}{
		"fresh": {freshTruth(), "authoritative/fresh"},
		"stale with cause": {&TruthEnvelope{
			Level:     "authoritative",
			Freshness: TruthFreshness{State: "stale", Cause: "index-rebuild"},
		}, "authoritative/stale (cause: index-rebuild)"},
		"level only": {&TruthEnvelope{Level: "authoritative"}, "authoritative"},
		"state only": {&TruthEnvelope{Freshness: TruthFreshness{State: "stale"}}, "stale"},
		"nil truth":  {nil, "Returned 1 result(s)."},
	}
	for name, c := range cases {
		got := summarizeToolText("count_repositories_by_language", envelopeWith(data, c.truth))
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: summary %q lacks %q", name, got, c.want)
		}
		if len(got) > maxSummaryLength+3 {
			t.Errorf("%s: summary exceeds the bound: %q", name, got)
		}
	}
}

// TestSummarizeToolBranches drives every tool-specific envelope summarizer
// with a representative payload and requires its key signals.
func TestSummarizeToolBranches(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		data map[string]any
		want []string
	}{
		"get_service_story": {map[string]any{
			"service_identity": map[string]any{"service_name": "api", "repo_count": float64(2)},
			"api_surface":      map[string]any{"endpoint_count": float64(5)},
		}, []string{"api"}},
		"investigate_service": {map[string]any{
			"service_name":           "api",
			"investigation_findings": []any{map[string]any{"f": 1}},
			"coverage_summary":       map[string]any{"truncated": true, "state": "partial"},
			"recommended_next_calls": []any{map[string]any{"tool": "find_code"}},
		}, []string{"api — 1 finding(s)", "truncated", "coverage partial", "next: find_code"}},
		"get_incident_context": {map[string]any{
			"incident":         map[string]any{"title": "outage"},
			"related_changes":  []any{map[string]any{"c": 1}},
			"missing_evidence": []any{map[string]any{"m": 1}, map[string]any{"m": 2}},
			"truncated":        true,
		}, []string{"outage — 1 related change(s)", "missing evidence 2", "ambiguous 0", "truncated"}},
		"build_evidence_citation_packet": {map[string]any{
			"coverage": map[string]any{"resolved_count": float64(2), "input_handle_count": float64(3), "missing_count": float64(1)},
		}, []string{"resolved 2/requested 3", "missing 1"}},
		"derive_visualization_packet": {map[string]any{
			"visualization_packet": map[string]any{"view": "graph", "nodes": []any{1, 2}, "edges": []any{1}, "supported": true},
		}, []string{"visualization graph: 2 node(s), 1 edge(s)"}},
		"analyze_code_relationships": {map[string]any{
			"relationships": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
		}, []string{"2"}},
	}
	for tool, c := range cases {
		got := summarizeToolText(tool, envelopeWith(c.data, freshTruth()))
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: summary %q lacks %q", tool, got, want)
			}
		}
		if len(got) > maxSummaryLength+3 {
			t.Errorf("%s: summary exceeds the bound", tool)
		}
	}
}

// TestSummarizePlainBranches drives every plain-payload summarizer.
func TestSummarizePlainBranches(t *testing.T) {
	t.Parallel()

	if got := summarizePlainToolText("get_index_status", map[string]any{"status": "healthy", "reasons": []any{"r1", "r2"}}); got != "index status: healthy — r1 (+1 more)" {
		t.Errorf("index status summary = %q", got)
	}
	if got := summarizePlainToolText("get_index_status", map[string]any{"scoped": true, "repository_count": float64(1)}); !strings.Contains(got, "1 repository in your grant") {
		t.Errorf("scoped index summary = %q", got)
	}
	if got := summarizePlainToolText("get_hosted_readiness", map[string]any{"state": "blocked", "failure_classes": []any{"db"}}); got != "hosted readiness: blocked — db" {
		t.Errorf("readiness summary = %q", got)
	}
	if got := summarizePlainToolText("get_ingester_status", map[string]any{"ingester": "git", "health": map[string]any{"state": "degraded", "reasons": []any{"lag"}}}); got != "ingester git: degraded — lag" {
		t.Errorf("ingester summary = %q", got)
	}
	if got := summarizePlainToolText("list_ingesters", map[string]any{"ingesters": []any{"a", "b"}}); got != "2 ingester(s) registered" {
		t.Errorf("ingester list summary = %q", got)
	}
	if got := summarizePlainToolText("list_collectors", map[string]any{"collectors": []any{"c1"}}); got != "1 collector(s) registered" {
		t.Errorf("collector list summary = %q", got)
	}
	if got := summarizePlainToolText("something_else", map[string]any{"a": 1}); got == "" {
		t.Error("generic plain summary is empty")
	}
	if got := summarizePlainToolText("something_else", "just text"); got != "Eshu query completed." {
		t.Errorf("generic text summary = %q", got)
	}
	if got := summarizePlainToolText("something_else", []any{1, 2}); got != "Returned 2 result(s)." {
		t.Errorf("generic slice summary = %q", got)
	}
}

// TestSummarizeBounds proves long payloads clamp instead of growing the
// summary.
func TestSummarizeBounds(t *testing.T) {
	t.Parallel()

	big := map[string]any{"blob": strings.Repeat("z", 10*maxSummaryLength)}
	if got := summarizePlainToolText("unknown_tool", big); len(got) > maxSummaryLength+3 {
		t.Errorf("unbounded generic summary of length %d", len(got))
	}
	envelope := envelopeWith(map[string]any{"answer_metadata": map[string]any{
		"limitations": []any{strings.Repeat("l", 500)},
		"truncated":   true,
	}}, freshTruth())
	if got := summarizeToolText("find_code", envelope); len(got) > maxSummaryLength+3 {
		t.Errorf("unbounded envelope summary of length %d", len(got))
	}
}

// TestSummarizeToleratesNonObjectShapes proves mistyped JSON values degrade
// to empty summaries instead of panicking: the seed only carries objects,
// but core answers evolve independently of this seam.
func TestSummarizeToleratesNonObjectShapes(t *testing.T) {
	t.Parallel()

	packet := map[string]any{"visualization_packet": map[string]any{
		"view":        "graph",
		"limitations": []any{float64(42)},
	}}
	if got := summarizeVisualizationPacket(packet); got != "visualization graph unsupported" {
		t.Errorf("visualization summary = %q, want the bare unsupported form", got)
	}
	if got := firstStringField(map[string]any{"limitations": []any{float64(42)}}, "limitations"); got != "" {
		t.Errorf("firstStringField(non-string) = %q, want empty", got)
	}
	if got := mcpAnswerMetadata(map[string]any{"answer_metadata": "stale"}); got != nil {
		t.Errorf("mcpAnswerMetadata(string) = %v, want nil", got)
	}
	got := summarizeCodeRelationships(map[string]any{"relationships": []any{"nope"}})
	if !strings.Contains(got, "1 unsupported") {
		t.Errorf("code relationships summary = %q, want the unsupported count", got)
	}
}
