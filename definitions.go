package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed contracts/tool-registry/v1/tool_definitions.json
var definitionsJSON []byte

// ToolDefinition is one served tool: the name, description, and input
// schema tools/list advertises, verbatim from the source census.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type definitionsSourceBounds struct {
	ContentSearchMaxOffset int      `json:"content_search_max_offset"`
	EnvelopeMIME           string   `json:"envelope_mime"`
	ProtocolVersions       []string `json:"protocol_versions"`
}

type definitionsDoc struct {
	Source struct {
		Bounds definitionsSourceBounds `json:"bounds"`
	} `json:"source"`
	RegistryTools []ToolDefinition          `json:"registry_tools"`
	RouterTools   []ToolDefinition          `json:"router_tools"`
	Examples      map[string]map[string]any `json:"examples"`
	CoreRouter    []string                  `json:"core_router_names"`
	Hidden        []string                  `json:"production_hidden"`
}

var (
	definitionsOnce sync.Once
	definitions     definitionsDoc
	definitionsErr  error
)

func loadDefinitions() (definitionsDoc, error) {
	definitionsOnce.Do(func() {
		var decoded definitionsDoc
		if err := json.Unmarshal(definitionsJSON, &decoded); err != nil {
			definitionsErr = fmt.Errorf("registry: decode embedded tool_definitions.json: %w", err)
			return
		}
		definitions = decoded
	})
	return definitions, definitionsErr
}

// Definitions reports the served registry tools in contract registration
// order. The returned slice is a copy; mutating it cannot change the served
// registry.
func Definitions() []ToolDefinition {
	doc, err := loadDefinitions()
	if err != nil {
		return nil
	}
	out := make([]ToolDefinition, len(doc.RegistryTools))
	copy(out, doc.RegistryTools)
	return out
}

// Definition returns the served definition of the named registry tool.
func Definition(name string) (ToolDefinition, bool) {
	doc, err := loadDefinitions()
	if err != nil {
		return ToolDefinition{}, false
	}
	for _, tool := range doc.RegistryTools {
		if tool.Name == name {
			return tool, true
		}
	}
	return ToolDefinition{}, false
}

// RouterTools returns the find_tool and call_tool definitions tools/list
// serves on the router surfaces, in serve order.
func RouterTools() []ToolDefinition {
	doc, err := loadDefinitions()
	if err != nil {
		return nil
	}
	out := make([]ToolDefinition, len(doc.RouterTools))
	copy(out, doc.RouterTools)
	return out
}

// Example returns a copy of the curated example arguments handed back for a
// registered tool: the required-only ExampleArguments, plus the first
// optional property (in name order) that lets the route resolve when the
// tool needs one of several selectors its schema cannot mark required.
func Example(name string) (map[string]any, bool) {
	doc, err := loadDefinitions()
	if err != nil {
		return nil, false
	}
	example, ok := doc.Examples[name]
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(example))
	for key, value := range example {
		out[key] = value
	}
	return out, true
}

// RequiredArguments returns the schema-required argument names of a tool in
// schema order, for the empty-path-segment refusal.
func RequiredArguments(tool string) []string {
	definition, ok := Definition(tool)
	if !ok {
		return nil
	}
	schema, ok := definition.InputSchema.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, value := range raw {
		if name, ok := value.(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// CoreRouterNames returns the core high-traffic tool set the core_router
// surface advertises beside find_tool and call_tool, in serve order.
func CoreRouterNames() []string {
	doc, err := loadDefinitions()
	if err != nil {
		return nil
	}
	out := make([]string, len(doc.CoreRouter))
	copy(out, doc.CoreRouter)
	return out
}

// SourceBounds reports versioned source facts the dispatch bounds depend
// on: the content-search max offset for budget paging, the envelope MIME,
// and the negotiated MCP protocol versions.
func SourceBounds() (maxOffset int, mime string, protocols []string) {
	doc, err := loadDefinitions()
	if err != nil {
		return 0, "", nil
	}
	bounds := doc.Source.Bounds
	out := make([]string, len(bounds.ProtocolVersions))
	copy(out, bounds.ProtocolVersions)
	return bounds.ContentSearchMaxOffset, bounds.EnvelopeMIME, out
}

// ProductionHidden reports the tools the production query profile hides
// from tools/list. Hidden tools stay callable; the gate is advertisement.
func ProductionHidden() map[string]bool {
	doc, err := loadDefinitions()
	if err != nil {
		return nil
	}
	hidden := make(map[string]bool, len(doc.Hidden))
	for _, name := range doc.Hidden {
		hidden[name] = true
	}
	return hidden
}
