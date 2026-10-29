package dispatch

// Ported from go/internal/mcp/dispatch_values.go, dispatch_args.go at
// 9922b49 with one adaptation: required-argument lookup reads the versioned
// tool definitions in the root registry package instead of the in-tree
// ReadOnlyTools catalog, so the refusal text stays byte-identical while the
// source of names moves to the contract. See docs/parity.md.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

func str(args map[string]any, key string) string {
	value, ok := args[key].(string)
	if !ok {
		return ""
	}
	return value
}

func intOr(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func intString(args map[string]any, key string, def int) string {
	return strconv.Itoa(intOr(args, key, def))
}

func boolOr(args map[string]any, key string, def bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return def
	}
	return v
}

func stringSlice(args map[string]any, key string) []any {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if ok {
		return values
	}
	stringValues, ok := raw.([]string)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(stringValues))
	for _, value := range stringValues {
		result = append(result, value)
	}
	return result
}

func objectSlice(args map[string]any, key string) []any {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(values))
	for _, value := range values {
		if _, ok := value.(map[string]any); ok {
			result = append(result, value)
		}
	}
	return result
}

func paginationQuery(args map[string]any, defaultLimit int) map[string]string {
	return map[string]string{
		"limit":  strconv.Itoa(intOr(args, "limit", defaultLimit)),
		"offset": strconv.Itoa(intOr(args, "offset", 0)),
	}
}

// dispatchStatusSucceeded reports whether an internal handler status counts
// as an answered tool call: only 2xx does. A 3xx is never success.
func dispatchStatusSucceeded(status int) bool {
	return status >= 200 && status < 300
}

// adaptChildRoute lifts a family-package Route result into the pointer form
// the selectRoute chain returns.
func adaptChildRoute(request routecontract.Request, handled bool) (*routecontract.Request, bool) {
	if !handled {
		return nil, false
	}
	return &request, true
}

// rejectEmptyPathSegment refuses a resolved route whose path has an empty
// segment ("//"). That shape comes from a required id argument left empty in
// a path template; the mux would answer it with a 307 redirect instead of
// running the handler. The error names the tool's required arguments that
// are empty, in the same "<field> is required" form as the other
// route-level missing-argument errors.
func rejectEmptyPathSegment(toolName string, args map[string]any, route *routecontract.Request) error {
	if route == nil || !strings.Contains(route.Path, "//") {
		return nil
	}
	missing := emptyRequiredArguments(toolName, args)
	if len(missing) == 0 {
		return fmt.Errorf("%s: a required path argument is empty", toolName)
	}
	return fmt.Errorf("%s is required", strings.Join(missing, ", "))
}

// emptyRequiredArguments returns the schema-required string arguments of a
// tool that are absent or empty in args, in schema order.
func emptyRequiredArguments(toolName string, args map[string]any) []string {
	var missing []string
	for _, name := range registry.RequiredArguments(toolName) {
		if str(args, name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}
