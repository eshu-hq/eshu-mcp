package router

import (
	"context"
	"testing"

	registry "github.com/eshu-hq/eshu-mcp"
)

// stubRanker records the tools Prepare hands it and satisfies Ranker.
type stubRanker struct {
	tools []registry.ToolDefinition
}

func (s *stubRanker) Rank(context.Context, Query, []registry.ToolDefinition, func(string) bool) ([]Candidate, error) {
	return nil, nil
}

func TestParseSurfaceCases(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]Surface{
		"":            SurfaceFull,
		"full":        SurfaceFull,
		"FULL":        SurfaceFull,
		"router":      SurfaceRouter,
		"Router":      SurfaceRouter,
		"core_router": SurfaceCoreRouter,
	} {
		got, err := ParseSurface(raw)
		if err != nil {
			t.Fatalf("ParseSurface(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseSurface(%q) = %q, want %q", raw, got, want)
		}
	}
	for _, raw := range []string{" full", "full ", "everything", "find_tool"} {
		if _, err := ParseSurface(raw); err == nil {
			t.Fatalf("ParseSurface(%q) passed, want an error", raw)
		}
	}
}

func TestPrepareBuildsRankerOnce(t *testing.T) {
	t.Parallel()

	tools := registry.Definitions()
	var built *stubRanker
	h := &Host{NewRanker: func(got []registry.ToolDefinition) Ranker {
		if len(got) != len(tools) {
			t.Fatalf("builder got %d tools, want %d", len(got), len(tools))
		}
		built = &stubRanker{tools: got}
		return built
	}}
	h.Prepare(tools, nil)
	if h.Ranker != Ranker(built) {
		t.Fatal("Prepare did not install the built ranker")
	}
	other := &stubRanker{}
	h.Ranker = other
	h.Prepare(tools, nil)
	if h.Ranker != Ranker(other) {
		t.Fatal("Prepare replaced a preset ranker")
	}
}

func TestPrepareNilBuilderIsNoOp(t *testing.T) {
	t.Parallel()

	h := &Host{}
	h.Prepare(registry.Definitions(), nil)
	if h.Ranker != nil {
		t.Fatal("Prepare with nil NewRanker set a ranker")
	}
}

func TestEngineExampleClone(t *testing.T) {
	t.Parallel()

	e := NewEngine(Config{Registry: registry.Definitions()})
	name := ""
	for _, tool := range registry.Definitions() {
		if _, ok := registry.Example(tool.Name); ok {
			name = tool.Name
			break
		}
	}
	if name == "" {
		t.Skip("no registered tool carries an example")
	}
	first, ok := e.Example(name)
	if !ok {
		t.Fatalf("Example(%q) missing", name)
	}
	first["injected"] = true
	second, ok := e.Example(name)
	if !ok {
		t.Fatalf("Example(%q) missing on second read", name)
	}
	if _, ok := second["injected"]; ok {
		t.Fatal("Example returns shared state, want a copy")
	}
	if _, ok := e.Example("no_such_tool"); ok {
		t.Fatal("Example(no_such_tool) hit, want a miss")
	}
}

func TestHostServeFindAndCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	newConfig := func() Config {
		return Config{Registry: registry.Definitions()}
	}
	h := &Host{Ranker: &stubRanker{}}
	findOut := h.Serve(ctx, newConfig, Request{}, FindToolName, map[string]any{"intent": "x"})
	if findOut == nil {
		t.Fatal("Serve(find) returned nil")
	}
	// Second call reuses the built engine (once.Do) and exercises Call.
	h2 := &Host{Ranker: &stubRanker{}}
	callOut := h2.Serve(ctx, newConfig, Request{Dispatch: testDispatch("v")},
		"other", map[string]any{"name": "count_repositories_by_language", "arguments": map[string]any{"language": "go"}})
	if callOut != "v" {
		t.Fatalf("Serve(call) = %#v, want the dispatched value", callOut)
	}
}
