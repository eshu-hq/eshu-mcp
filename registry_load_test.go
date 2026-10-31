package registry

import (
	"testing"
)

// TestRegistryLoadErrorAndVersion pins the embedded-registry load path:
// a healthy tree loads without error and reports the pinned contract
// version, so health can never serve an empty contract.
func TestRegistryLoadErrorAndVersion(t *testing.T) {
	t.Parallel()

	if err := LoadError(); err != nil {
		t.Fatalf("LoadError() = %v", err)
	}
	if got := ContractVersion(); got == "" {
		t.Fatal("ContractVersion() is empty")
	}
	if n := len(Definitions()); n == 0 {
		t.Fatal("Definitions() is empty")
	}
}

func TestRegistryLookupMissesAndCopies(t *testing.T) {
	t.Parallel()

	if _, ok := Definition("no_such_tool"); ok {
		t.Fatal("Definition miss hit")
	}
	if _, ok := Example("no_such_tool"); ok {
		t.Fatal("Example miss hit")
	}
	if RequiredArguments("no_such_tool") != nil {
		t.Fatal("RequiredArguments miss non-nil")
	}
	defs := Definitions()
	if len(defs) == 0 {
		t.Fatal("empty registry")
	}
	defs[0].Name = "mutated"
	if Definitions()[0].Name == "mutated" {
		t.Fatal("Definitions shares state")
	}
	rt := RouterTools()
	if len(rt) == 0 {
		t.Fatal("no router tools")
	}
	rt[0].Name = "mutated"
	if RouterTools()[0].Name == "mutated" {
		t.Fatal("RouterTools shares state")
	}
	if len(CoreRouterNames()) == 0 {
		t.Fatal("no core router names")
	}
	if maxOffset, mime, protocols := SourceBounds(); maxOffset <= 0 || mime == "" || len(protocols) == 0 {
		t.Fatalf("bounds = %d %q %v", maxOffset, mime, protocols)
	}
	if len(ProductionHidden()) == 0 {
		t.Fatal("no hidden tools")
	}
}
