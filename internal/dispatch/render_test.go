package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-mcp/internal/readcontract"
	"github.com/eshu-hq/eshu-mcp/internal/resultshape"
)

// hangingClient returns a read-contract client whose core never answers.
func hangingClient(t *testing.T, delay time.Duration) *readcontract.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(delay):
			if _, err := io.WriteString(w, `{}`); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return readcontract.NewClient(readcontract.DefaultConfig(srv.URL, ""))
}

// TestToolErrorResultShapes proves the error renderer types timeouts and
// wraps generic failures in an evidence-neutral envelope under structured.
// TestRenderUnmarshalableEnvelopeFallsBack proves an envelope that cannot
// marshal still renders: the embedded resource text falls back to empty
// while the summary and error flag stay intact.
func TestRenderUnmarshalableEnvelopeFallsBack(t *testing.T) {
	t.Parallel()

	poisoned := map[string]any{"callback": func() {}}
	result := &Result{
		Value:    poisoned,
		Envelope: &ResponseEnvelope{Data: poisoned},
		ToolName: "find_code",
	}
	got := RenderToolResult("find_code", result)
	if got.IsError {
		t.Errorf("IsError = true, want false for a successful render")
	}
	if len(got.Content) != 2 || got.Content[1].Resource == nil {
		t.Fatalf("content blocks = %+v, want text plus resource", got.Content)
	}
	if got.Content[1].Resource.Text != "" {
		t.Errorf("resource text = %q, want empty on marshal failure", got.Content[1].Resource.Text)
	}
	if got.Content[0].Text == "" {
		t.Error("summary text is empty, want the generic envelope summary")
	}
}

// TestRenderUnmarshalableValueFallsBack proves the same fallback for a plain
// payload without an envelope.
func TestRenderUnmarshalableValueFallsBack(t *testing.T) {
	t.Parallel()

	poisoned := map[string]any{"callback": func() {}}
	got := RenderToolResult("find_code", &Result{Value: poisoned, ToolName: "find_code"})
	if got.IsError {
		t.Errorf("IsError = true, want false for a successful render")
	}
	if len(got.Content) != 2 || got.Content[1].Resource == nil {
		t.Fatalf("content blocks = %+v, want text plus resource", got.Content)
	}
	if got.Content[1].Resource.URI != "eshu://tool-result/payload" {
		t.Errorf("resource URI = %q, want the payload resource", got.Content[1].Resource.URI)
	}
	if got.Content[1].Resource.Text != "" {
		t.Errorf("resource text = %q, want empty on marshal failure", got.Content[1].Resource.Text)
	}
}

func TestToolErrorResultShapes(t *testing.T) {
	t.Parallel()

	// A timed-out dispatch carries the canonical timeout envelope through
	// the same renderer the transport uses.
	_, err := Dispatch(context.Background(), hangingClient(t, 300*time.Millisecond), "find_code",
		map[string]any{"query": "x"}, "", nil, Options{Timeout: 50 * time.Millisecond, ResponseByteBudget: 1 << 20})
	if err == nil {
		t.Fatal("hanging dispatch succeeded, want the timeout error")
	}
	timeout := ToolErrorResultForCall(resultshape.Dual, "find_code", err, nil, 1<<20)
	if !timeout.IsError {
		t.Error("timeout result isError = false, want true")
	}
	body, marshalErr := json.Marshal(timeout.StructuredContent)
	if marshalErr != nil {
		t.Fatalf("marshal timeout structured content: %v", marshalErr)
	}
	if !strings.Contains(string(body), "mcp_dispatch_timeout") {
		t.Errorf("timeout structured content = %s, want the timeout envelope", body)
	}

	// A generic failure under structured gets the bounded error envelope,
	// never the raw error text alone.
	generic := ToolErrorResultForCall(resultshape.Structured, "find_code", errors.New("boom"), nil, 1<<20)
	body, marshalErr = json.Marshal(generic.StructuredContent)
	if marshalErr != nil {
		t.Fatalf("marshal generic structured content: %v", marshalErr)
	}
	if !strings.Contains(string(body), "mcp_dispatch_error") {
		t.Errorf("generic structured content = %s, want the error envelope", body)
	}
	for _, content := range generic.Content {
		if content.Resource != nil {
			t.Error("structured generic error carries an embedded resource, want text plus structured only")
		}
	}

	// The same failure under dual keeps its historic text bytes.
	dual := ToolErrorResultForCall(resultshape.Dual, "find_code", errors.New("boom"), nil, 1<<20)
	if !dual.IsError || len(dual.Content) == 0 {
		t.Errorf("dual generic error = %+v, want the text result", dual)
	}
	if dual.StructuredContent != nil {
		t.Errorf("dual generic error structured content = %v, want nil", dual.StructuredContent)
	}

	// ToolErrorResult is the no-budget shorthand for a transport failure.
	shorthand := ToolErrorResult(resultshape.Dual, errors.New("boom"))
	if !shorthand.IsError {
		t.Errorf("shorthand error = %+v, want the text result", shorthand)
	}
}
