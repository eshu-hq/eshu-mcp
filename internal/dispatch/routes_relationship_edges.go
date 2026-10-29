package dispatch

import (
	relationshiptools "github.com/eshu-hq/eshu-mcp/internal/dispatch/relationships"
	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// relationshipEdgesRoute adapts the child package's relationship-edge request
// into the root dispatcher's transport route. The child validates source_tool
// against the canonical vocabulary before forwarding; an empty value leaves the
// filter unset.
func relationshipEdgesRoute(toolName string, args map[string]any) (*routecontract.Request, bool, error) {
	request, handled, err := relationshiptools.EdgeRoute(toolName, routecontract.Arguments(args))
	if !handled || err != nil {
		return nil, handled, err
	}
	adapted, handled := adaptChildRoute(request, handled)
	return adapted, handled, nil
}
