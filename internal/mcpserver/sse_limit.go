package mcpserver

// Per-principal bound on concurrent SSE sessions (eshu-hq/eshu-mcp#50).
// GET /sse mints a session, a 64-slot channel, and a handler goroutine that
// live until the client disconnects. Without a bound, one credential can open
// sessions until the process runs out of memory or file descriptors, and the
// edge door check (which only proves the credential is valid) does nothing to
// stop it.
//
// The bound is keyed by principalKey, the same identity sessions bind to for
// the POST /mcp/message mismatch check. That key is already an opaque hash or
// the literal "shared", so the counters hold no credential material. Two
// principal classes need a note:
//
//   - The shared deployment key maps to the single principal "shared", so every
//     caller that uses it shares one allowance. A team behind one shared key
//     must raise the cap with WithMaxSSESessionsPerPrincipal (the command
//     exposes it as ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL).
//   - A deployment without transport auth (the loopback escape hatch) has no
//     principal: every session carries "". Those sessions share one anonymous
//     bucket, so the escape hatch is bounded too instead of being an
//     unbounded mint.
//
// The slot is taken inside the same sessMu critical section that inserts the
// session and returned in the same critical section that removes it, so there
// is no check-then-insert window and no path that inserts without counting.

import (
	"net/http"
	"strconv"
	"sync/atomic"
)

const (
	// defaultMaxSSESessionsPerPrincipal is the cap when the option is absent.
	// A well-behaved client holds one session; a reconnect briefly overlaps the
	// new session with the old one until the server notices the dropped TCP
	// connection, and one credential may legitimately serve a few editors or
	// agents. 32 leaves that headroom and, for the shared key, room for a small
	// team, while keeping one credential to roughly 32 goroutines and 32
	// buffered channels instead of an unbounded number.
	defaultMaxSSESessionsPerPrincipal = 32

	// sseSessionLimitRetryAfterSeconds is the retry hint on a 429. A slot frees
	// when a stale connection is torn down, which is not instant, so the hint is
	// longer than the admission 503's one second.
	sseSessionLimitRetryAfterSeconds = 5

	// sseSessionLimitErrorCode is the JSON-RPC error code for a refused session.
	// It sits next to -32001 (session owner mismatch) in the implementation-
	// defined server error range.
	sseSessionLimitErrorCode = -32002
)

// SSEStats reports the SSE session bound so an operator can see saturation
// without per-credential values. Both fields are aggregates.
type SSEStats struct {
	// Active is the number of live SSE sessions across all principals.
	Active int
	// Rejected counts GET /sse requests refused because the principal was at
	// its cap, since start.
	Rejected int64
}

// WithMaxSSESessionsPerPrincipal caps concurrent SSE sessions per principal.
// A value below one keeps the default, so a misconfigured zero cannot disable
// the bound.
func WithMaxSSESessionsPerPrincipal(limit int) ServerOption {
	return func(s *Server) {
		s.sseMaxPerPrincipal = limit
	}
}

// maxSSEPerPrincipal returns the effective per-principal cap.
func (s *Server) maxSSEPerPrincipal() int {
	if s.sseMaxPerPrincipal < 1 {
		return defaultMaxSSESessionsPerPrincipal
	}
	return s.sseMaxPerPrincipal
}

// SSEStats returns the current session count and the refusal counter.
func (s *Server) SSEStats() SSEStats {
	s.sessMu.RLock()
	active := len(s.sessions)
	s.sessMu.RUnlock()
	return SSEStats{Active: active, Rejected: s.sseLimit.rejected.Load()}
}

// sseLimiter is the per-principal live-session count. It is a field of
// Server and guarded by Server.sessMu, never by its own lock, so the count and
// the session map cannot disagree.
type sseLimiter struct {
	perPrincipal map[string]int
	rejected     atomic.Int64
}

// registerSession inserts sess under id unless its principal is at the cap.
// It reports whether the session was registered. The cap check and the insert
// share one sessMu critical section.
func (s *Server) registerSession(id string, sess *sseSession) bool {
	limit := s.maxSSEPerPrincipal()
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if s.sseLimit.perPrincipal[sess.principal] >= limit {
		return false
	}
	if s.sseLimit.perPrincipal == nil {
		s.sseLimit.perPrincipal = make(map[string]int)
	}
	s.sseLimit.perPrincipal[sess.principal]++
	s.sessions[id] = sess
	return true
}

// unregisterSession removes the session and returns its slot. It is safe to
// call for an id that was never registered or was already removed: the slot is
// returned only when the id was present, so the count cannot underflow.
func (s *Server) unregisterSession(id string) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return
	}
	delete(s.sessions, id)
	if remaining := s.sseLimit.perPrincipal[sess.principal] - 1; remaining > 0 {
		s.sseLimit.perPrincipal[sess.principal] = remaining
	} else {
		delete(s.sseLimit.perPrincipal, sess.principal)
	}
}

// refuseSessionOverLimit counts and logs a refused mint and answers 429 with
// a Retry-After hint and a JSON-RPC error in the same shape as the other
// handleSSE refusals. The log line carries only bounded fields: the principal
// is a credential hash and is never logged.
func (s *Server) refuseSessionOverLimit(w http.ResponseWriter) {
	s.sseLimit.rejected.Add(1)
	s.logger.Warn("sse session refused: principal at session cap",
		"outcome", "sse_session_cap_reached", "limit", s.maxSSEPerPrincipal())
	w.Header().Set("Retry-After", strconv.Itoa(sseSessionLimitRetryAfterSeconds))
	writeJSON(w, http.StatusTooManyRequests, jsonrpcResponse{
		JSONRPC: "2.0",
		Error:   &jsonrpcError{Code: sseSessionLimitErrorCode, Message: "too many open sessions for this credential; close one or retry shortly"},
	})
}
