package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthReportsContract(t *testing.T) {
	t.Parallel()

	srv := newServer("https://core:8443")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	resp := rec.Result()
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close /healthz body: %v", closeErr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", resp.StatusCode)
	}
	if err != nil {
		t.Fatalf("read /healthz body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	if decoded["contract"] != "v1" {
		t.Errorf("/healthz contract = %v, want v1", decoded["contract"])
	}
	if decoded["tools"] != float64(168) {
		t.Errorf("/healthz tools = %v, want 168", decoded["tools"])
	}
	if decoded["core"] != "https://core:8443" {
		t.Errorf("/healthz core = %v, want the configured origin", decoded["core"])
	}
}

func TestRunRequiresCoreBaseURL(t *testing.T) {
	t.Parallel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run([]string{}, quiet)
	if err == nil || !strings.Contains(err.Error(), "--core-base-url") {
		t.Fatalf("run() error = %v, want a missing --core-base-url error", err)
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	t.Parallel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run([]string{"--no-such-flag"}, quiet)
	if err == nil {
		t.Fatal("run() succeeded with an unknown flag, want an error")
	}
}

func TestRunSurfacesListenFailure(t *testing.T) {
	t.Parallel()

	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy a test port: %v", err)
	}
	defer func() {
		if err := occupant.Close(); err != nil {
			t.Errorf("release the test port: %v", err)
		}
	}()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = run([]string{
		"--addr", occupant.Addr().String(),
		"--core-base-url", "https://core:8443",
	}, quiet)
	if err == nil {
		t.Fatal("run() succeeded on an occupied address, want the listen error")
	}
}
