package mcpserver

// SSE transport and JSON-RPC message endpoint, ported from
// go/internal/mcp/server_sse.go at 9922b49. Sessions, the endpoint event,
// keepalives, standalone vs SSE-linked POST, principal binding, the surface
// and shape query parameters, and every status code and error body are
// unchanged. OTEL metric calls and the query route-denial signal are
// replaced with slog lines: the denial reasons stay visible to operators
// without a telemetry bootstrap. See docs/parity.md.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// sseSession holds the response channel for one SSE client. principal
// identifies the credential that opened the session; it is empty when
// transport auth is not configured, in which case session binding is not
// enforced.
//
// mu/closed guard the channel lifecycle: teardown closes the channel exactly
// once, so a POST /mcp/message that runs a slow tools/call while the SSE
// client disconnects can never send on a closed channel.
type sseSession struct {
	ch        chan []byte
	principal string
	// surface is the tool surface the client chose with GET /sse?surface=.
	// It is empty when the client chose none, so the process surface applies.
	// A POST that names its own surface still overrides it.
	surface router.Surface
	// shape is the tools/call result shape the client chose with
	// GET /sse?shape=. It is empty when the client chose none, so the process
	// shape applies. A POST that names its own shape still overrides it.
	shape resultshape.Mode

	mu     sync.Mutex
	closed bool
}

// send delivers msg to the SSE stream. It returns false when the session is
// already closed (client disconnected) or its buffer is full, so the caller
// logs a drop rather than block or panic. The non-blocking select keeps the
// lock hold short: it never waits for a reader while holding mu.
func (sess *sseSession) send(msg []byte) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return false
	}
	select {
	case sess.ch <- msg:
		return true
	default:
		return false
	}
}

// shutdown marks the session closed and closes the channel exactly once. It
// is safe to call concurrently with send: both take mu, so an in-flight send
// completes before shutdown closes the channel, and any later send observes
// closed and skips the channel entirely.
func (sess *sseSession) shutdown() {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return
	}
	sess.closed = true
	close(sess.ch)
}

// newSessionID mints an unguessable SSE session id. The id gates which
// stream a credential's POST lands on, so wall-clock values would let any
// credentialed caller enumerate another principal's session.
func newSessionID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("mcp: mint session id: %w", err)
	}
	return "sess_" + hex.EncodeToString(entropy[:]), nil
}

// sseEndpointURL builds the `endpoint` event URL that tells an SSE client
// where to POST JSON-RPC messages. A chosen surface rides in the URL so
// every POST the client follows carries it. Each value is query-escaped:
// the session id is server-minted and surface/shape are validated enums,
// and escaping keeps that invariant even if a value ever widens. The
// parameter order matches the pre-extraction server exactly
// (sessionId, surface, shape); TestSSEEndpointURLPinsParameterOrder pins it.
func sseEndpointURL(sessionID string, surface router.Surface, shape resultshape.Mode) string {
	endpoint := "/mcp/message?sessionId=" + url.QueryEscape(sessionID)
	if surface != "" {
		endpoint += "&surface=" + url.QueryEscape(string(surface))
	}
	if shape != "" {
		endpoint += "&shape=" + url.QueryEscape(string(shape))
	}
	return endpoint
}

// handleSSE establishes an SSE connection. It sends an `endpoint` event
// telling the client where to POST JSON-RPC messages, then streams keepalive
// events and any responses for the session.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// A surface query parameter picks the tool surface for every message of
	// this SSE session. Reject an unknown value before a session exists.
	surface, err := parseSurfaceParam(r)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}

	// A shape query parameter picks the tools/call result shape for every
	// message of this SSE session; an unknown value is rejected the same way.
	shape, err := parseShapeParam(r)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}

	// Create a session for this SSE connection, binding it to the credential
	// that opened it (empty when transport auth is not configured).
	sessionID, err := newSessionID()
	if err != nil {
		s.logger.Error("sse session id minting failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, jsonrpcResponse{
			JSONRPC: "2.0",
			Error:   &jsonrpcError{Code: -32603, Message: "internal error"},
		})
		return
	}
	sess := &sseSession{ch: make(chan []byte, 64), principal: principalKey(r.Context()), surface: surface, shape: shape}

	s.sessMu.Lock()
	s.sessions[sessionID] = sess
	s.sessMu.Unlock()

	defer func() {
		s.sessMu.Lock()
		delete(s.sessions, sessionID)
		s.sessMu.Unlock()
		sess.shutdown()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	// Send the endpoint event per MCP SSE spec.
	// The client uses this URL to POST JSON-RPC requests.
	endpoint := sseEndpointURL(sessionID, surface, shape)
	// #nosec G705 -- the sink is an SSE text/event-stream data frame, never HTML-interpreted, and every interpolated value is QueryEscape percent-encoded (outside [A-Za-z0-9-_.~], including <>"'&) from a server-minted hex session id plus validated surface/shape enums, so no markup can reach the sink.
	if _, err := fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint); err != nil {
		return
	}
	flusher.Flush()

	s.logger.Info("sse session started", "session_id", sessionID)

	// Keepalive ticker.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("sse session closed", "session_id", sessionID)
			return
		case msg, ok := <-sess.ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleHTTPMessage handles POST /mcp/message. If a sessionId query param is
// present, the response is sent via the SSE stream. Otherwise, the response
// is returned directly in the HTTP response body.
//
// When sessionId names a live session, the request's credential must match
// the credential that opened the session: a different principal is rejected
// with 403 before the request body is even decoded, so it cannot smuggle a
// message into another principal's SSE stream. Sessions opened without
// transport auth configured carry an empty principal and are never bound —
// both sides of an unauthenticated deployment are equally open, so there is
// nothing meaningful to bind.
func (s *Server) handleHTTPMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	var sess *sseSession
	if sessionID != "" {
		s.sessMu.RLock()
		sess = s.sessions[sessionID]
		s.sessMu.RUnlock()
	}

	if sess != nil && sess.principal != "" {
		if reqPrincipal := principalKey(r.Context()); reqPrincipal != sess.principal {
			s.logger.Warn("mcp transport session principal mismatch refused",
				"method", "mcp_message", "session_id", sessionID)
			writeForbiddenSessionMismatch(w)
			return
		}
	}

	// A surface query parameter overrides the process tool surface for this
	// request; without one, an SSE-linked request uses its session's surface.
	// An unknown value is rejected before the body is read.
	ctx := r.Context()
	surface, err := parseSurfaceParam(r)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}
	if surface == "" && sess != nil {
		surface = sess.surface
	}
	if surface != "" {
		ctx = withSurfaceOverride(ctx, surface)
	}
	// The same precedence for the result shape: the request's parameter, then
	// its session's choice, then the process default applied by callTool.
	shape, err := parseShapeParam(r)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}
	if shape == "" && sess != nil {
		shape = sess.shape
	}
	if shape != "" {
		ctx = resultshape.WithOverride(ctx, shape)
	}

	var req jsonrpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(jsonrpcResponse{
			JSONRPC: "2.0",
			Error:   &jsonrpcError{Code: -32700, Message: "parse error"},
		}); err != nil {
			return
		}
		return
	}

	resp := s.handleMessage(ctx, &req, r.Header.Get("Authorization"))

	if sess != nil {
		if resp != nil {
			encoded, err := json.Marshal(resp)
			if err == nil && !sess.send(encoded) {
				s.logger.Warn("sse session buffer full or closed, dropping message", "session_id", sessionID)
			}
			// For SSE-linked requests, return 202 Accepted (response sent via SSE).
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	// Standalone POST mode — return response directly.
	w.Header().Set("Content-Type", "application/json")
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}

// authenticatedTransportHandler wraps next with the server's edge-auth
// middleware and logs denied requests with the bounded reason label, so an
// operator can see catalog-enumeration attempts against initialize/
// tools/list/tools/call/ping and GET /sse. When s.transportAuth is nil (the
// explicit escape hatch), this is a plain pass-through.
func (s *Server) authenticatedTransportHandler(staticMethod string, next http.HandlerFunc) http.HandlerFunc {
	if s.transportAuth == nil {
		return next
	}
	wrapped := s.transportAuth(http.HandlerFunc(next))
	return func(w http.ResponseWriter, r *http.Request) {
		method := staticMethod
		if method == "" {
			method, r = peekMCPMethod(r)
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		wrapped.ServeHTTP(rec, r)
		if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
			s.logger.Warn("mcp transport request denied",
				"method", method, "status", rec.status, "path", r.URL.Path)
		}
	}
}

// statusRecorder captures the response status code for the denial log.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusRecorder) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it implements http.Flusher.
// GET /sse depends on this: without it, wrapping handleSSE in transport auth
// would hide the SSE handler's flushing behind a non-Flusher wrapper.
func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// knownMCPMethods bounds the "method" label in the denial log. An unknown or
// unparsable method is labeled "other"/"unknown" rather than passed through
// raw, so a malformed or malicious body cannot explode log cardinality.
var knownMCPMethods = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"tools/list":                true,
	"tools/call":                true,
	"ping":                      true,
}

// mcpMethodPeek is the minimal JSON-RPC envelope needed to label a denied
// request by method before a real handler decodes it.
type mcpMethodPeek struct {
	Method string `json:"method"`
}

// mcpMethodPeekLimit bounds how many leading bytes peekMCPMethod buffers to
// extract the JSON-RPC "method" for the denial log label. This peek runs
// BEFORE auth, so it must not let an unauthenticated caller amplify memory by
// posting a giant body. A well-formed JSON-RPC envelope carries "method" near
// the front; 4 KiB is ample. A body whose method sits past the limit simply
// degrades to the "unknown" label — the label is best-effort telemetry, never
// a correctness input.
const mcpMethodPeekLimit = 4 << 10

// peekedBody restores the full request body after peekMCPMethod buffers only
// its head: Read serves the buffered head followed by the untouched tail, and
// Close closes the original body. This keeps the authenticated handler's view
// of the body complete (no truncation of a legitimate large tools/call
// payload) while bounding the pre-auth buffered bytes to mcpMethodPeekLimit.
type peekedBody struct {
	io.Reader
	orig io.Closer
}

func (b peekedBody) Close() error { return b.orig.Close() }

// peekMCPMethod buffers at most mcpMethodPeekLimit leading bytes of r.Body to
// read the JSON-RPC "method", stitches the untouched remainder back onto the
// request (so the eventual authenticated handler still decodes the full
// request), and returns the bounded method label. It never truncates the body
// the handler sees; only the pre-auth peek buffer is bounded.
func peekMCPMethod(r *http.Request) (string, *http.Request) {
	if r.Body == nil {
		return "unknown", r
	}
	orig := r.Body
	head, err := io.ReadAll(io.LimitReader(orig, mcpMethodPeekLimit))
	// Reconstruct the full body regardless of outcome: buffered head first,
	// then whatever remains unread on the original stream.
	r.Body = peekedBody{Reader: io.MultiReader(bytes.NewReader(head), orig), orig: orig}
	if err != nil {
		return "unknown", r
	}
	var peek mcpMethodPeek
	if jsonErr := json.Unmarshal(head, &peek); jsonErr != nil || peek.Method == "" {
		return "unknown", r
	}
	if !knownMCPMethods[peek.Method] {
		return "other", r
	}
	return peek.Method, r
}

// parseSurfaceParam reads the surface query parameter. It trims surrounding
// whitespace first, so the query and the environment agree on what they
// accept. It returns "" when the parameter is absent, empty, or only
// whitespace, so the caller keeps the process surface, and an error naming
// the allowed values when the value is unknown.
func parseSurfaceParam(r *http.Request) (router.Surface, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("surface"))
	if raw == "" {
		return "", nil
	}
	return router.ParseSurface(raw)
}

// parseShapeParam reads the shape query parameter with the same contract as
// parseSurfaceParam: trimmed, "" when absent or whitespace-only so the caller
// keeps the process shape, an error naming the allowed values when unknown,
// and a canonical (lower case) value otherwise.
func parseShapeParam(r *http.Request) (resultshape.Mode, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("shape"))
	if raw == "" {
		return "", nil
	}
	return resultshape.Parse(raw)
}

// writeSurfaceError answers an unknown surface or shape value with a 400
// JSON-RPC error whose message lists the allowed values.
func writeSurfaceError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		Error:   &jsonrpcError{Code: -32602, Message: err.Error()},
	}); err != nil {
		return
	}
}
