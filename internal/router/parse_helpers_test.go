package router

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundEcho(t *testing.T) {
	t.Parallel()

	if got := boundEcho("short"); got != "short" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("b", MaxEchoBytes+10)
	got := boundEcho(long)
	if !strings.HasSuffix(got, "...") || len(got) > MaxEchoBytes+3 {
		t.Fatalf("long cut = %q", got)
	}
	wide := strings.Repeat("ü", MaxEchoBytes)
	if got := boundEcho(wide + "tail"); !utf8.ValidString(strings.TrimSuffix(got, "...")) {
		t.Fatalf("rune cut invalid: %q", got)
	}
}

func TestParseQuery(t *testing.T) {
	t.Parallel()

	q := parseQuery(nil)
	if q.Limit != DefaultLimit || q.Intent != "" {
		t.Fatalf("defaults = %+v", q)
	}
	q = parseQuery(map[string]any{
		"intent": "  find owners ",
		"limit":  7.0,
		"anchors": map[string]any{
			"repo_id":     "r1",
			"service_id":  42,
			"workload_id": "w1",
			"environment": "prod",
		},
	})
	if q.Intent != "find owners" || q.Limit != 7 {
		t.Fatalf("parsed = %+v", q)
	}
	if q.Anchors.RepoID != "r1" || q.Anchors.ServiceID != "" || q.Anchors.WorkloadID != "w1" || q.Anchors.Environment != "prod" {
		t.Fatalf("anchors = %+v", q.Anchors)
	}
}
