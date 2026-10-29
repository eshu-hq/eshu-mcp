package dispatch

import (
	infrasearchtools "github.com/eshu-hq/eshu-mcp/internal/dispatch/infra/search"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// infraResourceSearchRoute adapts the child package's infrastructure-search
// request selection into the root dispatcher's transport route.
func infraResourceSearchRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(infrasearchtools.Route(toolName, routecontract.Arguments(args)))
}
