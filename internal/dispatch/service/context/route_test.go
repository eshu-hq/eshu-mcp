package servicecontexttools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// TestRouteMatchesSeed re-resolves every sweep case this family claims and
// requires the identical request the source answers. Cases the family does
// not handle belong to another family and are skipped here. The sweep table
// is the verbatim source catalog_sweep_args.json at the seeded commit; the
// expectations are the source answers.
func TestRouteMatchesSeed(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	testdata := filepath.Join(filepath.Dir(thisFile), "../../testdata")

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

	claimed := 0
	for name, cases := range argsTable.Tools {
		want := expected[name]
		if len(want) != len(cases) {
			t.Fatalf("tool %q: %d seeded rows for %d sweep cases", name, len(want), len(cases))
		}
		for i, c := range cases {
			route, handled, routeErr := Route(name, routecontract.Arguments(c.Arguments))
			if routeErr != nil {
				t.Fatalf("tool %q case %q: Route error = %v, want nil (sweep cases resolve)", name, c.Label, routeErr)
			}
			if !handled {
				continue
			}
			claimed++
			got := map[string]any{"label": c.Label, "arguments": c.Arguments}
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
			if !wireEqual(got, want[i]) {
				t.Errorf("tool %q case %q: resolved request differs from the seeded answer", name, c.Label)
			}
		}
	}
	if claimed == 0 {
		t.Errorf("family claims no sweep case; the family moved or the table changed")
	}
}

// wireEqual compares two rows by their JSON wire form.
func wireEqual(got, want map[string]any) bool {
	gotJSON, err := json.Marshal(got)
	if err != nil {
		return false
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return false
	}
	var gotNorm, wantNorm any
	if err := json.Unmarshal(gotJSON, &gotNorm); err != nil {
		return false
	}
	if err := json.Unmarshal(wantJSON, &wantNorm); err != nil {
		return false
	}
	return reflect.DeepEqual(gotNorm, wantNorm)
}

// TestRouteRefusals proves the family refuses a missing service selector
// with the exact source errors instead of building a request with an empty
// path segment.
func TestRouteRefusals(t *testing.T) {
	t.Parallel()

	_, _, err := Route("get_service_intelligence_report", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "requires service_id, workload_id, or service_name") {
		t.Errorf("Route(missing selector) error = %v, want the selector refusal", err)
	}
	_, _, err = Route("investigate_service", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "requires service_id or service_name") {
		t.Errorf("Route(investigate_service) error = %v, want the selector refusal", err)
	}
}
