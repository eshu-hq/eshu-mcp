package mcpserver

// MCP draft-2024-11-05 HTTP+SSE transport serving the registry.
//
// GET /sse sessions are bounded per principal (sse_limit.go): a principal at
// its cap gets a 429 with Retry-After, and /healthz reports the aggregate.
