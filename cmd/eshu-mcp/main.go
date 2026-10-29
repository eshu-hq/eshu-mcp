package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/eshu-hq/eshu-mcp"
)

// server holds the dependencies of the pre-cutover serving surface. Tool
// dispatch over the read-contract clients lands next; until then the server
// carries no credential and makes no core calls.
type server struct {
	coreBaseURL string
}

func newServer(coreBaseURL string) *server {
	return &server{coreBaseURL: coreBaseURL}
}

// health reports the served contract version and registry size. It takes no
// credential and reveals none.
func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"contract": registry.ContractVersion(),
		"tools":    registry.Count(),
		"core":     s.coreBaseURL,
	}); err != nil {
		http.Error(w, "encode health response", http.StatusInternalServerError)
	}
}

func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	return mux
}

func run(args []string, logOut *slog.Logger) error {
	flags := flag.NewFlagSet("eshu-mcp", flag.ContinueOnError)
	addr := flags.String("addr", ":8080", "listen address for the health surface")
	coreBaseURL := flags.String("core-base-url", "", "core read-API origin, for example https://core:8443")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *coreBaseURL == "" {
		return fmt.Errorf("eshu-mcp: --core-base-url is required")
	}
	srv := newServer(*coreBaseURL)
	logOut.Info("eshu-mcp serving",
		"contract", registry.ContractVersion(),
		"tools", registry.Count(),
		"addr", *addr,
	)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return httpServer.ListenAndServe()
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("eshu-mcp exited", "error", err)
		os.Exit(1)
	}
}
