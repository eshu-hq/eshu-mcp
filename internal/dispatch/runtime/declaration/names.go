package runtimedeclarationtools

// Ported from go/internal/mcp/runtime/declaration/tools.go at 9922b49:
// only the tool-name constant the route selector needs. The served
// definition (description and input schema) lives in the versioned
// contracts/tool-registry/v1/tool_definitions.json instead, so the JSON
// contract stays the single source of advertised truth. See docs/parity.md.

// ToolName is the MCP tool this family owns.
const ToolName = "list_runtime_version_declarations"
