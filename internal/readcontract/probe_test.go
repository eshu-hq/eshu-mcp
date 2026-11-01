package readcontract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// probeCore records the one request the admission probe sends.
type probeCore struct {
	path, rawQuery, auth, accept, method string
}

func newProbeCore(t *testing.T, status int, challenge string) (*httptest.Server, *probeCore) {
	t.Helper()

	seen := &probeCore{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path, seen.rawQuery = r.URL.Path, r.URL.RawQuery
		seen.auth, seen.accept, seen.method = r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Method
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func newProbeClient(t *testing.T, baseURL string) *Client {
	t.Helper()

	client, err := NewClient(DefaultConfig(baseURL, ""))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// TestProbeAdmissionSendsTheCallerCredentialToTheTenantFreeRoute pins the
// one request admission makes: a GET on the capability catalog, bounded to a
// single entry, carrying the caller's Authorization header verbatim.
func TestProbeAdmissionSendsTheCallerCredentialToTheTenantFreeRoute(t *testing.T) {
	t.Parallel()

	srv, seen := newProbeCore(t, http.StatusOK, "")
	client := newProbeClient(t, srv.URL)

	status, challenge, err := client.ProbeAdmission(context.Background(), "Bearer opaque-token")
	if err != nil {
		t.Fatalf("ProbeAdmission() error = %v", err)
	}
	if status != http.StatusOK || challenge != "" {
		t.Errorf("status, challenge = %d, %q; want 200 and no challenge", status, challenge)
	}
	if seen.method != http.MethodGet || seen.path != "/api/v0/capabilities" || seen.rawQuery != "limit=1" {
		t.Errorf("probe sent %s %s?%s, want GET /api/v0/capabilities?limit=1", seen.method, seen.path, seen.rawQuery)
	}
	if seen.auth != "Bearer opaque-token" {
		t.Errorf("Authorization = %q, want the caller credential verbatim", seen.auth)
	}
	if seen.accept != EnvelopeMIME {
		t.Errorf("Accept = %q, want %q", seen.accept, EnvelopeMIME)
	}
}

func TestProbeAdmissionRelaysCoreChallengeOnRefusal(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _ := newProbeCore(t, status, `Bearer resource_metadata="https://core/.well-known/oauth-protected-resource"`)
		client := newProbeClient(t, srv.URL)

		got, challenge, err := client.ProbeAdmission(context.Background(), "Basic x")
		if err != nil {
			t.Fatalf("ProbeAdmission() error = %v", err)
		}
		if got != status {
			t.Errorf("status = %d, want core's %d", got, status)
		}
		if challenge == "" {
			t.Errorf("status %d: challenge empty, want core's WWW-Authenticate relayed", status)
		}
	}
}

// TestProbeAdmissionDoesNotFollowRedirects keeps the credential from being
// carried to a location the probe did not intend.
func TestProbeAdmissionDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	hit := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	t.Cleanup(elsewhere.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	status, _, err := newProbeClient(t, srv.URL).ProbeAdmission(context.Background(), "Bearer x")
	if err != nil {
		t.Fatalf("ProbeAdmission() error = %v", err)
	}
	if status != http.StatusFound || hit {
		t.Errorf("status = %d, redirect target hit = %v; want the 302 returned and no follow", status, hit)
	}
}

func TestProbeAdmissionReportsTransportFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := newProbeClient(t, srv.URL)
	srv.Close()

	if _, _, err := client.ProbeAdmission(context.Background(), "Bearer x"); err == nil {
		t.Fatal("ProbeAdmission() error = nil against a closed core, want a transport error")
	}
}
