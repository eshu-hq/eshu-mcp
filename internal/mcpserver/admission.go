package mcpserver

// Edge admission of forwarded principals (eshu-hq/eshu-mcp#50). The door
// check verifies dotted Bearer JWTs itself, but a dotless Bearer (Eshu
// personal and scoped tokens) and every non-Bearer scheme carry nothing the
// edge can inspect, so they forward as "fwd:" principals. tools/call is
// admitted by core on dispatch; the locally served methods (initialize,
// tools/list, ping, and the GET /sse session mint) never dispatch. The owner
// ruling on eshu-mcp#27 is that a satellite turns bad callers away at the
// door instead of forwarding blindly, so a forwarded principal is admitted
// here only after core says its credential is valid.
//
// Core is the only inspector for these credentials, so the edge asks it: one
// authenticated GET on a route that carries no tenant data and that every
// credential class core accepts may call (a scoped token, a personal token, a
// file-registry token, the shared key). A 200 admits, a 401 or 403 denies, and
// anything else, including no answer, is "unavailable" and fails closed with a
// retryable 503. Decisions are cached by credential hash so a repeated
// credential costs one core round trip per TTL; an unavailable answer is never
// cached.

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// admissionPositiveTTL bounds how long an admitted credential skips the
	// probe. A revoked credential stays admitted for the locally served
	// methods for at most this long; tools/call is still admitted by core on
	// every dispatch.
	admissionPositiveTTL = 30 * time.Second
	// admissionNegativeTTL is shorter than the positive TTL so a credential
	// that was just issued is not refused for long, while a flood of one bad
	// credential still costs core one request per window.
	admissionNegativeTTL = 10 * time.Second
	// admissionMaxEntries caps the cache so a flood of distinct credentials
	// cannot grow it without limit. A full cache evicts the entry closest to
	// expiry to make room, so a repeated credential still costs one probe per
	// TTL.
	admissionMaxEntries = 4096
	// admissionProbeTimeout bounds one probe, well under the dispatch
	// timeout, so a stalled core answers 503 quickly instead of hanging the
	// locally served methods.
	admissionProbeTimeout = 5 * time.Second
	// admissionRetryAfterSeconds is the retry hint on a 503, matching core's
	// identity-store-unavailable response.
	admissionRetryAfterSeconds = 1
)

// forwardedPrincipalPrefix marks a principal the edge could not verify and
// forwarded: the only principals admission probes.
const forwardedPrincipalPrefix = "fwd:"

// AdmissionProber asks core whether it admits one credential. It returns the
// HTTP status of an authenticated, tenant-data-free read, core's
// WWW-Authenticate challenge when it sent one, and a transport error. The
// credential is the caller's Authorization header, verbatim.
type AdmissionProber interface {
	ProbeAdmission(ctx context.Context, authHeader string) (status int, challenge string, err error)
}

// AdmissionStats counts admission outcomes since start so an operator can
// tell "core denied" from "core unavailable" and see the cache working. Every
// field is a bounded outcome, never a per-credential value.
type AdmissionStats struct {
	// Admitted counts probes core answered 200.
	Admitted int64
	// Denied counts probes core answered 401 or 403.
	Denied int64
	// Unavailable counts probes that failed closed: transport error, timeout,
	// or any status other than 200, 401, and 403.
	Unavailable int64
	// CacheHit counts decisions served without a probe.
	CacheHit int64
}

type admissionKind int

const (
	admissionAdmitted admissionKind = iota
	admissionDenied
	admissionUnavailable
)

// admissionDecision is the outcome for one forwarded credential. status and
// challenge are core's, relayed on a denial.
type admissionDecision struct {
	kind      admissionKind
	status    int
	challenge string
}

type admissionEntry struct {
	decision admissionDecision
	expires  time.Time
}

// Admission admits forwarded principals through core. It is safe for
// concurrent use. Concurrent first requests for one credential may each probe
// core; the answers agree and the cost is bounded by the cache TTL.
type Admission struct {
	prober AdmissionProber
	logger *slog.Logger

	now          func() time.Time
	positiveTTL  time.Duration
	negativeTTL  time.Duration
	maxEntries   int
	probeTimeout time.Duration

	mu    sync.Mutex
	cache map[string]admissionEntry

	admitted    atomic.Int64
	denied      atomic.Int64
	unavailable atomic.Int64
	cacheHit    atomic.Int64
}

// NewAdmission builds an Admission around a prober, with the pinned TTLs and
// bounds. logger may be nil.
func NewAdmission(prober AdmissionProber, logger *slog.Logger) *Admission {
	return &Admission{
		prober:       prober,
		logger:       logger,
		now:          time.Now,
		positiveTTL:  admissionPositiveTTL,
		negativeTTL:  admissionNegativeTTL,
		maxEntries:   admissionMaxEntries,
		probeTimeout: admissionProbeTimeout,
		cache:        make(map[string]admissionEntry),
	}
}

// Stats returns the outcome counters.
func (a *Admission) Stats() AdmissionStats {
	return AdmissionStats{
		Admitted:    a.admitted.Load(),
		Denied:      a.denied.Load(),
		Unavailable: a.unavailable.Load(),
		CacheHit:    a.cacheHit.Load(),
	}
}

// cacheLen reports the live cache size for tests.
func (a *Admission) cacheLen() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.cache)
}

// needsAdmission reports whether a principal is one the edge forwarded
// unverified. The shared key and an edge-verified JWT already proved
// themselves at the door.
func needsAdmission(principal string) bool {
	return strings.HasPrefix(principal, forwardedPrincipalPrefix)
}

// admit decides one forwarded principal. The principal is "fwd:" plus the
// SHA-256 of the credential, so it is the cache key and never the credential.
func (a *Admission) admit(ctx context.Context, principal, authHeader string) admissionDecision {
	if cached, ok := a.lookup(principal); ok {
		a.cacheHit.Add(1)
		return cached
	}
	probeCtx, cancel := context.WithTimeout(ctx, a.probeTimeout)
	defer cancel()
	status, challenge, err := a.prober.ProbeAdmission(probeCtx, authHeader)

	var decision admissionDecision
	switch {
	case err != nil:
		decision = admissionDecision{kind: admissionUnavailable}
	case status == http.StatusOK:
		decision = admissionDecision{kind: admissionAdmitted, status: status}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		decision = admissionDecision{kind: admissionDenied, status: status, challenge: challenge}
	default:
		decision = admissionDecision{kind: admissionUnavailable}
	}

	switch decision.kind {
	case admissionAdmitted:
		a.admitted.Add(1)
		a.store(principal, decision, a.positiveTTL)
	case admissionDenied:
		a.denied.Add(1)
		a.store(principal, decision, a.negativeTTL)
		a.log(slog.LevelWarn, "mcp forwarded credential denied by core", "outcome", "denied", "core_status", status)
	default:
		a.unavailable.Add(1)
		a.log(slog.LevelWarn, "mcp forwarded credential not admitted: core admission unavailable",
			"outcome", "unavailable", "core_status", status, "probe_failed", err != nil)
	}
	return decision
}

func (a *Admission) lookup(principal string) (admissionDecision, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[principal]
	if !ok {
		return admissionDecision{}, false
	}
	if !a.now().Before(entry.expires) {
		delete(a.cache, principal)
		return admissionDecision{}, false
	}
	return entry.decision, true
}

func (a *Admission) store(principal string, decision admissionDecision, ttl time.Duration) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.cache[principal]; !exists && len(a.cache) >= a.maxEntries {
		a.makeRoomLocked(now)
	}
	a.cache[principal] = admissionEntry{decision: decision, expires: now.Add(ttl)}
}

// makeRoomLocked frees one slot. It drops every expired entry, and when the
// cache is still full of live decisions it evicts the one closest to expiry.
// Storing the new decision (instead of skipping it) keeps the cost of a
// repeated credential at one probe per TTL even under a flood of distinct
// credentials: a flood can push valid entries out, but every flood request
// already cost core one probe, so eviction adds no amplification. The scan is
// O(maxEntries) and only runs on a miss, which already costs a core round trip.
func (a *Admission) makeRoomLocked(now time.Time) {
	var soonestKey string
	var soonest time.Time
	for key, entry := range a.cache {
		if !now.Before(entry.expires) {
			delete(a.cache, key)
			continue
		}
		if soonestKey == "" || entry.expires.Before(soonest) {
			soonestKey, soonest = key, entry.expires
		}
	}
	if len(a.cache) >= a.maxEntries && soonestKey != "" {
		delete(a.cache, soonestKey)
	}
}

func (a *Admission) log(level slog.Level, msg string, args ...any) {
	if a.logger != nil {
		a.logger.Log(context.Background(), level, msg, args...)
	}
}

// writeAdmissionDenied relays core's refusal: its 403 as a 403, its 401 as
// the edge's core-identical 401 carrying core's challenge when it sent one.
func writeAdmissionDenied(w http.ResponseWriter, r *http.Request, decision admissionDecision, fallbackChallenge string) {
	if decision.status == http.StatusForbidden {
		writeAdmissionError(w, r, http.StatusForbidden, "forbidden", "credential is not permitted")
		return
	}
	challenge := decision.challenge
	if challenge == "" {
		challenge = fallbackChallenge
	}
	writeUnauthorizedWithChallenge(w, r, challenge)
}

// writeAdmissionUnavailable answers the retryable 503 for a credential core
// could not judge. It sets no WWW-Authenticate challenge: the credential was
// never found wanting, so this is not an authentication failure. The shape
// mirrors core's identity-store-unavailable response.
func writeAdmissionUnavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", strconv.Itoa(admissionRetryAfterSeconds))
	writeAdmissionError(w, r, http.StatusServiceUnavailable, "backend_unavailable",
		"credential admission temporarily unavailable; retry shortly")
}

func writeAdmissionError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if acceptsEnvelope(r) {
		writeJSON(w, status, map[string]any{
			"data":  nil,
			"truth": nil,
			"error": map[string]any{
				"code":           code,
				"message":        message,
				"correlation_id": correlationID(r),
			},
		})
		return
	}
	writeJSON(w, status, map[string]string{
		"error_code":     code,
		"message":        message,
		"correlation_id": correlationID(r),
	})
}
