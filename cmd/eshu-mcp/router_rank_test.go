// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// postFindTool calls find_tool through the server's message endpoint and
// decodes the JSON-RPC response. The caller passes the deployment credential:
// the edge refuses a headerless request before the body is decoded.
func postFindTool(t *testing.T, mux http.Handler, intent string) map[string]any {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_tool","arguments":{"intent":` +
		strconv.Quote(intent) + `}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer deploy-key")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /mcp/message find_tool = %d, want 200", rec.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode find_tool response: %v", err)
	}
	return decoded
}

// findCandidates reads the router-built candidates array out of a find_tool
// result. find_tool renders its own payload under structuredContent, not the
// dispatched {data, truth, error} envelope, so the test reads candidates
// there and never at the top level of the result.
func findCandidates(t *testing.T, decoded map[string]any) []any {
	t.Helper()
	result, ok := decoded["result"].(map[string]any)
	if !ok {
		t.Fatalf("find_tool response has no result: %v", decoded)
	}
	raw, err := json.Marshal(result["structuredContent"])
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if strings.Contains(string(raw), "mcp_router_ranker_unavailable") {
		t.Fatalf("find_tool answered ranker-unavailable on a ranker-wired surface: %s", raw)
	}
	if result["isError"] == true {
		t.Fatalf("find_tool isError = true, want ranked candidates: %s", raw)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent = %T, want an object", result["structuredContent"])
	}
	candidates, ok := structured["candidates"].([]any)
	if !ok {
		t.Fatalf("find_tool response has no candidates array: %v", structured)
	}
	return candidates
}

// TestBuildServerRouterSurfacesBootAndRank proves the live router and
// core_router surfaces boot with the ported ranker (no startup refusal) and
// rank a find_tool request: candidates come back, the order is deterministic
// across identical requests, and every candidate carries the catalog-free
// cost default.
func TestBuildServerRouterSurfacesBootAndRank(t *testing.T) {
	for _, tc := range []struct {
		surface    router.Surface
		advertised int
	}{
		{router.SurfaceRouter, 2},
		{router.SurfaceCoreRouter, 12},
	} {
		server, extra, summary := buildServer(quietLogger(), testClient(t), "https://core:8443",
			"deploy-key", tc.surface, resultshape.Mode(""), "")
		if summary.surface != tc.surface {
			t.Errorf("surface = %q, want %q", summary.surface, tc.surface)
		}
		if summary.advertised != tc.advertised {
			t.Errorf("surface %q advertises %d tools, want %d", tc.surface, summary.advertised, tc.advertised)
		}
		mux := server.Handler(extra)

		first := findCandidates(t, postFindTool(t, mux, "repository"))
		if len(first) == 0 {
			t.Fatalf("surface %q find_tool returned no candidates, want a ranked list", tc.surface)
		}
		second := findCandidates(t, postFindTool(t, mux, "repository"))
		firstRaw, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal first candidates: %v", err)
		}
		secondRaw, err := json.Marshal(second)
		if err != nil {
			t.Fatalf("marshal second candidates: %v", err)
		}
		if string(firstRaw) != string(secondRaw) {
			t.Errorf("surface %q find_tool order is not deterministic:\n%s\n%s", tc.surface, firstRaw, secondRaw)
		}
		for _, item := range first {
			candidate, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("candidate = %T, want an object", item)
			}
			for _, key := range []string{"name", "why", "cost", "inputSchema", "example", "score"} {
				if _, ok := candidate[key]; !ok {
					t.Fatalf("candidate %v missing %q", candidate, key)
				}
			}
			if candidate["cost"] != "high" {
				t.Errorf("candidate %v cost = %v, want %q (catalog-free default)", candidate["name"], candidate["cost"], "high")
			}
			name, ok := candidate["name"].(string)
			if !ok || name == "" {
				t.Errorf("candidate name = %v, want a non-empty string", candidate["name"])
			}
		}
	}
}
