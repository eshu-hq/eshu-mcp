package dispatch

import (
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

func queryPlaybookRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	switch toolName {
	case "list_query_playbooks":
		query := map[string]string{
			"limit":  intString(args, "limit", 20),
			"offset": intString(args, "offset", 0),
		}
		if view := str(args, "view"); view != "" {
			query["view"] = view
		}
		return &routecontract.Request{Method: "GET", Path: "/api/v0/query-playbooks", Query: query}, true
	case "resolve_query_playbook":
		return &routecontract.Request{
			Method: "POST",
			Path:   "/api/v0/query-playbooks/resolve",
			Body: map[string]any{
				"playbook_id": str(args, "playbook_id"),
				"inputs":      mapStringAny(args, "inputs"),
			},
		}, true
	default:
		return nil, false
	}
}

func mapStringAny(args map[string]any, key string) map[string]any {
	value, ok := args[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return value
}
