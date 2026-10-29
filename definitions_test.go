package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDefinitionsMatchRegistryContract proves the served definitions are the
// contract the registry advertises: same 169 names in the same order, the
// router pair intact, every example keyed to a registered tool, and the
// profile and core-router sets drawn from real tool names.
func TestDefinitionsMatchRegistryContract(t *testing.T) {
	t.Parallel()

	names := ToolNames()
	defs := Definitions()
	if len(defs) != len(names) {
		t.Fatalf("len(Definitions()) = %d, want %d registry names", len(defs), len(names))
	}
	for i, tool := range defs {
		if tool.Name != names[i] {
			t.Fatalf("Definitions()[%d].Name = %q, want %q", i, tool.Name, names[i])
		}
		if tool.Description == "" {
			t.Errorf("Definitions()[%d] has an empty description", i)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Errorf("Definitions()[%d] schema is not an object map", i)
		}
	}

	pair := RouterTools()
	if len(pair) != 2 || pair[0].Name != "find_tool" || pair[1].Name != "call_tool" {
		t.Fatalf("RouterTools() = %v, want [find_tool call_tool]", pair)
	}

	known := map[string]bool{}
	for _, name := range names {
		known[name] = true
	}
	for _, name := range CoreRouterNames() {
		if !known[name] {
			t.Errorf("core router name %q is not a registered tool", name)
		}
	}
	for name := range ProductionHidden() {
		if !known[name] {
			t.Errorf("production-hidden tool %q is not a registered tool", name)
		}
	}
	for _, name := range names {
		if _, ok := Example(name); !ok {
			t.Errorf("Example(%q) missing, want a curated example for every tool", name)
		}
	}
}

// TestDefinitionsWireLossless proves the definitions survive the JSON
// round-trip losslessly: re-marshaling a loaded schema or example yields the
// seeded bytes, so tools/list and invalid-argument examples match source
// byte for byte.
func TestDefinitionsWireLossless(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	path := filepath.Join(filepath.Dir(thisFile), "contracts", "tool-registry", "v1", "tool_definitions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(tool_definitions.json) failed: %v", err)
	}
	var seeded struct {
		RegistryTools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"registry_tools"`
		RouterTools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"router_tools"`
		Examples map[string]map[string]any `json:"examples"`
	}
	if err := json.Unmarshal(raw, &seeded); err != nil {
		t.Fatalf("json.Unmarshal(tool_definitions.json) failed: %v", err)
	}

	served := Definitions()
	if len(seeded.RegistryTools) != len(served) {
		t.Fatalf("seeded registry tools = %d, served = %d", len(seeded.RegistryTools), len(served))
	}
	for i, want := range seeded.RegistryTools {
		wantJSON, err := json.Marshal(want.InputSchema)
		if err != nil {
			t.Fatalf("marshal seeded schema for %q: %v", want.Name, err)
		}
		got, ok := served[i].InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("served schema for %q is %T, want map", want.Name, served[i].InputSchema)
		}
		gotJSON, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal served schema for %q: %v", want.Name, err)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("schema for %q changed across the round-trip:\n got: %s\nwant: %s", want.Name, gotJSON, wantJSON)
		}
	}
}

// TestDefinitionsExamplesWireLossless proves seeded examples re-marshal
// identically, so invalid-argument error details match source byte for byte.
func TestDefinitionsExamplesWireLossless(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	path := filepath.Join(filepath.Dir(thisFile), "contracts", "tool-registry", "v1", "tool_definitions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(tool_definitions.json) failed: %v", err)
	}
	var seeded struct {
		Examples map[string]map[string]any `json:"examples"`
	}
	if err := json.Unmarshal(raw, &seeded); err != nil {
		t.Fatalf("json.Unmarshal(tool_definitions.json) failed: %v", err)
	}
	for name, want := range seeded.Examples {
		got, ok := Example(name)
		if !ok {
			t.Errorf("Example(%q) missing", name)
			continue
		}
		wantJSON, marshalErr := json.Marshal(want)
		if marshalErr != nil {
			t.Fatalf("marshal want example for %q: %v", name, marshalErr)
		}
		gotJSON, marshalErr := json.Marshal(got)
		if marshalErr != nil {
			t.Fatalf("marshal got example for %q: %v", name, marshalErr)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("example for %q changed across the round-trip:\n got: %s\nwant: %s", name, gotJSON, wantJSON)
		}
	}
}

// TestRegistryAccessors proves the lookup accessors: a known definition,
// required-argument order, unknown names, and source bounds.
func TestRegistryAccessors(t *testing.T) {
	t.Parallel()

	definition, ok := Definition("search_dockerfile_installs")
	if !ok {
		t.Fatal("Definition(search_dockerfile_installs) missing")
	}
	if definition.Description == "" {
		t.Error("Definition description is empty")
	}
	if _, ok := Definition("no_such_tool"); ok {
		t.Error("Definition(unknown) found, want missing")
	}
	if got := RequiredArguments("search_dockerfile_installs"); len(got) != 1 || got[0] != "package" {
		t.Errorf("RequiredArguments = %v, want [package]", got)
	}
	if got := RequiredArguments("no_such_tool"); got != nil {
		t.Errorf("RequiredArguments(unknown) = %v, want nil", got)
	}
	maxOffset, mime, protocols := SourceBounds()
	if maxOffset != 10000 {
		t.Errorf("SourceBounds max offset = %d, want 10000", maxOffset)
	}
	if mime != "application/eshu.envelope+json" {
		t.Errorf("SourceBounds MIME = %q, want the envelope MIME", mime)
	}
	if len(protocols) != 1 || protocols[0] != "2024-11-05" {
		t.Errorf("SourceBounds protocols = %v, want [2024-11-05]", protocols)
	}
}
