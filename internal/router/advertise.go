package router

import registry "github.com/eshu-hq/eshu-mcp"

// Advertise returns the tool definitions tools/list shows for surface.
//
// tools is the registry after any query-profile filtering, so a tool hidden by
// the profile stays hidden in every mode:
//
//   - SurfaceFull returns tools unchanged, the same slice, so the response is
//     byte-identical to a server with no surface option.
//   - SurfaceRouter returns Tools() only.
//   - SurfaceCoreRouter returns the CoreToolNames tools in registration order,
//     then Tools().
//
// The non-full results are fresh slices. A surface value ParseSurface would
// reject is treated as SurfaceFull; callers validate configuration first.
func Advertise(tools []registry.ToolDefinition, surface Surface) []registry.ToolDefinition {
	switch surface {
	case SurfaceRouter:
		return registry.RouterTools()
	case SurfaceCoreRouter:
		return advertiseCoreRouter(tools, registry.CoreRouterNames())
	default:
		return tools
	}
}

// advertiseCoreRouter keeps the tools named in core, in the order tools lists
// them, and appends the router pair. A core name that is not in tools (for
// example one the query profile hides) is skipped.
func advertiseCoreRouter(tools []registry.ToolDefinition, core []string) []registry.ToolDefinition {
	want := make(map[string]struct{}, len(core))
	for _, name := range core {
		want[name] = struct{}{}
	}
	pair := registry.RouterTools()
	out := make([]registry.ToolDefinition, 0, len(core)+len(pair))
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			out = append(out, tool)
		}
	}
	return append(out, pair...)
}
