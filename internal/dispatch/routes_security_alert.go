package dispatch

import (
	alerttools "github.com/eshu-hq/eshu-mcp/internal/dispatch/alerts"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// securityAlertRoute adapts the child package's security-alert
// reconciliation request selection into the root dispatcher's transport
// route.
func securityAlertRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(alerttools.Route(toolName, routecontract.Arguments(args)))
}
