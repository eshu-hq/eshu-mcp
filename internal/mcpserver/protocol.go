package mcpserver

// JSON-RPC and MCP wire types plus initialize version negotiation, ported
// from go/internal/mcp/server.go and protocol_version.go at 9922b49.
// Supported versions, negotiation, and truncation bound are unchanged. The
// serverInfo name and version are the destination's own: this is an
// independently released binary, so initialize identifies it honestly rather
// than echoing core's build. See docs/parity.md.

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	registry "github.com/eshu-hq/eshu-mcp"
)

// JSON-RPC 2.0 message types.

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCP-specific types.

type mcpInitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    mcpCapabilities `json:"capabilities"`
	ServerInfo      mcpServerInfo   `json:"serverInfo"`
}

type mcpCapabilities struct {
	Tools *mcpToolsCap `json:"tools,omitempty"`
}

type mcpToolsCap struct {
	ListChanged bool `json:"listChanged"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpToolsListResult struct {
	Tools []registry.ToolDefinition `json:"tools"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// supportedProtocolVersions lists the MCP protocol revisions this server
// implements, oldest first. The last entry is the server's latest and is the
// answer for a request that names no supported version.
//
// The list holds only 2024-11-05. Later revisions changed the wire contract
// in ways this server does not implement: 2025-03-26 replaced the HTTP+SSE
// transport with Streamable HTTP and added JSON-RPC batching, and 2025-06-18
// removed batching and requires an MCP-Protocol-Version header on HTTP
// requests. This server serves HTTP+SSE, decodes one JSON-RPC object per
// POST, and reads no version header. The extra structuredContent field on tool
// results is additive and does not make the server a 2025-06-18 server.
// Add a version here only with the transport behavior that revision requires.
var supportedProtocolVersions = []string{"2024-11-05"}

// negotiateProtocolVersion answers the initialize version negotiation: a
// supported requested version is echoed back unchanged, and any other
// request, including an empty one, gets the latest version in supported. An
// empty supported list answers with the empty string instead of panicking.
// The match is exact.
func negotiateProtocolVersion(requested string, supported []string) string {
	if len(supported) == 0 {
		return ""
	}
	if slices.Contains(supported, requested) {
		return requested
	}
	return supported[len(supported)-1]
}

// requestedProtocolVersion reads params.protocolVersion from an initialize
// request. A missing, malformed, or non-string value reads as empty, so the
// caller negotiates the server's own version instead of rejecting the
// handshake.
func requestedProtocolVersion(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	return p.ProtocolVersion
}

// maxLoggedVersionBytes bounds a client-supplied version string in a log line.
const maxLoggedVersionBytes = 64

// truncateForLog bounds a client-supplied string before it reaches a log line,
// so a hostile initialize cannot write an unbounded value. The cut backs up to
// a rune boundary, so the logged value stays valid UTF-8.
func truncateForLog(s string) string {
	if len(s) <= maxLoggedVersionBytes {
		return s
	}
	cut := maxLoggedVersionBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// IsProductionProfile reports whether raw names the production query
// profile. Profile names match case-sensitively after trimming, mirroring
// querycontract.ParseQueryProfile; anything else keeps the full surface.
func IsProductionProfile(raw string) bool {
	return strings.TrimSpace(raw) == "production"
}
