package rank

import (
	"reflect"
	"testing"
)

func TestCloneValueBranches(t *testing.T) {
	t.Parallel()

	nested := map[string]any{"a": []any{map[string]any{"b": 1}}, "s": "x", "n": 3}
	got, ok := cloneValue(nested).(map[string]any)
	if !ok {
		t.Fatalf("type %T", cloneValue(nested))
	}
	if !reflect.DeepEqual(got, nested) {
		t.Fatalf("clone = %#v", got)
	}
	inner, ok := got["a"].([]any)
	if !ok || len(inner) == 0 {
		t.Fatalf("clone list = %#v", got["a"])
	}
	innerMap, ok := inner[0].(map[string]any)
	if !ok {
		t.Fatalf("clone item = %#v", inner[0])
	}
	innerMap["b"] = 2
	origInner, ok := nested["a"].([]any)
	if !ok || len(origInner) == 0 {
		t.Fatalf("orig list = %#v", nested["a"])
	}
	origMap, ok := origInner[0].(map[string]any)
	if !ok {
		t.Fatalf("orig item = %#v", origInner[0])
	}
	if origMap["b"] != 1 {
		t.Fatal("clone shares nested state")
	}
	if got := cloneValue([]string{"a"}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("strings = %#v", got)
	}
	if got := cloneValue([]int{1}); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("ints = %#v", got)
	}
	if got := cloneValue([]float64{1.5}); !reflect.DeepEqual(got, []float64{1.5}) {
		t.Fatalf("floats = %#v", got)
	}
	if got := cloneValue([]bool{true}); !reflect.DeepEqual(got, []bool{true}) {
		t.Fatalf("bools = %#v", got)
	}
	if got := cloneValue(map[string]string{"k": "v"}); !reflect.DeepEqual(got, map[string]string{"k": "v"}) {
		t.Fatalf("strmap = %#v", got)
	}
	if cloneValue(7) != 7 || cloneValue("s") != "s" {
		t.Fatal("scalars must pass through")
	}
}

func TestTypeNameBranches(t *testing.T) {
	t.Parallel()

	if typeName(map[string]any{"type": "string"}) != "string" {
		t.Fatal("string type")
	}
	if typeName(map[string]any{"type": []string{"null", "integer"}}) != "integer" {
		t.Fatal("string list skips null")
	}
	if typeName(map[string]any{"type": []any{"null", "boolean"}}) != "boolean" {
		t.Fatal("any list skips null")
	}
	if typeName(map[string]any{}) != "" || typeName(map[string]any{"type": []string{"null"}}) != "" {
		t.Fatal("missing/all-null want empty")
	}
}

func TestCompareScoredOrders(t *testing.T) {
	t.Parallel()

	mk := func(key int64, canonical bool, cost int, name string, pos int) scored {
		return scored{d: &doc{name: name, costRank: cost, canonical: canonical}, key: key, pos: pos}
	}
	if compareScored(mk(2, false, 0, "b", 0), mk(1, false, 0, "a", 0)) >= 0 {
		t.Fatal("higher key must sort first")
	}
	if compareScored(mk(1, true, 0, "b", 0), mk(1, false, 0, "a", 0)) >= 0 {
		t.Fatal("canonical must sort first")
	}
	if compareScored(mk(1, false, 0, "b", 0), mk(1, true, 0, "a", 0)) <= 0 {
		t.Fatal("non-canonical must sort last")
	}
	if compareScored(mk(1, false, 1, "a", 0), mk(1, false, 2, "a", 0)) >= 0 {
		t.Fatal("cheaper cost must sort first")
	}
	if compareScored(mk(1, false, 0, "b", 0), mk(1, false, 0, "a", 0)) <= 0 {
		t.Fatal("name tiebreak wrong")
	}
	if compareScored(mk(1, false, 0, "a", 1), mk(1, false, 0, "a", 0)) <= 0 {
		t.Fatal("position tiebreak wrong")
	}
	if compareScored(mk(1, false, 0, "a", 0), mk(1, false, 0, "a", 0)) != 0 {
		t.Fatal("identical must compare equal")
	}
}
