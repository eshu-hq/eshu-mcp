package dispatch

import (
	cicdtools "github.com/eshu-hq/eshu-mcp/internal/dispatch/cicd"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// cicdRoute adapts the child package's CI/CD run-correlation request into the
// root dispatcher's transport route.
func cicdRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(cicdtools.Route(toolName, routecontract.Arguments(args)))
}
