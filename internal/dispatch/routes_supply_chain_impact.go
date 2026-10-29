package dispatch

import (
	supplychainimpacttools "github.com/eshu-hq/eshu-mcp/internal/dispatch/supply/chain/impact"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// supplyChainImpactRoute adapts the child package's supply-chain-impact
// request selection into the root dispatcher's transport route.
func supplyChainImpactRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(supplychainimpacttools.Route(toolName, routecontract.Arguments(args)))
}
