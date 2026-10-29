package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// contractFile re-reads the pinned contract straight from the checkout,
// independent of the package under test: the test never trusts the
// implementation's own memory of the surface.
func contractFile(t *testing.T) contractRaw {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	path := filepath.Join(filepath.Dir(thisFile), "contracts", "tool-registry", "v1", "tools.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(tools.json) failed: %v", err)
	}
	var decoded contractRaw
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(tools.json) failed: %v", err)
	}
	return decoded
}

type contractRaw struct {
	ContractVersion string `json:"contract_version"`
	Registry        struct {
		ToolCount int      `json:"tool_count"`
		ToolOrder []string `json:"tool_order"`
	} `json:"registry"`
}

func TestContractVersion(t *testing.T) {
	t.Parallel()

	if got := ContractVersion(); got != "v1" {
		t.Fatalf("ContractVersion() = %q, want %q", got, "v1")
	}
}

func TestRegistryCountMatchesContract(t *testing.T) {
	t.Parallel()

	want := contractFile(t).Registry.ToolCount
	if got := Count(); got != want {
		t.Fatalf("Count() = %d, want contract tool_count %d", got, want)
	}
}

func TestRegistryOrderMatchesContract(t *testing.T) {
	t.Parallel()

	want := contractFile(t).Registry.ToolOrder
	got := ToolNames()
	if len(got) != len(want) {
		t.Fatalf("len(ToolNames()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ToolNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistryBoundaryNames(t *testing.T) {
	t.Parallel()

	got := ToolNames()
	if len(got) == 0 {
		t.Fatal("ToolNames() is empty")
	}
	if got[0] != "find_code" {
		t.Errorf("ToolNames()[0] = %q, want %q", got[0], "find_code")
	}
	if last := got[len(got)-1]; last != "list_runtime_version_declarations" {
		t.Errorf("ToolNames()[last] = %q, want %q", last, "list_runtime_version_declarations")
	}
}

func TestRegistryNamesUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int, len(ToolNames()))
	for i, name := range ToolNames() {
		if first, dup := seen[name]; dup {
			t.Fatalf("ToolNames()[%d] = %q duplicates index %d", i, name, first)
		}
		seen[name] = i
	}
}
