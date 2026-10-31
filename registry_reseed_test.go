package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRegistryReseed171 pins the #9303 reseed plus the #9670 appendix: the
// contract carries all 171 source tools in registration order. The first 169
// come from the origin/main 9922b49 seed facts (TestZZEmitSeedFacts at that
// commit), closing the stale-168 defect that rode the skeleton; entries 170
// and 171 are the two tools the source census appended after the seed
// (search_php_configuration, list_ssm_host_inventory), ported verbatim from
// the in-tree census tail. This test locks the rendered result.
func TestRegistryReseed171(t *testing.T) {
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
	if decoded.Source.CensusCountConstant != 171 {
		t.Errorf("source.census_count_constant = %d, want 171", decoded.Source.CensusCountConstant)
	}
	if decoded.Registry.ToolCount != 171 {
		t.Errorf("registry.tool_count = %d, want 171", decoded.Registry.ToolCount)
	}
	if len(decoded.Registry.ToolOrder) != 171 {
		t.Fatalf("len(tool_order) = %d, want 171", len(decoded.Registry.ToolOrder))
	}
	if tail := decoded.Registry.ToolOrder[169:]; len(tail) != 2 || tail[0] != "search_php_configuration" || tail[1] != "list_ssm_host_inventory" {
		t.Errorf("tool_order[169:] = %q, want the #9670 appendix tail", tail)
	}
	if decoded.Source.OwnershipMapSHA256 == "71b005ee7305ca0768f38694ac6de237901d963df75d4077dd57e189bfadc89f" {
		t.Errorf("ownership_map_sha256 still pins the e4c385f4 map; resync docs/source/CUTOVER_OWNERSHIP.md to 9922b49")
	}
}
