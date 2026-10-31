package dispatch

import (
	"reflect"
	"testing"
)

func TestArgHelpers(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"flag":   true,
		"count":  3,
		"mixed":  []any{"a", "b"},
		"strs":   []string{"x", "y"},
		"objs":   []any{map[string]any{"k": 1}, "skip"},
		"scalar": 7,
	}
	if !boolOr(args, "flag", false) || boolOr(args, "missing", true) != true || boolOr(args, "scalar", true) != true {
		t.Fatal("boolOr wrong")
	}
	if got := stringSlice(args, "mixed"); !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Fatalf("stringSlice any = %#v", got)
	}
	if got := stringSlice(args, "strs"); !reflect.DeepEqual(got, []any{"x", "y"}) {
		t.Fatalf("stringSlice strings = %#v", got)
	}
	if stringSlice(args, "missing") != nil || stringSlice(args, "scalar") != nil {
		t.Fatal("stringSlice missing/scalar want nil")
	}
	if got := objectSlice(args, "objs"); !reflect.DeepEqual(got, []any{map[string]any{"k": 1}}) {
		t.Fatalf("objectSlice = %#v", got)
	}
	if objectSlice(args, "scalar") != nil || objectSlice(args, "missing") != nil {
		t.Fatal("objectSlice scalar/missing want nil")
	}
}

func TestMcpMetadataRows(t *testing.T) {
	t.Parallel()

	if mcpMetadataRows(nil, "k") != nil {
		t.Fatal("nil metadata want nil")
	}
	meta := map[string]any{
		"typed":   []map[string]any{{"reason": "r"}},
		"untyped": []any{map[string]any{"kind": "k"}, "skip"},
		"scalar":  "x",
	}
	if got := mcpMetadataRows(meta, "typed"); len(got) != 1 || got[0]["reason"] != "r" {
		t.Fatalf("typed = %#v", got)
	}
	if got := mcpMetadataRows(meta, "untyped"); len(got) != 1 || got[0]["kind"] != "k" {
		t.Fatalf("untyped = %#v", got)
	}
	if mcpMetadataRows(meta, "scalar") != nil || mcpMetadataRows(meta, "absent") != nil {
		t.Fatal("scalar/absent want nil")
	}
}

func TestMcpValueSliceLen(t *testing.T) {
	t.Parallel()

	if mcpValueSliceLen([]any{1, 2}) != 2 || mcpValueSliceLen([]map[string]any{{}}) != 1 || mcpValueSliceLen("x") != 0 {
		t.Fatal("mcpValueSliceLen wrong")
	}
}

func TestValueHelpersBranches(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"s":     "x",
		"num":   42,
		"f":     2.0,
		"i64":   int64(7),
		"b":     true,
		"strs":  []string{"a"},
		"mixed": []any{"a", 1},
		"nilv":  nil,
	}
	if stringVal(row, "s") != "x" || stringVal(row, "num") != "42" || stringVal(row, "missing") != "" || stringVal(row, "nilv") != "" {
		t.Fatal("stringVal wrong")
	}
	if !boolVal(row, "b") || boolVal(row, "num") || boolVal(row, "missing") {
		t.Fatal("boolVal wrong")
	}
	if intVal(row, "num") != 42 || intVal(row, "f") != 2 || intVal(row, "i64") != 7 || intVal(row, "s") != 0 || intVal(row, "missing") != 0 {
		t.Fatal("intVal wrong")
	}
	if got := stringSliceVal(row, "strs"); len(got) != 1 {
		t.Fatalf("stringSliceVal strs = %#v", got)
	}
	if got := stringSliceVal(row, "mixed"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("stringSliceVal mixed = %#v", got)
	}
	if stringSliceVal(row, "num") != nil || stringSliceVal(row, "missing") != nil {
		t.Fatal("stringSliceVal scalar/missing want nil")
	}
}
