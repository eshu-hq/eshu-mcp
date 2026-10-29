package dispatch

// Local value helpers, ported from
// go/internal/query/querycontract/rowvalue/decode.go at 9922b49
// (StringVal, BoolVal, IntVal, StringSliceVal). The summaries only ever read
// JSON-decoded maps, so the driver shapes collapse to these four. Freshness
// fresh state matches querycontract.FreshnessFresh at that commit. See
// docs/parity.md.

import "fmt"

// freshnessFresh is the freshness state of an answer read from live backends.
const freshnessFresh = "fresh"

func stringVal(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func boolVal(row map[string]any, key string) bool {
	v, ok := row[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

func intVal(row map[string]any, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func stringSliceVal(row map[string]any, key string) []string {
	v, ok := row[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}
