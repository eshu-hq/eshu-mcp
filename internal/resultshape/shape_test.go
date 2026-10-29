package resultshape

import (
	"testing"
)

// TestParseShapes proves the closed wire layouts parse case-insensitively
// with whitespace trimmed, and anything else is rejected naming the allowed
// values.
func TestParseShapes(t *testing.T) {
	t.Parallel()

	if got, err := Parse("dual"); err != nil || got != Dual {
		t.Errorf("Parse(dual) = (%q, %v), want (dual, nil)", got, err)
	}
	if got, err := Parse("structured"); err != nil || got != Structured {
		t.Errorf("Parse(structured) = (%q, %v), want (structured, nil)", got, err)
	}
	if got, err := Parse("STRUCTURED"); err != nil || got != Structured {
		t.Errorf("Parse(STRUCTURED) = (%q, %v), want (structured, nil): matching is case-insensitive", got, err)
	}
	if _, err := Parse(" structured "); err == nil {
		t.Error("Parse(padded) succeeded, want rejection: callers trim first")
	}
	if _, err := Parse("triple"); err == nil {
		t.Error("Parse(triple) succeeded, want the allowed-values error")
	}
	if got := OrDual(Mode("")); got != Dual {
		t.Errorf("OrDual(empty) = %q, want dual", got)
	}
	if got := OrDual(Structured); got != Structured {
		t.Errorf("OrDual(structured) = %q, want structured", got)
	}
}

// TestContextOverride proves the request override rides the context and
// FromContext reports its absence.
func TestContextOverride(t *testing.T) {
	t.Parallel()

	ctx := WithOverride(t.Context(), Structured)
	got, ok := FromContext(ctx)
	if !ok || got != Structured {
		t.Errorf("FromContext = (%q, %v), want (structured, true)", got, ok)
	}
	if _, ok := FromContext(t.Context()); ok {
		t.Error("FromContext(empty) ok = true, want false")
	}
}
