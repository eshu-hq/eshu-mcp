package dispatch

import (
	"strings"
	"testing"
)

func TestSummarizeServiceStoryBranches(t *testing.T) {
	t.Parallel()

	if got := summarizeServiceStory(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	full := summarizeServiceStory(map[string]any{
		"service_identity": map[string]any{
			"service_name": "checkout",
			"limitations":  []any{"slow", "flaky"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 12,
			"truncated":      true,
		},
		"result_limits": map[string]any{
			"upstream_count":   3,
			"downstream_count": 4,
		},
		"answer_metadata": map[string]any{
			"limitations": []any{
				map[string]any{"reason": "quota"},
				map[string]any{"reason": "latency"},
			},
		},
	})
	for _, want := range []string{"checkout", "API surface 12 (truncated)", "deps 3", "consumers 4", "top limitation: quota", "(+1 more)"} {
		if !strings.Contains(full, want) {
			t.Fatalf("missing %q in %q", want, full)
		}
	}
	fallback := summarizeServiceStory(map[string]any{
		"service_identity": map[string]any{"service_id": "svc-9"},
	})
	if !strings.HasPrefix(fallback, "svc-9") {
		t.Fatalf("service_id fallback: %q", fallback)
	}
	anonymous := summarizeServiceStory(map[string]any{})
	if !strings.HasPrefix(anonymous, "service") {
		t.Fatalf("anonymous fallback: %q", anonymous)
	}
	identityLimits := summarizeServiceStory(map[string]any{
		"service_identity": map[string]any{"limitations": []any{"only"}},
	})
	if !strings.Contains(identityLimits, "top limitation: only") || strings.Contains(identityLimits, "more") {
		t.Fatalf("identity limitations: %q", identityLimits)
	}
}

func TestSummarizeDomainBranches(t *testing.T) {
	t.Parallel()

	for _, fn := range []func(map[string]any) string{
		summarizeServiceInvestigation, summarizeIncidentContext, summarizeCitationPacket,
		summarizeIndexStatus, summarizeHostedReadiness, summarizeIngesterStatus,
		summarizeIngesterList, summarizeCollectorList, summarizeCodeRelationships,
	} {
		if fn(nil) != "" {
			t.Fatal("nil must summarize empty")
		}
	}
	if got := summarizeServiceInvestigation(map[string]any{
		"service_name": "s", "investigation_findings": []any{1, 2},
		"coverage_summary":       map[string]any{"truncated": true, "state": "partial"},
		"recommended_next_calls": []any{map[string]any{"tool": "call_tool"}},
	}); !strings.Contains(got, "2 finding(s)") || !strings.Contains(got, "truncated") || !strings.Contains(got, "next: call_tool") {
		t.Fatalf("investigation = %q", got)
	}
	if got := summarizeIncidentContext(map[string]any{
		"incident":           map[string]any{"title": "t"},
		"related_changes":    []any{1},
		"missing_evidence":   []any{1},
		"ambiguous_evidence": []any{1, 2},
		"truncated":          true,
	}); !strings.Contains(got, "1 related change(s)") || !strings.Contains(got, "ambiguous 2") || !strings.Contains(got, "truncated") {
		t.Fatalf("incident = %q", got)
	}
	if got := summarizeCitationPacket(map[string]any{
		"coverage": map[string]any{"resolved_count": 3, "input_handle_count": 4, "missing_count": 1, "truncated": true},
	}); !strings.Contains(got, "resolved 3/requested 4") || !strings.Contains(got, "truncated") {
		t.Fatalf("citation = %q", got)
	}
	if summarizeCitationPacket(map[string]any{}) != "" {
		t.Fatal("citation without coverage must be empty")
	}
	if got := summarizeIndexStatus(map[string]any{"status": "ok", "reasons": []any{"a", "b"}}); !strings.Contains(got, "index status: ok") || !strings.Contains(got, "(+1 more)") {
		t.Fatalf("index = %q", got)
	}
	if got := summarizeIndexStatus(map[string]any{"scoped": true, "repository_count": 1}); !strings.Contains(got, "1 repository in your grant") {
		t.Fatalf("scoped = %q", got)
	}
	if summarizeIndexStatus(map[string]any{}) != "" {
		t.Fatal("empty index must be empty")
	}
	if got := summarizeHostedReadiness(map[string]any{"state": "blocked", "failure_classes": []any{"net", "auth"}}); !strings.Contains(got, "blocked") || !strings.Contains(got, "(+1 more)") {
		t.Fatalf("readiness = %q", got)
	}
	if summarizeHostedReadiness(map[string]any{}) != "" {
		t.Fatal("empty readiness must be empty")
	}
	if got := summarizeIngesterStatus(map[string]any{"ingester": "fs", "health": map[string]any{"reasons": []any{"r1", "r2"}}}); !strings.Contains(got, "ingester fs: unknown") || !strings.Contains(got, "(+1 more)") {
		t.Fatalf("ingester = %q", got)
	}
	if summarizeIngesterStatus(map[string]any{}) != "" {
		t.Fatal("nameless ingester must be empty")
	}
	if got := summarizeIngesterList(map[string]any{"ingesters": []any{1}}); got != "1 ingester(s) registered" {
		t.Fatalf("ingesters = %q", got)
	}
	if summarizeIngesterList(map[string]any{}) != "" || summarizeCollectorList(map[string]any{}) != "" {
		t.Fatal("missing lists must be empty")
	}
	if got := summarizeCollectorList(map[string]any{"collectors": []any{1, 2}}); got != "2 collector(s) registered" {
		t.Fatalf("collectors = %q", got)
	}
	if got := summarizeCodeRelationships(map[string]any{"relationships": []any{
		map[string]any{"provenance": map[string]any{"truth_state": "derived"}},
		map[string]any{"provenance": map[string]any{"truth_state": "heuristic"}},
		map[string]any{"provenance": map[string]any{}},
		"junk",
	}}); !strings.Contains(got, "4 relationship(s): 1 derived, 1 heuristic/ambiguous, 2 unsupported") {
		t.Fatalf("relationships = %q", got)
	}
	if summarizeCodeRelationships(map[string]any{}) != "" {
		t.Fatal("missing relationships must be empty")
	}
}
