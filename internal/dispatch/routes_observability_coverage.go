package dispatch

import (
	observabilitycoveragetools "github.com/eshu-hq/eshu-mcp/internal/dispatch/observability/coverage"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// observabilityCoverageRoute adapts the child package's observability-coverage
// request into the root dispatcher's transport route.
func observabilityCoverageRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(observabilitycoveragetools.Route(toolName, routecontract.Arguments(args)))
}
