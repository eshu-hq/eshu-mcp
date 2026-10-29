package dispatch

import (
	admissiondecisionstools "github.com/eshu-hq/eshu-mcp/internal/dispatch/admission/decisions"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// admissionDecisionsRoute adapts the child package's admission-decisions
// request selection into the root dispatcher's transport route.
func admissionDecisionsRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(admissiondecisionstools.Route(toolName, routecontract.Arguments(args)))
}
