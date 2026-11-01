package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
)

// admissionProbeDelay stands in for one core round trip. Real core latency is
// deployment specific; 5ms is a same-region, authenticated, cache-warm read.
// The with/without-cache ratio scales with this number, so the benchmark
// reports the absolute per-request time for the fixed delay it pins.
const admissionProbeDelay = 5 * time.Millisecond

// delayedCore is a fake core whose admission probe takes a fixed time and
// counts the probes it served.
func delayedCore(b *testing.B) (*readcontract.Client, *atomic.Int64) {
	b.Helper()
	var probes atomic.Int64
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		time.Sleep(admissionProbeDelay)
		w.WriteHeader(http.StatusOK)
	}))
	b.Cleanup(core.Close)
	client, err := readcontract.NewClient(readcontract.DefaultConfig(core.URL, ""))
	if err != nil {
		b.Fatalf("NewClient() error = %v", err)
	}
	return client, &probes
}

// benchAdmissionRequest measures one locally served request (ping) behind the
// production admission middleware, the path eshu-hq/eshu-mcp#50 adds to every
// initialize, tools/list, ping, and GET /sse. cached keeps the shipped TTLs;
// uncached sets the positive TTL to zero so every request pays the probe.
func benchAdmissionRequest(b *testing.B, cached bool) {
	client, probes := delayedCore(b)
	admission := newTestAdmission(client)
	if !cached {
		admission.positiveTTL = 0
	}
	h := admissionServer(b, "", nil, admission)
	auth := "Bearer " + strings.Repeat("a", 43)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	// Warm the connection pool and, when cached, the decision, so the timed
	// loop measures steady state rather than the first dial.
	warm := httptest.NewRecorder()
	warmReq := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
	warmReq.Header.Set("Authorization", auth)
	h.ServeHTTP(warm, warmReq)
	if warm.Code != http.StatusOK {
		b.Fatalf("warm-up ping = %d, want 200", warm.Code)
	}
	probes.Store(0)

	b.ResetTimer()
	for range b.N {
		req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("ping = %d, want 200", rec.Code)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(probes.Load())/float64(b.N), "probes/op")
}

// BenchmarkAdmissionRequestCached is the steady state: one admitted
// credential repeated inside the TTL, so no request after the first probes.
func BenchmarkAdmissionRequestCached(b *testing.B) { benchAdmissionRequest(b, true) }

// BenchmarkAdmissionRequestUncached forces a probe on every request: the cost
// of admission with the cache removed, and the ceiling the cache protects.
func BenchmarkAdmissionRequestUncached(b *testing.B) { benchAdmissionRequest(b, false) }
