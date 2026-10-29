package dispatch

import (
	kubernetestools "github.com/eshu-hq/eshu-mcp/internal/dispatch/kubernetes"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// kubernetesCorrelationsRoute adapts the child package's Kubernetes-correlation
// request selection into the root dispatcher's transport route.
func kubernetesCorrelationsRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(kubernetestools.Route(toolName, routecontract.Arguments(args)))
}
