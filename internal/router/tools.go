package router

// Router tool names, limits, and interception, ported from
// go/internal/mcp/router/tools.go at 9922b49. The find_tool and call_tool
// definitions themselves live in the versioned tool_definitions.json
// contract (registry.RouterTools); the names, limits, and intercept rule
// stay code so request validation never depends on definition order.

const (
	// FindToolName is the MCP name of the tool-discovery tool.
	FindToolName = "find_tool"
	// CallToolName is the MCP name of the tool-invocation tool.
	CallToolName = "call_tool"
)

const (
	// DefaultLimit is the find_tool candidate count used when limit is omitted.
	DefaultLimit = 5
	// MaxLimit is the largest find_tool candidate count a caller may request.
	MaxLimit = 10
)

// Intercepts reports whether name is a router tool the MCP root must hand to
// Host.Serve instead of dispatch.
func Intercepts(name string) bool {
	return name == FindToolName || name == CallToolName
}
