package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRegistryReseedPartC pins the fix-07 part-C reseed: the contract
// carries all 173 source tools in registration order from the part-B merge
// commit (TestZZEmitSeedFacts at that commit). The former #9670, #10477,
// and #10535 appendix tools are in-census rows now; the #10477/#10535 pair
// sits at positions 117-118 in source registration order, and the tail is
// the pre-appendix census tail. This test locks the rendered result.
func TestRegistryReseedPartC(t *testing.T) {
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
	if decoded.Source.Commit != "93f481e3d261cb5b126d765834b4d688b67693b3" {
		t.Errorf("source.commit = %q, want the fix-07 part-B merge 93f481e3d2", decoded.Source.Commit)
	}
	if decoded.Source.CensusCountConstant != 173 {
		t.Errorf("source.census_count_constant = %d, want 173", decoded.Source.CensusCountConstant)
	}
	if decoded.Registry.ToolCount != 173 {
		t.Errorf("registry.tool_count = %d, want 173", decoded.Registry.ToolCount)
	}
	if len(decoded.Registry.ToolOrder) != 173 {
		t.Fatalf("len(tool_order) = %d, want 173", len(decoded.Registry.ToolOrder))
	}
	if pair := decoded.Registry.ToolOrder[117:119]; len(pair) != 2 || pair[0] != "list_service_api_endpoints" || pair[1] != "list_service_downstream_consumers" {
		t.Errorf("tool_order[117:119] = %q, want the in-census #10477/#10535 pair", pair)
	}
	if tail := decoded.Registry.ToolOrder[169:]; len(tail) != 4 || tail[0] != "list_runtime_version_declarations" || tail[1] != "search_dockerfile_installs" || tail[2] != "search_php_configuration" || tail[3] != "list_ssm_host_inventory" {
		t.Errorf("tool_order[169:] = %q, want the pre-appendix census tail", tail)
	}
	if decoded.Source.OwnershipMapSHA256 != "ed6909ca4169804a120a14674842c11c111176a32d4ae92a832d9b88a874cf72" {
		t.Errorf("ownership_map_sha256 = %q, want the go/cmd/api map at 93f481e3d2; resync docs/source/CUTOVER_OWNERSHIP.md", decoded.Source.OwnershipMapSHA256)
	}
}
