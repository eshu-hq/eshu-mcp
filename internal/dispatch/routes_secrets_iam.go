package dispatch

import (
	secretsiamtools "github.com/eshu-hq/eshu-mcp/internal/dispatch/access/posture"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// secretsIAMRoute adapts the child package's secrets/IAM posture request into
// the root dispatcher's transport route.
func secretsIAMRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(secretsiamtools.Route(toolName, routecontract.Arguments(args)))
}
