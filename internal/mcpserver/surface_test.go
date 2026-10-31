package mcpserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

func surfaceTestTools() []registry.ToolDefinition {
	return []registry.ToolDefinition{
		{Name: "find_tool"},
		{Name: "call_tool"},
		{Name: "search_code"},
	}
}

func TestProcessSurfaceDefaultsToFull(t *testing.T) {
	t.Parallel()

	s := NewServer(surfaceTestTools(), nil, nil, "test", "dev")
	if got := s.processSurface(); got != router.SurfaceFull {
		t.Fatalf("processSurface() = %q, want %q", got, router.SurfaceFull)
	}
}

func TestWithToolSurfaceNarrowsAdvertisement(t *testing.T) {
	t.Parallel()

	full := NewServer(surfaceTestTools(), nil, nil, "test", "dev")
	narrow := NewServer(surfaceTestTools(), nil, nil, "test", "dev", WithToolSurface(router.SurfaceRouter))
	ctx := context.Background()
	if got, want := len(full.advertisedTools(ctx)), len(narrow.advertisedTools(ctx)); got <= want {
		t.Fatalf("full=%d narrow=%d, want full to advertise more", got, want)
	}
	for _, tool := range narrow.advertisedTools(ctx) {
		if tool.Name != "find_tool" && tool.Name != "call_tool" {
			t.Fatalf("router surface advertises %q", tool.Name)
		}
	}
}

func TestToolSurfaceSummaryCounts(t *testing.T) {
	t.Parallel()

	s := NewServer(surfaceTestTools(), nil, nil, "test", "dev")
	surface, count, size := s.ToolSurfaceSummary()
	if surface != router.SurfaceFull {
		t.Fatalf("surface = %q, want full", surface)
	}
	if count != len(surfaceTestTools()) {
		t.Fatalf("count = %d, want %d", count, len(surfaceTestTools()))
	}
	if size <= 0 {
		t.Fatal("size = 0, want marshaled payload bytes")
	}
}

func TestProcessShapeDefaultsToDual(t *testing.T) {
	t.Parallel()

	s := NewServer(surfaceTestTools(), nil, nil, "test", "dev")
	if got := s.processShape(); got == "" {
		t.Fatal("processShape() is empty, want the dual default")
	}
}

func TestRunHTTPServesHealthUntilCancel(t *testing.T) {
	t.Parallel()

	s := NewServer(nil, nil, nil, "test", "dev")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.RunHTTP(ctx, addr, nil) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatalf("close body: %v", err)
			}
			if resp.StatusCode != 200 || string(body) != `{"status":"ok"}` {
				t.Fatalf("health = %d %q", resp.StatusCode, body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunHTTP = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunHTTP did not return after cancel")
	}
}

func TestWithResultShapeSetsMode(t *testing.T) {
	t.Parallel()

	s := NewServer(nil, nil, nil, "test", "dev", WithResultShape(resultshape.Structured))
	if got := s.processShape(); got != resultshape.Structured {
		t.Fatalf("processShape() = %q, want structured", got)
	}
}

func TestHandlerMountsExtra(t *testing.T) {
	t.Parallel()

	s := NewServer(nil, nil, nil, "test", "dev")
	extra := http.NewServeMux()
	extra.HandleFunc("GET /extra", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	mux := s.Handler(extra)
	req := httptest.NewRequest("GET", "/extra", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("extra mount status = %d", rec.Code)
	}
}
