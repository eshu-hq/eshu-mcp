package routecontract

// MissingSelectorError is a route-builder refusal for a call that names none
// of the selector arguments the route needs. Sites keep their historic
// refusal text in Message so existing refusal-text contracts do not move;
// Fields names the selector arguments, in the precedence order the route
// checks them, so the dispatch renderer can answer the structured
// mcp_router_invalid_arguments error telling the caller what to fix
// (eshu-hq/eshu-mcp#73). It lives here, next to Arguments and Request,
// because family route builders (which the dispatcher imports) construct it
// while the dispatcher renders it — neither side can own the type without
// an import cycle.
type MissingSelectorError struct {
	// Tool is the requested MCP tool name.
	Tool string
	// Fields holds the selector arguments that would satisfy the route, in
	// precedence order. It is never empty: a refusal always names at least
	// one field.
	Fields []string
	// Message is the historic refusal text, kept byte-identical.
	Message string
}

// Error reports the historic refusal text.
func (e *MissingSelectorError) Error() string { return e.Message }
