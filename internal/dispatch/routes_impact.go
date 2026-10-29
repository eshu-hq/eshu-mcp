package dispatch

import (
	impacttools "github.com/eshu-hq/eshu-mcp/internal/dispatch/impact"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// impactRoute adapts the child package's impact-analysis request selection
// into the root dispatcher's transport route. It is consulted from
// resolveRoute's default case, the same point in the chain the family's own
// switch occupied before the extraction.
func impactRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(impacttools.Route(toolName, routecontract.Arguments(args)))
}
