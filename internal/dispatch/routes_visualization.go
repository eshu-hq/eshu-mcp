package dispatch

import (
	visualizationtools "github.com/eshu-hq/eshu-mcp/internal/dispatch/visualization"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// visualizationRoute adapts the child package's visualization request into
// the root dispatcher's transport route.
func visualizationRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(visualizationtools.Route(toolName, routecontract.Arguments(args)))
}
