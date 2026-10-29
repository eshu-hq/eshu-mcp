package supplychainimpacttools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	testdata := filepath.Join(filepath.Dir(thisFile), "../../../testdata")

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
			route, handled := Route(name, routecontract.Arguments(c.Arguments))
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
