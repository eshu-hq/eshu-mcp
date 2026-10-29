package routecontract

import (
	"reflect"
	"testing"
)

// TestArgumentsCoerce proves every accessor coerces JSON-decoded values the
// way the route builders rely on.
func TestArgumentsCoerce(t *testing.T) {
	t.Parallel()

	args := Arguments(map[string]any{
		"name":     "api",
		"count":    float64(7),
		"missing":  nil,
		"flag":     true,
		"ratio":    float64(1.5),
		"badfloat": "x",
		"ids":      []any{"a", "b"},
		"strings":  []string{"c"},
		"scalar":   "solo",
		"empty":    "",
		"blank":    "   ",
	})
	if got := args.String("name"); got != "api" {
		t.Errorf("String = %q, want api", got)
	}
	if got := args.String("missing"); got != "" {
		t.Errorf("String(missing) = %q, want empty", got)
	}
	if got := args.String("empty"); got != "" {
		t.Errorf("String(empty) = %q, want empty", got)
	}
	if got := args.IntOr("count", 3); got != 7 {
		t.Errorf("IntOr = %d, want 7", got)
	}
	if got := args.IntOr("missing", 3); got != 3 {
		t.Errorf("IntOr(missing) = %d, want the fallback", got)
	}
	if got := args.BoolOr("flag", false); !got {
		t.Error("BoolOr = false, want true")
	}
	if got, ok := args.OptionalFloat("ratio"); !ok || got != 1.5 {
		t.Errorf("OptionalFloat = (%v, %v), want (1.5, true)", got, ok)
	}
	if _, ok := args.OptionalFloat("badfloat"); ok {
		t.Error("OptionalFloat(string) ok = true, want false")
	}
	if got := args.StringSlice("ids"); !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("StringSlice = %v", got)
	}
	if got := args.StringSlice("strings"); !reflect.DeepEqual(got, []any{"c"}) {
		t.Errorf("StringSlice([]string) = %v", got)
	}
	if got := args.StringSlice("scalar"); got != nil {
		t.Errorf("StringSlice(scalar) = %v, want nil: the method does not wrap scalars", got)
	}
	if got := args.StringSlice("missing"); got != nil {
		t.Errorf("StringSlice(missing) = %v, want nil", got)
	}
}

// TestStringRejectsWrongType proves a present-but-mistyped argument reads as
// empty: route builders treat that the same as absent.
func TestStringRejectsWrongType(t *testing.T) {
	t.Parallel()

	args := Arguments(map[string]any{"count": float64(7)})
	if got := args.String("count"); got != "" {
		t.Errorf("String(count) = %q, want empty", got)
	}
}
