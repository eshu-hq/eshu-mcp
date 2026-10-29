package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// The sweep table is a verbatim copy of the source catalog_sweep_args.json
// at 9922b49; the expectations are the source ResolveRoute answers for those
// arguments, emitted by the seed. The test re-resolves every case through
// the ported selectors and requires the identical request (or the identical
// refusal), so any selector drift fails the build.
func TestResolveSweepMatchesSeed(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	testdata := filepath.Join(filepath.Dir(thisFile), "testdata")

	argsRaw, err := os.ReadFile(filepath.Join(testdata, "sweep_args.json"))
	if err != nil {
		t.Fatalf("read sweep_args.json: %v", err)
	}
	var argsTable struct {
		Tools map[string][]struct {
			Label     string         `json:"label"`
			Arguments map[string]any `json:"arguments"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(argsRaw, &argsTable); err != nil {
		t.Fatalf("parse sweep_args.json: %v", err)
	}

	expectRaw, err := os.ReadFile(filepath.Join(testdata, "sweep_resolutions.json"))
	if err != nil {
		t.Fatalf("read sweep_resolutions.json: %v", err)
	}
	var expected map[string][]map[string]any
	if err := json.Unmarshal(expectRaw, &expected); err != nil {
		t.Fatalf("parse sweep_resolutions.json: %v", err)
	}

	if len(argsTable.Tools) != 169 {
		t.Fatalf("sweep table covers %d tools, want all 169", len(argsTable.Tools))
	}
	for name, cases := range argsTable.Tools {
		want, ok := expected[name]
		if !ok {
			t.Errorf("tool %q has sweep cases but no seeded expectation", name)
			continue
		}
		if len(want) != len(cases) {
			t.Errorf("tool %q: %d seeded rows for %d sweep cases", name, len(want), len(cases))
			continue
		}
		for i, c := range cases {
			route, err := ResolveRoute(name, c.Arguments)
			got := map[string]any{"label": c.Label, "arguments": c.Arguments}
			if err != nil {
				got["error"] = err.Error()
			} else {
				got["method"] = route.Method
				got["path"] = route.Path
				if route.Query != nil {
					got["query"] = route.Query
				}
				if route.Body != nil {
					got["body"] = route.Body
				} else {
					got["body"] = nil
				}
			}
			if !wireEqual(t, got, want[i]) {
				gotJSON, gotErr := json.Marshal(got)
				wantJSON, wantErr := json.Marshal(want[i])
				if gotErr != nil || wantErr != nil {
					t.Errorf("tool %q case %q: remarshal failed: %v %v", name, c.Label, gotErr, wantErr)
				} else {
					t.Errorf("tool %q case %q:\n got: %s\nwant: %s", name, c.Label, gotJSON, wantJSON)
				}
			}
		}
	}
}

// wireEqual compares two resolution rows by their JSON wire form, so an int
// limit and a float64 limit from the seeded JSON compare equal when they
// encode identically.
func wireEqual(t *testing.T, got, want map[string]any) bool {
	t.Helper()

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal resolved row: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected row: %v", err)
	}
	var gotNorm, wantNorm any
	if err := json.Unmarshal(gotJSON, &gotNorm); err != nil {
		t.Fatalf("reparse resolved row: %v", err)
	}
	if err := json.Unmarshal(wantJSON, &wantNorm); err != nil {
		t.Fatalf("reparse expected row: %v", err)
	}
	return reflect.DeepEqual(gotNorm, wantNorm)
}

// TestResolveUnknownTool pins the refusal for a name no route claims: the
// dispatch error a client has always seen.
func TestResolveUnknownTool(t *testing.T) {
	t.Parallel()

	_, err := ResolveRoute("no_such_tool_registered", map[string]any{})
	if err == nil {
		t.Fatal("ResolveRoute(unknown) succeeded, want the unknown-tool refusal")
	}
	if got, want := err.Error(), "unknown tool: no_such_tool_registered"; got != want {
		t.Fatalf("ResolveRoute(unknown) error = %q, want %q", got, want)
	}
}
