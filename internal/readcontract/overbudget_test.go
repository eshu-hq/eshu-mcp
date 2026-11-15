package readcontract

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOverBudgetErrorCarriesCap proves the wire refusal names the cap that
// fired while staying compatible with errors.Is(ErrOverBudget), and never
// echoes the over-cap body.
func TestOverBudgetErrorCarriesCap(t *testing.T) {
	t.Parallel()

	const budget = 16
	body := strings.Repeat("x", budget+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, body)
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig(srv.URL, "")
	cfg.ResponseByteBudget = budget
	client := mustClient(t, cfg)
	_, _, err := client.DoRaw(context.Background(), RawRequest{Method: "GET", Path: "/api/v0/repos"}, "")
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("DoRaw() error = %v, want ErrOverBudget", err)
	}
	var over *OverBudgetError
	if !errors.As(err, &over) {
		t.Fatalf("DoRaw() error type = %T, want *OverBudgetError", err)
	}
	if over.Budget != budget {
		t.Errorf("OverBudgetError.Budget = %d, want %d", over.Budget, budget)
	}
	if strings.Contains(err.Error(), body) {
		t.Error("over-budget error echoes the core body")
	}

	if _, err := client.Do(context.Background(), "repos", nil); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Do() error = %v, want ErrOverBudget", err)
	} else if !errors.As(err, &over) {
		t.Fatalf("Do() error type = %T, want *OverBudgetError", err)
	}
}

// TestOverBudgetBoundaryExactCapPasses proves the wire cap fires only past
// the limit for both read paths.
func TestOverBudgetBoundaryExactCapPasses(t *testing.T) {
	t.Parallel()

	const budget = 16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, strings.Repeat("x", budget))
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig(srv.URL, "")
	cfg.ResponseByteBudget = budget
	client := mustClient(t, cfg)
	if _, _, err := client.DoRaw(context.Background(), RawRequest{Method: "GET", Path: "/api/v0/repos"}, ""); err != nil {
		t.Fatalf("DoRaw() error = %v, want nil at exactly the cap", err)
	}
	if _, err := client.Do(context.Background(), "repos", nil); err != nil {
		t.Fatalf("Do() error = %v, want nil at exactly the cap", err)
	}
}
