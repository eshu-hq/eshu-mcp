package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/dispatch"
	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
)

// mustMap requires a decoded JSON value to be an object. A mismatch is a
// test failure: the server under test always answers objects here.
func mustMap(t *testing.T, value any) map[string]any {
	t.Helper()
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("decoded value = %T, want map[string]any", value)
	}
	return typed
}

// mustSlice requires a decoded JSON value to be an array.
func mustSlice(t *testing.T, value any) []any {
	t.Helper()
	typed, ok := value.([]any)
	if !ok {
		t.Fatalf("decoded value = %T, want []any", value)
	}
	return typed
}

// mustJSON marshals a decoded value for byte comparison.
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	return raw
}

// stubCore answers read-contract routes with a canned envelope and records
// the request it received.
type stubCore struct {
	t          *testing.T
	status     int
	body       string
	seenAuth   string
	seenAccept string
}

func (s *stubCore) handler(w http.ResponseWriter, r *http.Request) {
	s.seenAuth = r.Header.Get("Authorization")
	s.seenAccept = r.Header.Get("Accept")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	if _, err := io.WriteString(w, s.body); err != nil {
		s.t.Errorf("write stub response: %v", err)
	}
}

func testServer(core *stubCore, tools []registry.ToolDefinition, opts ...ServerOption) (*Server, func()) {
	coreSrv := httptest.NewServer(http.HandlerFunc(core.handler))
	client, err := readcontract.NewClient(readcontract.DefaultConfig(coreSrv.URL, ""))
	if err != nil {
		// Unreachable: httptest always serves a non-empty URL, which is
		// the only input NewClient rejects.
		panic(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatchFunc := func(ctx context.Context, name string, args map[string]any, authHeader string) (any, error) {
		result, err := dispatch.Dispatch(ctx, client, name, args, authHeader, quiet, dispatch.DefaultOptions())
		if err != nil {
			return nil, err
		}
		return dispatch.RenderToolResult(name, result), nil
	}
	mcp := NewServer(tools, dispatchFunc, quiet, "eshu-mcp-test", "test",
		append([]ServerOption{WithTransportAuth(EdgeAuthMiddleware("", quiet))}, opts...)...)
	return mcp, coreSrv.Close
}

func postMessage(t *testing.T, mux http.Handler, body, auth string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	resp := rec.Result()
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			return
		}
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return resp.StatusCode, decoded
}

// TestTransportRefusesHeaderless proves GET /sse and POST /mcp/message
// without a credential are refused with the core-identical 401.
func TestTransportRefusesHeaderless(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()
	mux := mcp.Handler(nil)

	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /sse without credential = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want the bare Bearer challenge", got)
	}
	var denied map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &denied); err != nil {
		t.Fatalf("decode 401 body: %v", err)
	}
	if denied["error_code"] != "unauthenticated" || denied["message"] != "authentication is required" {
		t.Errorf("401 body = %v, want the unauthenticated envelope", denied)
	}
	if denied["correlation_id"] == "" {
		t.Error("401 body lacks a correlation_id")
	}

	status, decoded := postMessage(t, mux, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
	if status != http.StatusUnauthorized {
		t.Errorf("POST /mcp/message without credential = %d, want 401", status)
	}
	if decoded["error_code"] != "unauthenticated" {
		t.Errorf("POST 401 body = %v, want the unauthenticated envelope", decoded)
	}
}

// TestTransportEnvelope401 proves a caller asking for the envelope gets the
// envelope-shaped 401 with a null data/truth pair.
func TestTransportEnvelope401(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Accept", "application/eshu.envelope+json")
	rec := httptest.NewRecorder()
	mcp.Handler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode envelope 401: %v", err)
	}
	if decoded["data"] != nil {
		t.Errorf("envelope 401 data = %v, want null", decoded["data"])
	}
	envelope := mustMap(t, decoded["error"])
	if envelope["code"] != "unauthenticated" {
		t.Errorf("envelope 401 error = %v, want the unauthenticated code", decoded["error"])
	}
}

// TestToolsListServesRegistry proves an authenticated tools/list serves the
// full 169-tool registry in contract order with descriptions and schemas.
func TestToolsListServesRegistry(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "Bearer caller-key")
	result := mustMap(t, decoded["result"])
	tools := mustSlice(t, result["tools"])
	if len(tools) != 169 {
		t.Fatalf("tools/list serves %d tools, want the 169-tool registry", len(tools))
	}
	first := mustMap(t, tools[0])
	last := mustMap(t, tools[168])
	if first["name"] != "find_code" || last["name"] != "search_dockerfile_installs" {
		t.Errorf("tools/list boundaries = %v/%v, want the registry order", first["name"], last["name"])
	}
	schema := mustMap(t, first["inputSchema"])
	if schema["type"] != "object" {
		t.Errorf("tools/list first schema = %v, want the object schema", schema)
	}

	// The served payload normalizes to the contract definitions: names,
	// order, descriptions, and schemas identical.
	servedRaw, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal served tools: %v", err)
	}
	contractRaw, err := json.Marshal(registry.Definitions())
	if err != nil {
		t.Fatalf("marshal registry definitions: %v", err)
	}
	var servedNorm, contractNorm any
	if err := json.Unmarshal(servedRaw, &servedNorm); err != nil {
		t.Fatalf("reparse served tools: %v", err)
	}
	if err := json.Unmarshal(contractRaw, &contractNorm); err != nil {
		t.Fatalf("reparse registry definitions: %v", err)
	}
	servedCanon := mustJSON(t, servedNorm)
	contractCanon := mustJSON(t, contractNorm)
	if string(servedCanon) != string(contractCanon) {
		t.Error("served tools/list differs from the registry definitions")
	}
}

// TestToolsCallRoundTrip proves a tools/call dispatches through the HTTP
// boundary and renders the core envelope with its summary.
func TestToolsCallRoundTrip(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{"data":{"count":2},"truth":null,"error":null}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"count_repositories_by_language","arguments":{"language":"go"}}}`,
		"Bearer caller-key")
	result := mustMap(t, decoded["result"])
	if result["isError"] == true {
		t.Errorf("tools/call isError = true, want the successful envelope: %v", result)
	}
	structured := mustMap(t, result["structuredContent"])
	data := mustMap(t, structured["data"])
	if data["count"] != float64(2) {
		t.Errorf("tools/call data = %v, want the core payload", structured["data"])
	}
	if core.seenAuth != "Bearer caller-key" {
		t.Errorf("core saw Authorization %q, want the caller credential verbatim", core.seenAuth)
	}
	if core.seenAccept != "application/eshu.envelope+json" {
		t.Errorf("core saw Accept %q, want the envelope MIME", core.seenAccept)
	}
}

// TestToolsCallCoreRefusalPassesThrough proves a core error envelope with a
// non-2xx status arrives as an error result carrying the same envelope.
func TestToolsCallCoreRefusalPassesThrough(t *testing.T) {
	t.Parallel()

	core := &stubCore{
		status: 403,
		body:   `{"data":null,"truth":null,"error":{"code":"not_permitted","message":"this credential cannot call search_file_content"}}`,
	}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_file_content","arguments":{"query":"x"}}}`,
		"Bearer scoped-key")
	result := mustMap(t, decoded["result"])
	if result["isError"] != true {
		t.Errorf("tools/call isError = %v, want true for the core refusal", result["isError"])
	}
	structured := mustMap(t, result["structuredContent"])
	envelope := mustMap(t, structured["error"])
	if envelope["code"] != "not_permitted" {
		t.Errorf("tools/call error = %v, want the core refusal envelope", structured["error"])
	}
}

// TestInitializeNegotiates proves the handshake answers the supported
// protocol version with this binary's identity.
func TestInitializeNegotiates(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`, "Bearer k")
	result := mustMap(t, decoded["result"])
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}
	info := mustMap(t, result["serverInfo"])
	if info["name"] != "eshu-mcp-test" {
		t.Errorf("serverInfo.name = %v, want this binary's identity", info["name"])
	}

	_, decoded = postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`, "Bearer k")
	result = mustMap(t, decoded["result"])
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unsupported version answer = %v, want the server version", result["protocolVersion"])
	}
}

// TestMessageErrors proves unknown methods and bad JSON get the historic
// JSON-RPC errors.
func TestMessageErrors(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	_, decoded := postMessage(t, mcp.Handler(nil),
		`{"jsonrpc":"2.0","id":1,"method":"tools/warp"}`, "Bearer k")
	failure := mustMap(t, decoded["error"])
	if failure["code"] != float64(-32601) {
		t.Errorf("unknown method error = %v, want -32601", decoded["error"])
	}

	_, decoded = postMessage(t, mcp.Handler(nil), `{oops`, "Bearer k")
	failure = mustMap(t, decoded["error"])
	if failure["code"] != float64(-32700) {
		t.Errorf("parse error = %v, want -32700", decoded["error"])
	}
}

// TestRouterSurfaces proves the surface parameter selects the advertised
// set, and an unknown surface is a 400 naming the allowed values.
func TestRouterSurfaces(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()
	mux := mcp.Handler(nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp/message?surface=router",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer k")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode router tools/list: %v", err)
	}
	result := mustMap(t, decoded["result"])
	tools := mustSlice(t, result["tools"])
	if len(tools) != 2 {
		t.Errorf("router surface serves %d tools, want [find_tool call_tool]", len(tools))
	}

	_, decoded = postMessage(t, mux,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "Bearer k")
	_ = decoded
	bad := httptest.NewRequest(http.MethodPost, "/mcp/message?surface=bogus",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	bad.Header.Set("Authorization", "Bearer k")
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Errorf("bad surface status = %d, want 400", badRec.Code)
	}
	var badDecoded map[string]any
	if err := json.Unmarshal(badRec.Body.Bytes(), &badDecoded); err != nil {
		t.Fatalf("decode bad surface error: %v", err)
	}
	failure := mustMap(t, badDecoded["error"])
	if failure["code"] != float64(-32602) {
		t.Errorf("bad surface error = %v, want -32602", badDecoded["error"])
	}
}

// TestRouterToolsThroughMessage proves find_tool answers ranker-unavailable
// and call_tool validates through the message endpoint.
func TestRouterToolsThroughMessage(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{"data":{},"truth":null,"error":null}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()
	mux := mcp.Handler(nil)

	_, decoded := postMessage(t, mux,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_tool","arguments":{"intent":"find dead code"}}}`,
		"Bearer k")
	result := mustMap(t, decoded["result"])
	body := mustJSON(t, result["structuredContent"])
	if !strings.Contains(string(body), "mcp_router_ranker_unavailable") {
		t.Errorf("find_tool result = %s, want the ranker-unavailable code", body)
	}

	_, decoded = postMessage(t, mux,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call_tool","arguments":{"name":"count_repositories_by_language","arguments":{}}}}`,
		"Bearer k")
	result = mustMap(t, decoded["result"])
	body = mustJSON(t, result["structuredContent"])
	if !strings.Contains(string(body), "mcp_router_invalid_arguments") {
		t.Errorf("call_tool result = %s, want the invalid-arguments code", body)
	}
}

// TestHealthProbe proves the k8s liveness probe answers without a credential.
func TestHealthProbe(t *testing.T) {
	t.Parallel()

	core := &stubCore{status: 200, body: `{}`}
	mcp, cleanup := testServer(core, registry.Definitions())
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mcp.Handler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Errorf("GET /health body = %q, want the probe payload", rec.Body.String())
	}
}
