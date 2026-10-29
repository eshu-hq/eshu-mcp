package dispatch

import (
	codeflowtools "github.com/eshu-hq/eshu-mcp/internal/dispatch/code/flow"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// codeFlowRoute adapts the child package's code-flow request selection into
// the root dispatcher's transport route. It occupies the same delegation
// position in resolveRoute that the family's own selector occupied before the
// extraction.
func codeFlowRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(codeflowtools.Route(toolName, routecontract.Arguments(args)))
}
