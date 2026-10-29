package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRegistryReseed169 pins the #9303 reseed: the contract carries all 169
// source tools in registration order from origin/main 9922b49, closing the
// stale-168 defect that rode the skeleton. The seed facts come from
// TestZZEmitSeedFacts at that commit; this test locks the rendered result.
func TestRegistryReseed169(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "contracts", "tool-registry", "v1", "tools.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(tools.json) failed: %v", err)
	}
	var decoded struct {
		Source struct {
			Commit              string `json:"commit"`
			CensusCountConstant int    `json:"census_count_constant"`
			OwnershipMapSHA256  string `json:"ownership_map_sha256"`
		} `json:"source"`
		Registry struct {
			ToolCount int      `json:"tool_count"`
			ToolOrder []string `json:"tool_order"`
		} `json:"registry"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(tools.json) failed: %v", err)
	}
	if decoded.Source.Commit != "9922b492d8a3a25ef87b39d38864b36ff522a6ce" {
		t.Errorf("source.commit = %q, want origin/main 9922b49", decoded.Source.Commit)
	}
	if decoded.Source.CensusCountConstant != 169 {
		t.Errorf("source.census_count_constant = %d, want 169", decoded.Source.CensusCountConstant)
	}
	if decoded.Registry.ToolCount != 169 {
		t.Errorf("registry.tool_count = %d, want 169", decoded.Registry.ToolCount)
	}
	if len(decoded.Registry.ToolOrder) != 169 {
		t.Fatalf("len(tool_order) = %d, want 169", len(decoded.Registry.ToolOrder))
	}
	if last := decoded.Registry.ToolOrder[168]; last != "search_dockerfile_installs" {
		t.Errorf("tool_order[168] = %q, want the #9303 tail tool", last)
	}
	if decoded.Source.OwnershipMapSHA256 == "71b005ee7305ca0768f38694ac6de237901d963df75d4077dd57e189bfadc89f" {
		t.Errorf("ownership_map_sha256 still pins the e4c385f4 map; resync docs/source/CUTOVER_OWNERSHIP.md to 9922b49")
	}
}
