package mcpserver

// Negative kid cache and singleflight refetch: one crafted token must not
// trigger egress per request, however its kids alternate or concur.

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testIssuerServerCounted is testIssuerServer with a hit counter, so tests
// can prove the negative cache and singleflight actually suppress egress.
func testIssuerServerCounted(t *testing.T, key *rsa.PrivateKey, kid string) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issuer":   srv.URL,
			"jwks_uri": srv.URL + "/jwks",
		}); err != nil {
			t.Errorf("encode discovery doc: %v", err)
		}
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(jwksDoc(t, &key.PublicKey, kid))); err != nil {
			t.Errorf("write jwks doc: %v", err)
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestVerifyUnknownKidRefetchThenDeny(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv := testIssuerServer(t, key, testKid)
	v := testVerifier(t, srv)

	result := v.Verify(mintToken(t, key, "rotated-key", srv.URL, testResource, time.Now().Add(time.Hour)))
	if result.Denial != DenyPostMatch {
		t.Fatalf("result.Denial = %v, want post-match", result.Denial)
	}
}

func TestUnknownKidSecondSightNoRefetch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv, hits := testIssuerServerCounted(t, key, testKid)
	v := testVerifier(t, srv)

	bad := mintToken(t, key, "rotated-key", srv.URL, testResource, time.Now().Add(time.Hour))
	if result := v.Verify(bad); result.Denial != DenyPostMatch {
		t.Fatalf("first sight: result.Denial = %v", result.Denial)
	}
	firstHits := hits.Load()
	if result := v.Verify(bad); result.Denial != DenyPostMatch {
		t.Fatalf("second sight: result.Denial = %v", result.Denial)
	}
	if got := hits.Load(); got != firstHits {
		t.Fatalf("second unknown-kid sight fetched again: hits %d -> %d", firstHits, got)
	}
}

func TestUnknownKidConcurrentSingleFetch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv, hits := testIssuerServerCounted(t, key, testKid)
	v := testVerifier(t, srv)

	bad := mintToken(t, key, "rotated-key", srv.URL, testResource, time.Now().Add(time.Hour))
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if result := v.Verify(bad); result.Denial != DenyPostMatch {
				t.Errorf("result.Denial = %v, want post-match", result.Denial)
			}
		}()
	}
	wg.Wait()
	// One discovery fetch plus one JWKS fetch; followers wait instead of
	// fetching. Generous bound: never one fetch per request.
	if got := hits.Load(); got > 4 {
		t.Fatalf("concurrent unknown-kid requests fetched %d times", got)
	}
}

func TestUnknownKidsAlternateWithoutRefetch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	srv, hits := testIssuerServerCounted(t, key, testKid)
	v := testVerifier(t, srv)

	ghostA := mintToken(t, key, "ghost-a", srv.URL, testResource, time.Now().Add(time.Hour))
	ghostB := mintToken(t, key, "ghost-b", srv.URL, testResource, time.Now().Add(time.Hour))
	if result := v.Verify(ghostA); result.Denial != DenyPostMatch {
		t.Fatalf("first ghost-a: result.Denial = %v", result.Denial)
	}
	if result := v.Verify(ghostB); result.Denial != DenyPostMatch {
		t.Fatalf("first ghost-b: result.Denial = %v", result.Denial)
	}
	warm := hits.Load()
	for range 5 {
		if result := v.Verify(ghostA); result.Denial != DenyPostMatch {
			t.Fatalf("repeat ghost-a: result.Denial = %v", result.Denial)
		}
		if result := v.Verify(ghostB); result.Denial != DenyPostMatch {
			t.Fatalf("repeat ghost-b: result.Denial = %v", result.Denial)
		}
	}
	if got := hits.Load(); got != warm {
		t.Fatalf("alternating unknown kids refetched: hits %d -> %d", warm, got)
	}
}
