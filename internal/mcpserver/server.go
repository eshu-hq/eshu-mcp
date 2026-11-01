package mcpserver

// MCP message server, adapted from go/internal/mcp/server.go at 9922b49.
// The JSON-RPC framing, method set, initialize negotiation, tools/list
// advertisement, tools/call interception, ping, and unknown-method errors are
// unchanged. The dispatch handler calls core over HTTP read contracts
// instead of an in-process query mux, so the server holds no stores. The
// query-profile filter keeps the production hide set from the versioned
// contract. Telemetry lines are slog; the Prometheus handler, pprof, OTEL
// providers, governance audit, and stdio transport stay in core. See
// docs/parity.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/dispatch"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// Dispatcher runs one registered tool through the HTTP dispatch boundary and
// returns the rendered MCP tool result, converting dispatch errors to
// rendered results itself so the error return stays for transport failures.
type Dispatcher func(ctx context.Context, name string, args map[string]any, authHeader string) (any, error)

// Server is the MCP server that dispatches tool calls to core over HTTP.
type Server struct {
	tools   []registry.ToolDefinition
	surface router.Surface
	shape   resultshape.Mode
	name    string
	version string
	logger  *slog.Logger

	dispatch Dispatcher

	sessMu   sync.RWMutex
	sessions map[string]*sseSession
	// sseLimit counts live sessions per principal under sessMu; see
	// sse_limit.go. sseMaxPerPrincipal is the configured cap (0 = default).
	sseLimit           sseLimiter
	sseMaxPerPrincipal int

	// transportAuth wraps GET /sse and POST /mcp/message with the edge
	// credential check. Nil leaves the transport unauthenticated, which the
	// command refuses to boot without the explicit escape hatch.
	transportAuth func(http.Handler) http.Handler

	router router.Host
}

// NewServer creates an MCP server over the given Dispatcher. tools is the
// served registry before surface selection; pass the profile-filtered list.
// name and version identify this build in initialize.
func NewServer(tools []registry.ToolDefinition, dispatch Dispatcher, logger *slog.Logger, name, version string, opts ...ServerOption) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		tools:    tools,
		name:     name,
		version:  version,
		logger:   logger,
		dispatch: dispatch,
		sessions: make(map[string]*sseSession),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.router.Prepare(s.tools, s.logger)
	return s
}

// ServerOption configures optional Server behavior.
type ServerOption func(*Server)

// WithTransportAuth authenticates the MCP HTTP transport itself — GET /sse
// session establishment and every POST /mcp/message method — with middleware,
// so a credential that can call a tool can also establish a session and list
// the catalog, and a credential that cannot is refused uniformly. A nil
// middleware (the default) leaves the transport unauthenticated.
func WithTransportAuth(middleware func(http.Handler) http.Handler) ServerOption {
	return func(s *Server) {
		s.transportAuth = middleware
	}
}

// WithRouterRanker sets the builder for the find_tool ranker. Without this
// option find_tool answers mcp_router_ranker_unavailable. The command wires
// the catalog-free lexical ranker, so the router and core_router surfaces
// boot with find_tool ranking.
func WithRouterRanker(build router.RankerBuilder) ServerOption {
	return func(s *Server) {
		s.router.NewRanker = build
	}
}

// WithToolSurface sets the tool surface tools/list advertises. It changes
// advertisement only: tools/call dispatches every registered tool whatever
// the surface.
func WithToolSurface(surface router.Surface) ServerOption {
	return func(s *Server) {
		s.surface = surface
	}
}

// WithResultShape sets the process default tools/call result shape.
func WithResultShape(mode resultshape.Mode) ServerOption {
	return func(s *Server) {
		s.shape = resultshape.OrDual(mode)
	}
}

// processShape returns the shape set by WithResultShape, or dual when the
// option is absent.
func (s *Server) processShape() resultshape.Mode {
	return resultshape.OrDual(s.shape)
}

// processSurface returns the surface set by WithToolSurface, or full when
// the option is absent.
func (s *Server) processSurface() router.Surface {
	if s.surface == "" {
		return router.SurfaceFull
	}
	return s.surface
}

// advertisedTools returns the tools/list payload for this request: the
// override when ctx carries one, otherwise the process surface. The registry
// s.tools is never mutated.
func (s *Server) advertisedTools(ctx context.Context) []registry.ToolDefinition {
	surface := s.processSurface()
	if override, ok := ctx.Value(surfaceContextKey{}).(router.Surface); ok {
		surface = override
	}
	return router.Advertise(s.tools, surface)
}

// ToolSurfaceSummary reports the process surface, how many tools its
// tools/list advertises, and the size in bytes of the marshaled tools/list
// result. The command logs it once at startup so an operator can confirm
// which surface a deployment serves and what it costs a client.
func (s *Server) ToolSurfaceSummary() (router.Surface, int, int) {
	surface := s.processSurface()
	tools := router.Advertise(s.tools, surface)
	raw, err := json.Marshal(mcpToolsListResult{Tools: tools})
	if err != nil {
		return surface, len(tools), 0
	}
	return surface, len(tools), len(raw)
}

// RunHTTP starts the MCP server as an HTTP service listening on addr. It
// exposes GET /sse (SSE transport), POST /mcp/message (JSON-RPC endpoint,
// standalone or with an SSE session), and GET /health (k8s probe). Routes
// from extra mount alongside them. Blocks until ctx is cancelled.
func (s *Server) RunHTTP(ctx context.Context, addr string, extra *http.ServeMux) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(extra),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // disable for SSE long-lived connections
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// WithoutCancel, not Background: the parent is already done (that is
		// the trigger), and the shutdown still gets its own 5s bound.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("mcp server shutdown failed", "error", err)
		}
	}()

	s.logger.Info("mcp server started", "transport", "http+sse", "addr", addr, "tools", len(s.tools), "tool_surface", string(s.processSurface()))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

// Handler returns the full HTTP mux RunHTTP would serve — GET /sse,
// POST /mcp/message, and GET /health — without binding a real listener, so
// tests can drive the transport surface directly. Routes from extra mount
// alongside them; a nil extra mounts nothing.
func (s *Server) Handler(extra *http.ServeMux) *http.ServeMux {
	mux := http.NewServeMux()
	if extra != nil {
		mux.Handle("/", extra)
	}

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			return
		}
	})

	// SSE transport endpoint and the JSON-RPC endpoint (standalone POST or
	// SSE-linked POST) both require the edge credential check when transport
	// auth is configured; authenticatedTransportHandler is a no-op wrap
	// when s.transportAuth is nil.
	mux.HandleFunc("GET /sse", s.authenticatedTransportHandler("sse", s.handleSSE))
	mux.HandleFunc("POST /mcp/message", s.authenticatedTransportHandler("", s.handleHTTPMessage))

	return mux
}

func (s *Server) handleMessage(ctx context.Context, req *jsonrpcRequest, authHeader string) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		requested := requestedProtocolVersion(req.Params)
		negotiated := negotiateProtocolVersion(requested, supportedProtocolVersions)
		if requested != negotiated {
			s.logger.Info("mcp initialize requested an unsupported protocol version, answering with the server version",
				"requested_protocol_version", truncateForLog(requested), "negotiated_protocol_version", negotiated)
		}
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcpInitializeResult{
				ProtocolVersion: negotiated,
				Capabilities: mcpCapabilities{
					Tools: &mcpToolsCap{ListChanged: false},
				},
				ServerInfo: mcpServerInfo{
					Name:    s.name,
					Version: s.version,
				},
			},
		}

	case "notifications/initialized":
		return nil // notification, no response

	case "tools/list":
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolsListResult{Tools: s.advertisedTools(ctx)},
		}

	case "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, -32602, "invalid params")
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: s.callTool(ctx, params, authHeader)}

	case "ping":
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	default:
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// callTool answers one tools/call. find_tool and call_tool go to the router,
// which runs a call_tool target through the same run as a direct call, so a
// routed result is byte-identical to a direct one.
func (s *Server) callTool(ctx context.Context, params mcpToolCallParams, authHeader string) any {
	mode := s.processShape()
	if override, ok := resultshape.FromContext(ctx); ok {
		mode = resultshape.OrDual(override)
	}
	ctx = resultshape.WithOverride(ctx, mode)
	run := func(ctx context.Context, name string, args map[string]any) (any, error) {
		result, err := s.dispatch(ctx, name, args, authHeader)
		if err != nil {
			return dispatch.ToolErrorResultForCall(mode, name, err, s.logger, dispatch.DefaultOptions().ResponseByteBudget), nil
		}
		if rendered, ok := result.(dispatch.ToolResult); ok {
			return rendered, nil
		}
		return result, nil
	}
	if !router.Intercepts(params.Name) {
		result, runErr := run(ctx, params.Name, params.Arguments)
		if runErr != nil {
			return dispatch.ToolErrorResultForCall(mode, params.Name, runErr, s.logger, dispatch.DefaultOptions().ResponseByteBudget)
		}
		return result
	}
	newConfig := func() router.Config {
		return router.Config{Registry: s.tools, Resolve: dispatch.ResolveRoute, Logger: s.logger}
	}
	req := router.Request{Visible: s.tools, Session: principalKey(ctx), Dispatch: run}
	return s.router.Serve(ctx, newConfig, req, params.Name, params.Arguments)
}

func (s *Server) errorResponse(id any, code int, msg string) *jsonrpcResponse {
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: msg},
	}
}

// surfaceContextKey carries a per-request or per-SSE-session surface override
// from the HTTP transport to handleMessage without changing its signature.
type surfaceContextKey struct{}

// withSurfaceOverride returns ctx carrying surface as the override for the
// request being served.
func withSurfaceOverride(ctx context.Context, surface router.Surface) context.Context {
	return context.WithValue(ctx, surfaceContextKey{}, surface)
}
