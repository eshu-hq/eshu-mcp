package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed contracts/tool-registry/v1/tools.json
var toolsJSON []byte

// contract mirrors the fields of tools.json this binary serves. It stays
// unexported: callers use the accessor functions so the parsed surface has
// exactly one construction site.
type contract struct {
	ContractVersion string `json:"contract_version"`
	Registry        struct {
		ToolCount int      `json:"tool_count"`
		ToolOrder []string `json:"tool_order"`
	} `json:"registry"`
}

var (
	loadOnce sync.Once
	loaded   contract
	loadErr  error
)

func load() (contract, error) {
	loadOnce.Do(func() {
		var decoded contract
		if err := json.Unmarshal(toolsJSON, &decoded); err != nil {
			loadErr = fmt.Errorf("registry: decode embedded tools.json: %w", err)
			return
		}
		if len(decoded.Registry.ToolOrder) != decoded.Registry.ToolCount {
			loadErr = fmt.Errorf(
				"registry: embedded tools.json lists %d names but declares tool_count %d",
				len(decoded.Registry.ToolOrder), decoded.Registry.ToolCount,
			)
			return
		}
		loaded = decoded
	})
	return loaded, loadErr
}

// LoadError reports the embedded registry load failure, if any. The binary
// fails fast on it at startup, so health can never answer 200 with an empty
// contract and zero tools.
func LoadError() error {
	_, err := load()
	return err
}

// ContractVersion reports the pinned tool-contract version (v1).
func ContractVersion() string {
	c, err := load()
	if err != nil {
		return ""
	}
	return c.ContractVersion
}

// Count reports the number of registry tools the server advertises.
func Count() int {
	c, err := load()
	if err != nil {
		return 0
	}
	return c.Registry.ToolCount
}

// ToolNames reports the registry tool names in contract registration order.
// Order is contractual: callers must not sort or filter the result. The
// returned slice is a copy; mutating it cannot change the served registry.
func ToolNames() []string {
	c, err := load()
	if err != nil {
		return nil
	}
	out := make([]string, len(c.Registry.ToolOrder))
	copy(out, c.Registry.ToolOrder)
	return out
}
