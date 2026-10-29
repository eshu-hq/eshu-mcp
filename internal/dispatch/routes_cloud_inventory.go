package dispatch

import (
	"strconv"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// cloudInventoryRoute maps the canonical cloud inventory readback tool to its
// internal GET route. It forwards only the bounded, non-sensitive filter
// parameters; the handler validates provider and management_origin against
// closed sets and gates the capability by runtime profile.
func cloudInventoryRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	if toolName != "list_cloud_resource_inventory" {
		return nil, false
	}
	return &routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/cloud/inventory",
		Query:  cloudInventoryQuery(args),
	}, true
}

func cloudInventoryQuery(args map[string]any) map[string]string {
	query := map[string]string{}
	for _, key := range []string{
		"provider",
		"scope_id",
		"account_id",
		"project_id",
		"subscription_id",
		"management_origin",
		"cursor",
	} {
		if value := str(args, key); value != "" {
			query[key] = value
		}
	}
	if limit := intOr(args, "limit", 50); limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}
	return query
}
