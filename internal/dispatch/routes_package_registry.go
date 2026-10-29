package dispatch

import (
	packageregistrytools "github.com/eshu-hq/eshu-mcp/internal/dispatch/package/registry"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// packageRegistryRoute adapts the child package's package-registry request into
// the root dispatcher's transport route.
func packageRegistryRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(packageregistrytools.Route(toolName, routecontract.Arguments(args)))
}
