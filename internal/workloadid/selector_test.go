package workloadid

import (
	"testing"
)

// TestClassifyWorkloadSelector proves the handle ladder from the ported doc
// examples: full scoped ids, public handles, and everything else as bare.
func TestClassifyWorkloadSelector(t *testing.T) {
	t.Parallel()

	cases := map[string]SelectorKind{
		"workload:my-repo:my-workload": SelectorKindFullID,
		"workload:my-workload":         SelectorKindHandle,
		"":                             SelectorKindBare,
		"workload:":                    SelectorKindBare,
		"my-workload":                  SelectorKindBare,
		"repo:my-repo":                 SelectorKindBare,
		"  workload:spaced  ":          SelectorKindHandle,
	}
	for selector, want := range cases {
		if got := ClassifyWorkloadSelector(selector); got != want {
			t.Errorf("ClassifyWorkloadSelector(%q) = %q, want %q", selector, got, want)
		}
	}
}

// TestHandleRoundTrip proves strip and format invert each other on bare
// names and leave full ids' repository segment intact.
func TestHandleRoundTrip(t *testing.T) {
	t.Parallel()

	if got := StripWorkloadHandlePrefix("workload:api"); got != "api" {
		t.Errorf("Strip = %q, want api", got)
	}
	if got := FormatWorkloadHandle("api"); got != "workload:api" {
		t.Errorf("Format = %q, want workload:api", got)
	}
	if got := FormatWorkloadHandle("workload:api"); got != "workload:api" {
		t.Errorf("Format(handle) = %q, want the idempotent handle", got)
	}
}
