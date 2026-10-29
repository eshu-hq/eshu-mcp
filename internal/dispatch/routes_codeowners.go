package dispatch

import (
	codeownerstools "github.com/eshu-hq/eshu-mcp/internal/dispatch/code/owners"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// codeownersRoute adapts the child package's CODEOWNERS ownership request into
// the root dispatcher's transport route.
func codeownersRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(codeownerstools.Route(toolName, routecontract.Arguments(args)))
}
