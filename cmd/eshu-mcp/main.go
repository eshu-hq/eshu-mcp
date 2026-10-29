package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	eshuregistry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/dispatch"
	"github.com/eshu-hq/eshu-mcp/internal/mcpserver"
	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// version identifies this build in initialize serverInfo. Release wiring
// sets it with -ldflags "-X main.version=..."; local builds report dev.
var version = "dev"

// isTruthy mirrors the core truthy parsing for boolean env knobs:
// 1, true, t, yes, y, on (case-insensitive, trimmed).
func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func run(args []string, logOut *slog.Logger) error {
	flags := flag.NewFlagSet("eshu-mcp", flag.ContinueOnError)
	addr := flags.String("addr", ":8080", "listen address for the MCP surface")
	coreBaseURL := flags.String("core-base-url", "", "core read-API origin, for example https://core:8443")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *coreBaseURL == "" {
		return fmt.Errorf("eshu-mcp: --core-base-url is required")
	}

	getenv := func(key string) string { return os.Getenv(key) }

	// The destination serves HTTP only: stdio is the local embedded flow in
	// core, while this binary is a network service. Refuse a stdio request
	// instead of silently serving HTTP.
	if transport := strings.ToLower(strings.TrimSpace(getenv("ESHU_MCP_TRANSPORT"))); transport != "" && transport != "http" {
		return fmt.Errorf("eshu-mcp: ESHU_MCP_TRANSPORT=%q is not supported (want http or unset)", transport)
	}

	apiKey := getenv("ESHU_API_KEY")
	allowUnauthenticated := isTruthy(getenv("ESHU_MCP_ALLOW_UNAUTHENTICATED"))

	surface, err := router.ParseSurface(strings.TrimSpace(getenv("ESHU_MCP_TOOL_SURFACE")))
	if err != nil {
		return fmt.Errorf("eshu-mcp: invalid ESHU_MCP_TOOL_SURFACE: %w", err)
	}
	shape, err := resultshape.Parse(strings.TrimSpace(getenv("ESHU_MCP_RESULT_SHAPE")))
	if err != nil {
		return fmt.Errorf("eshu-mcp: invalid ESHU_MCP_RESULT_SHAPE: %w", err)
	}
	profile := strings.TrimSpace(getenv("ESHU_MCP_QUERY_PROFILE"))

	// No silent open mode over HTTP: refuse to start with no credential
	// posture unless the operator explicitly opts into the loopback escape
	// hatch. The edge forwards the caller credential to core on every
	// dispatch; core remains the admission authority.
	if err := mcpserver.RequireCredentialSource(apiKey != "", allowUnauthenticated); err != nil {
		return err
	}
	// A surface that advertises find_tool as its entry point with no ranker
	// is an error, so a misconfigured deployment fails at startup instead of
	// answering mcp_router_ranker_unavailable on its first request. The full
	// surface still serves: find_tool stays callable there.
	if surface == router.SurfaceRouter || surface == router.SurfaceCoreRouter {
		return fmt.Errorf("eshu-mcp: tool surface %q serves find_tool, but no find_tool ranker is built yet (ranker increment under #9498)", surface)
	}

	if allowUnauthenticated {
		logOut.Warn("ESHU_MCP_ALLOW_UNAUTHENTICATED=true: MCP HTTP transport starting without edge credential check. " +
			"Every dispatch still forwards the caller credential to core for admission. Dev/loopback use only; " +
			"never expose this port publicly with the escape hatch set.")
	}
	server, extra, summary := buildServer(logOut, *coreBaseURL, apiKey, surface, shape, profile)
	logOut.Info("eshu-mcp serving",
		"contract", eshuregistry.ContractVersion(),
		"tools", summary.advertised,
		"tool_surface", string(summary.surface),
		"tools_list_bytes", summary.listBytes,
		"core", *coreBaseURL,
		"addr", *addr,
	)
	return server.RunHTTP(context.Background(), *addr, extra)
}

// serverSummary is the startup advertisement the operator confirms.
type serverSummary struct {
	surface    router.Surface
	advertised int
	listBytes  int
}

// buildServer wires the registry, dispatch boundary, transport, and auth
// without binding a listener, so tests can prove the wiring serves the
// pinned surface. It fails closed on a router surface without a ranker.
func buildServer(logOut *slog.Logger, coreBaseURL, apiKey string, surface router.Surface, shape resultshape.Mode, profile string) (*mcpserver.Server, *http.ServeMux, serverSummary) {
	tools := eshuregistry.Definitions()
	if mcpserver.IsProductionProfile(profile) {
		hidden := eshuregistry.ProductionHidden()
		kept := make([]eshuregistry.ToolDefinition, 0, len(tools))
		for _, tool := range tools {
			if !hidden[tool.Name] {
				kept = append(kept, tool)
			}
		}
		tools = kept
	}

	client := readcontract.NewClient(readcontract.DefaultConfig(coreBaseURL, ""))
	dispatchFunc := func(ctx context.Context, name string, args map[string]any, authHeader string) (any, error) {
		result, err := dispatch.Dispatch(ctx, client, name, args, authHeader, logOut, dispatch.DefaultOptions())
		if err != nil {
			return nil, err
		}
		return dispatch.RenderToolResult(name, result), nil
	}

	opts := []mcpserver.ServerOption{
		mcpserver.WithToolSurface(surface),
		mcpserver.WithResultShape(shape),
	}
	if apiKey != "" {
		opts = append(opts, mcpserver.WithTransportAuth(mcpserver.EdgeAuthMiddleware(apiKey, logOut)))
	}
	// The escape-hatch caller passes apiKey "" with transport auth off: every
	// dispatch still forwards the caller credential to core for admission.
	server := mcpserver.NewServer(tools, dispatchFunc, logOut, "eshu-mcp", version, opts...)

	surfaceName, advertised, listBytes := server.ToolSurfaceSummary()

	extra := http.NewServeMux()
	extra.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"contract": eshuregistry.ContractVersion(),
			"tools":    advertised,
			"core":     coreBaseURL,
		}); err != nil {
			return
		}
	})
	return server, extra, serverSummary{surface: surfaceName, advertised: advertised, listBytes: listBytes}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("eshu-mcp exited", "error", err)
		os.Exit(1)
	}
}
