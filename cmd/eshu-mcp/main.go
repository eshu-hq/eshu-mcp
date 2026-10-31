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
	"github.com/eshu-hq/eshu-mcp/internal/router/rank"
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
	getenv := func(key string) string { return os.Getenv(key) }
	addr, server, extra, err := wireServer(args, getenv, logOut)
	if err != nil {
		return err
	}
	return server.RunHTTP(context.Background(), addr, extra)
}

// wireServer parses flags and operator env and wires the full serving
// stack without binding a listener, so tests can drive the exact boot
// path run serves: any wiring break here fails the wire-level tests
// rather than hiding behind a unit-tested predicate.
func wireServer(args []string, getenv func(string) string, logOut *slog.Logger) (string, *mcpserver.Server, *http.ServeMux, error) {
	flags := flag.NewFlagSet("eshu-mcp", flag.ContinueOnError)
	addr := flags.String("addr", ":8080", "listen address for the MCP surface")
	coreBaseURL := flags.String("core-base-url", "", "core read-API origin, for example https://core:8443")
	if err := flags.Parse(args); err != nil {
		return "", nil, nil, err
	}
	if *coreBaseURL == "" {
		return "", nil, nil, fmt.Errorf("eshu-mcp: --core-base-url is required")
	}

	// The destination serves HTTP only: stdio is the local embedded flow in
	// core, while this binary is a network service. Refuse a stdio request
	// instead of silently serving HTTP.
	if transport := strings.ToLower(strings.TrimSpace(getenv("ESHU_MCP_TRANSPORT"))); transport != "" && transport != "http" {
		return "", nil, nil, fmt.Errorf("eshu-mcp: ESHU_MCP_TRANSPORT=%q is not supported (want http or unset)", transport)
	}

	apiKey := getenv("ESHU_API_KEY")
	allowUnauthenticated := isTruthy(getenv("ESHU_MCP_ALLOW_UNAUTHENTICATED"))
	scopedTokensFile := getenv(mcpserver.ScopedTokensFileEnv)
	resourceURI := getenv(mcpserver.ResourceURIEnv)
	credentialConfigured := mcpserver.CredentialSourceConfigured(apiKey, scopedTokensFile, resourceURI)

	surface, err := router.ParseSurface(strings.TrimSpace(getenv("ESHU_MCP_TOOL_SURFACE")))
	if err != nil {
		return "", nil, nil, fmt.Errorf("eshu-mcp: invalid ESHU_MCP_TOOL_SURFACE: %w", err)
	}
	shape, err := resultshape.Parse(strings.TrimSpace(getenv("ESHU_MCP_RESULT_SHAPE")))
	if err != nil {
		return "", nil, nil, fmt.Errorf("eshu-mcp: invalid ESHU_MCP_RESULT_SHAPE: %w", err)
	}
	profile := strings.TrimSpace(getenv("ESHU_MCP_QUERY_PROFILE"))

	// No silent open mode over HTTP: refuse to start with no credential
	// posture unless the operator explicitly opts into the loopback escape
	// hatch. The posture folds the same three knobs the monolith counts
	// (shared key, scoped-token file, OIDC resource URI); the edge
	// forwards the caller credential to core on every dispatch, and core
	// remains the admission authority.
	if err := mcpserver.RequireCredentialSource(credentialConfigured, allowUnauthenticated); err != nil {
		return "", nil, nil, err
	}
	// Fail fast when the embedded registry did not load: serving health
	// with an empty contract and zero tools would be a false-green signal.
	if err := eshuregistry.LoadError(); err != nil {
		return "", nil, nil, fmt.Errorf("eshu-mcp: registry load failed: %w", err)
	}

	if allowUnauthenticated {
		logOut.Warn("ESHU_MCP_ALLOW_UNAUTHENTICATED=true: MCP HTTP transport starting without edge credential check. " +
			"Every dispatch still forwards the caller credential to core for admission. Dev/loopback use only; " +
			"never expose this port publicly with the escape hatch set.")
	}
	client, err := readcontract.NewClient(readcontract.DefaultConfig(*coreBaseURL, ""))
	if err != nil {
		return "", nil, nil, fmt.Errorf("eshu-mcp: invalid core client config: %w", err)
	}
	discovery := mcpserver.DiscoveryConfigFromEnv(getenv)
	if discovery.Enabled() {
		logOut.Info("oauth discovery enabled",
			"resource", discovery.Resource,
			"metadata_url", discovery.MetadataURL)
	}
	server, extra, summary := buildServer(logOut, client, *coreBaseURL, apiKey, discovery, credentialConfigured, surface, shape, profile)
	logOut.Info("eshu-mcp serving",
		"contract", eshuregistry.ContractVersion(),
		"tools", summary.advertised,
		"tool_surface", string(summary.surface),
		"tools_list_bytes", summary.listBytes,
		"core", *coreBaseURL,
		"addr", *addr,
	)
	return *addr, server, extra, nil
}

// serverSummary is the startup advertisement the operator confirms.
type serverSummary struct {
	surface    router.Surface
	advertised int
	listBytes  int
}

// buildServer wires the registry, dispatch boundary, transport, and auth
// without binding a listener, so tests can prove the wiring serves the
// pinned surface. It wires the catalog-free lexical ranker, so the router
// and core_router surfaces boot with find_tool ranking. The core client
// arrives built: wireServer rejects an invalid config before any listener
// starts. authEnforced mirrors the monolith: transport auth is wired
// whenever any credential knob is set, so a headerless request is denied;
// only the escape-hatch-open deployment with no source serves open.
func buildServer(logOut *slog.Logger, client *readcontract.Client, coreBaseURL, apiKey string, discovery mcpserver.DiscoveryConfig, authEnforced bool, surface router.Surface, shape resultshape.Mode, profile string) (*mcpserver.Server, *http.ServeMux, serverSummary) {
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
		mcpserver.WithRouterRanker(func(tools []eshuregistry.ToolDefinition) router.Ranker {
			return rank.New(tools)
		}),
	}
	if authEnforced {
		opts = append(opts, mcpserver.WithTransportAuth(
			mcpserver.EdgeAuthMiddlewareWithChallenge(apiKey, discovery.ChallengeValue(), logOut)))
	}
	// The escape-hatch caller passes apiKey "" with transport auth off: every
	// dispatch still forwards the caller credential to core for admission.
	server := mcpserver.NewServer(tools, dispatchFunc, logOut, "eshu-mcp", version, opts...)

	surfaceName, advertised, listBytes := server.ToolSurfaceSummary()

	extra := http.NewServeMux()
	discovery.Mount(extra)
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
