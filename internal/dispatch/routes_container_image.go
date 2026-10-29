package dispatch

import (
	containerimagetools "github.com/eshu-hq/eshu-mcp/internal/dispatch/container/image"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// containerImageRoute adapts the child package's container-image identity
// request into the root dispatcher's transport route.
func containerImageRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(containerimagetools.Route(toolName, routecontract.Arguments(args)))
}
